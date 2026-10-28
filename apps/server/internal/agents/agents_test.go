package agents

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/realtime"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
	"github.com/riverqueue/river"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

var ctx = context.Background()

type fakeProvider struct {
	fn    func(context.Context, []Message) (Answer, error)
	calls atomic.Int32
}

func (p *fakeProvider) Ready() bool { return true }
func (p *fakeProvider) Complete(c context.Context, _ string, m []Message, _ []Tool) (Answer, error) {
	p.calls.Add(1)
	return p.fn(c, m)
}
func setupAgent(t *testing.T) (*cmstest.Env, *Service, auth.Actor, uuid.UUID, uuid.UUID) {
	s := &Service{DefaultModel: "test/model", Provider: &fakeProvider{fn: func(context.Context, []Message) (Answer, error) {
		return Answer{Message: Message{Role: "assistant", Content: "Готово"}, Usage: Usage{100, 10}}, nil
	}}}
	e := cmstest.New(t, s.Register, workflow.Register)
	s.Pool = e.Pool
	human := e.Human("Owner", auth.AgentDelegate, auth.DesignRead, auth.DesignCompose, auth.ContentWrite, auth.SchemaRead, auth.BehaviorUse, auth.ContentPublish)
	var cs changes.Changeset
	e.Must(human, "create-changeset", map[string]any{"title": "Agent draft", "targets": []string{"staging"}}, &cs)
	var applied changes.ApplyResult
	e.Must(human, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 0, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": "/agent", "root": map[string]any{"id": "n_root", "type": "Box"}}}}}, &applied)
	return e, s, human, cs.ID, applied.Operations[0].Target
}
func registered(t *testing.T, e *cmstest.Env) uuid.UUID {
	t.Helper()
	var out struct{ ID uuid.UUID }
	e.Must(e.Admin, "register-agent", registerPayload{"Test agent", "delegated"}, &out)
	return out.ID
}
func delegation(t *testing.T, e *cmstest.Env, a auth.Actor, id, cs uuid.UUID, quota int) Session {
	t.Helper()
	var out Session
	secret, hash := auth.NewToken(auth.AgentTokenPrefix)
	e.Must(a, "start-agent-session", sessionPayload{TokenHash: hash, AgentID: id, Changesets: []uuid.UUID{cs}, Rights: []string{string(auth.DesignRead), string(auth.DesignCompose), string(auth.ContentWrite), string(auth.SchemaRead)}, TTLSeconds: 28800, OpsQuota: quota}, &out)
	out.Token = secret
	return out
}
func testTokenHash() []byte { _, hash := auth.NewToken(auth.AgentTokenPrefix); return hash }
func agentActor(t *testing.T, e *cmstest.Env, s Session) auth.Actor {
	t.Helper()
	a, err := auth.Authenticate(ctx, e.Q, "Bearer "+s.Token)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func dispatch(e *cmstest.Env, a auth.Actor, name string, p any, key, reason string) (commandbus.Response, error) {
	raw, _ := json.Marshal(p)
	return e.Bus.Dispatch(ctx, a, commandbus.Request{Name: name, Payload: raw, IdempotencyKey: key, Reason: reason})
}
func runStart(t *testing.T, e *cmstest.Env, h auth.Actor, cs, doc uuid.UUID) uuid.UUID {
	t.Helper()
	var out struct{ ID uuid.UUID }
	e.Must(h, "start-agent-run", runPayload{Scope: realtime.Scope{ChangesetID: cs, DocumentID: doc, Environment: "staging"}, Prompt: "Измени заголовок", ExpectedSeq: 1}, &out)
	return out.ID
}

// AGT-003: agent changes are operations with actor.kind = agent in a Change Set.
// AGT-004: the agent reads structure through deterministic tools.
func TestDelegationInvariants(t *testing.T) { // AGT-001/020…024, STU-120/121.
	e, s, h, cs, doc := setupAgent(t)
	id := registered(t, e)
	session := delegation(t, e, h, id, cs, 1)
	var cached string
	if err := e.Pool.QueryRow(ctx, `SELECT convert_from(response,'UTF8') FROM idempotency_keys WHERE actor_id=$1 AND command='start-agent-session' LIMIT 1`, h.ID).Scan(&cached); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cached, session.Token) || strings.Contains(cached, `"token"`) {
		t.Fatal("credential persisted in idempotency response")
	}
	a := agentActor(t, e, session)
	if a.OnBehalfOf == nil || *a.OnBehalfOf != h.ID || a.Rights.Has(auth.ContentPublish) || a.Rights.Has(auth.ProjectAdmin) {
		t.Fatal("delegation escalated")
	}
	input := mustJSON(map[string]any{"changesetId": cs, "documentId": doc, "expectedSeq": 1, "reason": "Update heading", "payload": map[string]any{"nodeId": "n_root", "set": map[string]any{"as": "section"}}})
	if _, err := s.Call(ctx, a, "update_props", input, "same", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Call(ctx, a, "update_props", input, "same", nil); err != nil {
		t.Fatal("replay charged or seq checked", err)
	}
	var used int
	e.Pool.QueryRow(ctx, `SELECT ops_used FROM agent_sessions WHERE id=$1`, session.ID).Scan(&used)
	if used != 1 {
		t.Fatal("quota", used)
	}
	var behalf, actor, sid uuid.UUID
	var source string
	if err := e.Pool.QueryRow(ctx, `SELECT actor_id,on_behalf_of,agent_session_id,source FROM operations WHERE changeset_id=$1 AND seq=2`, cs).Scan(&actor, &behalf, &sid, &source); err != nil {
		t.Fatal(err)
	}
	if actor != id || behalf != h.ID || sid != session.ID || source != "agent" {
		t.Fatal("provenance")
	}
	input = mustJSON(map[string]any{"changesetId": cs, "documentId": doc, "expectedSeq": 2, "reason": "Quota", "payload": map[string]any{"nodeId": "n_root", "set": map[string]any{"as": "section"}}})
	if _, err := s.Call(ctx, a, "update_props", input, "quota", nil); cmstest.Code(err) != "AGENT_QUOTA_EXCEEDED" {
		t.Fatal(err)
	}
	// Another delegation with the same actor and key cannot replay session one's result.
	second := delegation(t, e, h, id, cs, 2)
	secondActor := agentActor(t, e, second)
	if _, err := s.Call(ctx, secondActor, "update_props", input, "same", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Call(ctx, a, "get_page_structure", mustJSON(map[string]any{"changesetId": cs, "documentId": doc}), "read", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Call(ctx, a, "get_changeset", mustJSON(map[string]any{"changesetId": cs}), "read", nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"list_schemas", "get_capabilities"} {
		if _, err := s.Call(ctx, a, name, mustJSON(map[string]any{"environment": "staging"}), "read", nil); err != nil {
			t.Fatal(name, err)
		}
	}
	for _, test := range []struct {
		name  string
		raw   []byte
		bound *Scope
		code  string
	}{
		{"update_props", []byte(`{broken`), nil, "VALIDATION_FAILED"}, {"update_props", []byte(`{} {}`), nil, "VALIDATION_FAILED"},
		{"get_changeset", mustJSON(map[string]any{"changesetId": uuid.New()}), nil, "POLICY_DENIED"},
		{"get_page_structure", mustJSON(map[string]any{"changesetId": cs, "documentId": doc}), &Scope{cs, uuid.New(), "staging"}, "POLICY_DENIED"},
		{"unknown", mustJSON(map[string]any{"changesetId": cs}), nil, "TOOL_NOT_FOUND"},
		{"update_props", mustJSON(map[string]any{"changesetId": cs}), nil, "VALIDATION_FAILED"},
		{"update_props", mustJSON(map[string]any{"changesetId": cs, "documentId": doc, "expectedSeq": 3, "payload": []any{}}), nil, "VALIDATION_FAILED"},
	} {
		_, err := s.Call(ctx, a, test.name, test.raw, "test", test.bound)
		if cmstest.Code(err) != test.code {
			t.Errorf("%s: %v", test.name, err)
		}
	}
	if _, err := s.Call(ctx, h, "get_changeset", []byte(`{}`), "no", nil); cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal(err)
	}
	_, err := dispatch(e, secondActor, "undo", map[string]any{"changesetId": cs, "expectedSeq": 3}, "deny", "Model undo")
	if cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal(err)
	}
	// Human can undo the agent operation with ordinary immutable CS history.
	e.Must(h, "undo", map[string]any{"changesetId": cs, "expectedSeq": 3}, nil)
	e.Must(h, "end-agent-session", sessionRef{session.ID}, nil)
	if _, err = dispatch(e, a, "create-changeset", map[string]any{"title": "Revoked"}, "revoked", "Explicit test"); cmstest.Code(err) != "UNAUTHENTICATED" {
		t.Fatal("cached actor escaped revocation", err)
	}
	if _, err = auth.Authenticate(ctx, e.Q, "Bearer "+session.Token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal("revoked token", err)
	}
	if _, err = s.Call(ctx, a, "get_changeset", mustJSON(map[string]any{"changesetId": cs}), "read", nil); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal("stale actor", err)
	}
	e.Exec(`UPDATE actors SET disabled_at=now() WHERE id=$1`, h.ID)
	if _, err = auth.Authenticate(ctx, e.Q, "Bearer "+second.Token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal("disabled delegator", err)
	}
}
func TestSessionPolicyAndValidation(t *testing.T) {
	e, s, h, cs, _ := setupAgent(t)
	id := registered(t, e)
	for _, p := range []sessionPayload{{AgentID: id, TTLSeconds: 28801, OpsQuota: 1}, {AgentID: id, TTLSeconds: 1, OpsQuota: 0}, {AgentID: uuid.Nil, TTLSeconds: 1, OpsQuota: 1}} {
		p.TokenHash = testTokenHash()
		if err := e.Do(h, "start-agent-session", p, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
			t.Fatal(err)
		}
	}
	for _, p := range []registerPayload{{"", ""}, {strings.Repeat("x", 101), ""}, {"okay", strings.Repeat("x", 1001)}} {
		if err := e.Do(e.Admin, "register-agent", p, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
			t.Fatal(err)
		}
	}
	if err := e.Do(e.Admin, "set-agent-policy", auth.AgentPolicy{Allow: []string{"unknown"}}, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
		t.Fatal(err)
	}
	e.Must(e.Admin, "set-agent-policy", auth.AgentPolicy{Allow: []string{string(auth.DesignRead), string(auth.DesignCompose)}, Deny: []string{string(auth.DesignCompose)}}, nil)
	if err := e.Do(h, "start-agent-session", sessionPayload{TokenHash: testTokenHash(), AgentID: id, TTLSeconds: 60, OpsQuota: 1, Rights: []string{string(auth.DesignCompose)}}, nil); cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal(err)
	}
	var session Session
	secretSession, hashSession := auth.NewToken(auth.AgentTokenPrefix)
	e.Must(h, "start-agent-session", sessionPayload{TokenHash: hashSession, AgentID: id, TTLSeconds: 60, OpsQuota: 1, Changesets: []uuid.UUID{cs}, Rights: []string{string(auth.DesignRead)}}, &session)
	session.Token = secretSession
	a := agentActor(t, e, session)
	if a.Rights.Has(auth.DesignCompose) {
		t.Fatal("deny ignored")
	}
	e.Must(e.Admin, "set-agent-policy", auth.AgentPolicy{Allow: []string{string(auth.DesignRead), string(auth.DesignCompose)}}, nil)
	a = agentActor(t, e, session)
	if a.Rights.Has(auth.DesignCompose) {
		t.Fatal("frozen ceiling grew")
	}
	other := e.Human("Other", auth.AgentDelegate)
	if err := e.Do(other, "end-agent-session", sessionRef{session.ID}, nil); cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal(err)
	}
	e.Exec(`UPDATE roles SET capabilities=ARRAY['design.read']::text[] WHERE project_id=$1 AND id IN (SELECT role_id FROM role_bindings WHERE actor_id=$2)`, h.ProjectID, h.ID)
	if _, err := auth.Authenticate(ctx, e.Q, "Bearer "+session.Token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal("lost delegation", err)
	}
	if _, err := s.Config(ctx, other); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	e.Must(e.Admin, "end-agent-session", sessionRef{session.ID}, nil)
	// Ordinary API tokens never authenticate agents, even with all role scopes.
	secret, hash := auth.NewToken(auth.ServiceTokenPrefix)
	e.Exec(`INSERT INTO api_tokens(id,project_id,actor_id,token_hash,scopes,expires_at) VALUES($1,$2,$3,$4,ARRAY['*'],now()+interval '1 hour')`, uuid.New(), h.ProjectID, id, hash)
	if _, err := auth.Authenticate(ctx, e.Q, "Bearer "+secret); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal(err)
	}
}

// AGT-041: the orchestrator writes into the user's open Change Set.
func TestRunLifecycle(t *testing.T) { // STU-123…125: outbox, usage, persistence, cancellation.
	e, s, h, cs, doc := setupAgent(t)
	if err := e.Do(h, "set-agent-config", Settings{"test/model", 100000, 10000}, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	for _, cfg := range []Settings{{"", 1, 1}, {"test/model", 0, 1}, {"test/model", 100, 101}} {
		if err := e.Do(e.Admin, "set-agent-config", cfg, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
			t.Fatal(err)
		}
	}
	e.Must(e.Admin, "set-agent-config", Settings{"test/model", 1000000, 100000}, nil)
	cfg, err := s.Config(ctx, h)
	if err != nil || cfg["configured"] != true || cfg["canDelegate"] != true {
		t.Fatal(cfg, err)
	}
	id := runStart(t, e, h, cs, doc)
	if err = e.Do(h, "start-agent-run", runPayload{Scope: realtime.Scope{ChangesetID: cs, DocumentID: doc, Environment: "staging"}, Prompt: "another", ExpectedSeq: 1}, nil); cmstest.Code(err) != "AGENT_RUN_ACTIVE" {
		t.Fatal(err)
	}
	r, err := loadRun(ctx, s.Pool, id)
	if err != nil {
		t.Fatal(err)
	}
	// Internal state commands cannot be forged via public Command API.
	if err = e.Do(e.Admin, "agent-internal-update", update{RunID: id, State: "completed"}, nil); cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal(err)
	}
	var count int
	e.Pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind='studio_agent' AND args->>'runId'=$1`, id.String()).Scan(&count)
	if count != 1 {
		t.Fatal("outbox", count)
	}
	fake := s.Provider.(*fakeProvider)
	fake.fn = func(_ context.Context, m []Message) (Answer, error) {
		if len(m) == 2 {
			var call ToolCall
			call.ID = "tool-1"
			call.Type = "function"
			call.Function.Name = "get_changeset"
			call.Function.Arguments = string(mustJSON(map[string]any{"changesetId": cs}))
			return Answer{Message: Message{Role: "assistant", ToolCalls: []ToolCall{call}}, Usage: Usage{100, 20}}, nil
		}
		if m[len(m)-1].Role != "tool" {
			t.Fatal("tool data not separated")
		}
		return Answer{Message: Message{Role: "assistant", Content: "<script>Готово</script>"}, Usage: Usage{100, 20}}, nil
	}
	if err = (&Worker{Service: s}).Work(ctx, &river.Job[RunArgs]{Args: RunArgs{RunID: id}}); err != nil {
		t.Fatal(err)
	}
	detail, err := s.Detail(ctx, h, id)
	if err != nil || detail.State != "completed" || detail.Tokens != 240 || !detail.UsageKnown || detail.Reserved != 0 || len(detail.Steps) != 5 {
		t.Fatal(detail, err)
	}
	if err = s.Execute(ctx, id); err != nil || fake.calls.Load() != 2 {
		t.Fatal("repeated LLM", err)
	}
	list, err := s.List(ctx, h, Scope{cs, doc, "staging"})
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	outsider := e.Human("Other", auth.AgentDelegate, auth.DesignRead)
	if _, err = s.Detail(ctx, outsider, id); cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal(err)
	}
	if err = e.Do(outsider, "cancel-agent-run", runRef{id}, nil); cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal(err)
	}
	if err = s.mutate(ctx, r, update{Kind: "message", Data: mustJSON(map[string]string{"text": "late"})}); cmstest.Code(err) != "AGENT_RUN_CLOSED" {
		t.Fatal(err)
	}
	cancelled := runStart(t, e, h, cs, doc)
	e.Must(h, "cancel-agent-run", runRef{cancelled}, nil)
	if err = s.Execute(ctx, cancelled); err != nil {
		t.Fatal(err)
	}
	detail, _ = s.Detail(ctx, h, cancelled)
	if detail.State != "cancelled" {
		t.Fatal(detail)
	}
	expired := runStart(t, e, h, cs, doc)
	r, _ = loadRun(ctx, s.Pool, expired)
	uid := uuid.New()
	if err = s.mutate(ctx, r, update{UsageID: uid, Reserve: 15000}); err != nil {
		t.Fatal(err)
	}
	e.Exec(`UPDATE agent_runs SET deadline=now()-interval '1 second',state='running' WHERE id=$1`, expired)
	if err = s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	detail, _ = s.Detail(ctx, h, expired)
	if detail.State != "interrupted" || detail.UsageKnown || detail.Reserved != 15000 {
		t.Fatal(detail)
	}
}
func TestRunUnknownUsageAndLimits(t *testing.T) {
	for _, mode := range []string{"unknown", "limit", "badtool", "steps", "oversize", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			e, s, h, cs, doc := setupAgent(t)
			fake := s.Provider.(*fakeProvider)
			id := runStart(t, e, h, cs, doc)
			switch mode {
			case "limit":
				e.Must(e.Admin, "set-agent-config", Settings{"test/model", 1000, 1000}, nil)
			case "unknown":
				fake.fn = func(context.Context, []Message) (Answer, error) { return Answer{}, ErrUsageUnknown }
			case "oversize":
				fake.fn = func(context.Context, []Message) (Answer, error) {
					return Answer{Message: Message{Role: "assistant"}, Usage: Usage{999999, 1}}, nil
				}
			case "cancel":
				fake.fn = func(c context.Context, _ []Message) (Answer, error) {
					e.Must(h, "cancel-agent-run", runRef{id}, nil)
					select {
					case <-c.Done():
					case <-time.After(4 * time.Second):
						t.Fatal("provider context not cancelled")
					}
					return Answer{}, ErrUsageUnknown
				}
			default:
				fake.fn = func(context.Context, []Message) (Answer, error) {
					var call ToolCall
					call.ID = "tool"
					call.Type = "function"
					call.Function.Name = "get_changeset"
					call.Function.Arguments = string(mustJSON(map[string]any{"changesetId": cs}))
					if mode == "badtool" {
						call.Function.Name = "publish"
					}
					return Answer{Message: Message{Role: "assistant", ToolCalls: []ToolCall{call}}, Usage: Usage{100, 10}}, nil
				}
			}
			if err := s.Execute(ctx, id); err != nil {
				t.Fatal(err)
			}
			detail, err := s.Detail(ctx, h, id)
			if err != nil {
				t.Fatal(err)
			}
			expected := map[string]string{"unknown": "AGENT_USAGE_UNKNOWN", "limit": "AGENT_TOKEN_LIMIT", "badtool": "AGENT_STEP_LIMIT", "steps": "AGENT_STEP_LIMIT", "oversize": "AGENT_USAGE_EXCEEDED"}[mode]
			if mode == "cancel" {
				if detail.State != "cancelled" {
					t.Fatal(detail)
				}
			} else if detail.ErrorCode == nil || *detail.ErrorCode != expected {
				t.Fatal(detail)
			}
			if mode == "unknown" && (detail.UsageKnown || detail.Reserved == 0 || fake.calls.Load() != 1) {
				t.Fatal("unknown silently retried", detail)
			}
			if mode == "limit" && fake.calls.Load() != 0 {
				t.Fatal("provider called above limit")
			}
		})
	}
}
func TestHistoricalAgentPolicy(t *testing.T) {
	e, s, h, cs, doc := setupAgent(t)
	id := registered(t, e)
	session := delegation(t, e, h, id, cs, 10)
	a := agentActor(t, e, session)
	input := mustJSON(map[string]any{"changesetId": cs, "documentId": doc, "expectedSeq": 1, "reason": "Change approved by a different human", "payload": map[string]any{"nodeId": "n_root", "set": map[string]any{"as": "section"}}})
	if _, err := s.Call(ctx, a, "update_props", input, "edit", nil); err != nil {
		t.Fatal(err)
	}
	e.Must(h, "end-agent-session", sessionRef{session.ID}, nil)
	var review workflow.Review
	e.Must(h, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": 2}, &review)
	if review.Risk == nil || *review.Risk == workflow.RiskLow || review.RequiredApprovals < 1 {
		t.Fatal(review)
	}
	if err := e.Do(h, "approve-changeset", map[string]any{"changesetId": cs, "expectedSeq": 2}, nil); cmstest.Code(err) != "APPROVAL_SELF" {
		t.Fatal("delegator self-approved", err)
	}
	persisted, err := e.Q.GetAgentSession(ctx, store.GetAgentSessionParams{ID: session.ID, ProjectID: h.ProjectID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = auth.SessionActor(ctx, e.Q, persisted, true); err != nil {
		t.Fatal("closed provenance invalidated", err)
	}
	e.Must(e.Admin, "set-agent-policy", auth.AgentPolicy{Allow: []string{string(auth.DesignRead)}}, nil)
	current, err := auth.SessionActor(ctx, e.Q, persisted, true)
	if err != nil || current.Rights.Has(auth.DesignCompose) {
		t.Fatal(current, err)
	}
}
