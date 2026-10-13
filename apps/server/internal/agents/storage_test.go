package agents

import (
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/realtime"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
	"strings"
	"testing"
)

func TestAgentStorageFailuresNeverReportSuccess(t *testing.T) { // STU-125/AGT-024: transactional failures remain visible.
	cases := []struct{ name, table, op string }{
		{"config", "projects", ""}, {"detail", "agent_runs", ""}, {"steps", "agent_steps", ""}, {"list", "agent_runs", ""}, {"list-detail", "agent_steps", ""}, {"recover-read", "agent_runs", ""}, {"recover-write", "agent_steps", "INSERT"},
		{"register-actor", "actors", "INSERT"}, {"register-definition", "agent_definitions", "INSERT"},
		{"session-definition", "agent_definitions", ""}, {"session-settings", "projects", ""}, {"session-insert", "agent_sessions", "INSERT"}, {"session-scope", "agent_session_changesets", "INSERT"},
		{"run-config", "projects", ""}, {"run-definition", "agent_definitions", ""}, {"run-insert", "agent_runs", "INSERT"},
		{"cancel-read", "agent_runs", ""}, {"cancel-lock", "agent_sessions", ""}, {"cancel-revoke", "agent_sessions", "UPDATE"},
		{"update-read", "agent_runs", ""}, {"update-lock", "agent_sessions", ""}, {"update-state", "agent_runs", "UPDATE"}, {"update-revoke", "agent_sessions", "UPDATE"}, {"update-step", "agent_runs", "UPDATE"},
		{"reserve-insert", "agent_usage", "INSERT"}, {"reserve-read", "agent_usage", ""},
		{"execute-load", "agent_runs", ""}, {"execute-start", "agent_steps", "INSERT"},
		{"auth-principal", "actors", ""}, {"auth-rights", "role_bindings", ""}, {"auth-settings", "projects", ""}, {"auth-scope", "agent_session_changesets", ""},
		{"tool-session", "agent_sessions", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			e, s, h, cs, doc := setupAgent(t)
			agentID := registered(t, e)
			session := delegation(t, e, h, agentID, cs, 10)
			a := agentActor(t, e, session)
			persisted, err := e.Q.GetAgentSession(ctx, store.GetAgentSessionParams{ID: session.ID, ProjectID: h.ProjectID})
			if err != nil {
				t.Fatal(err)
			}
			id := runStart(t, e, h, cs, doc)
			r, err := loadRun(ctx, e.Pool, id)
			if err != nil {
				t.Fatal(err)
			}
			if test.name == "recover-write" {
				e.Exec(`UPDATE agent_runs SET deadline=now()-interval '1 second' WHERE id=$1`, id)
			}
			e.Break(test.table, test.op)
			switch test.name {
			case "config":
				_, err = s.Config(ctx, h)
			case "detail", "steps":
				_, err = s.Detail(ctx, h, id)
			case "list", "list-detail":
				_, err = s.List(ctx, h, Scope{cs, doc, "staging"})
			case "recover-read", "recover-write":
				err = s.Recover(ctx)
			case "register-actor", "register-definition":
				err = e.Do(e.Admin, "register-agent", registerPayload{"Failure test", ""}, nil)
			case "session-definition", "session-settings", "session-insert", "session-scope":
				err = e.Do(h, "start-agent-session", sessionPayload{TokenHash: testTokenHash(), AgentID: agentID, Changesets: []uuid.UUID{cs}, Rights: []string{string(auth.DesignRead)}, TTLSeconds: 60, OpsQuota: 1}, nil)
			case "run-config", "run-definition", "run-insert":
				// The first run must be closed so the next start reaches the mutation under test.
				e.Exec(`UPDATE agent_runs SET state='completed' WHERE id=$1`, id)
				err = e.Do(h, "start-agent-run", runPayload{Scope: scopeFor(cs, doc), Prompt: "fail", ExpectedSeq: 1}, nil)
			case "cancel-read", "cancel-lock", "cancel-revoke":
				err = e.Do(h, "cancel-agent-run", runRef{id}, nil)
			case "update-read", "update-lock", "update-state", "update-revoke":
				err = s.mutate(ctx, r, update{State: "completed", Kind: "finished", Data: json.RawMessage(`{}`)})
			case "update-step":
				err = s.mutate(ctx, r, update{Kind: "message", Data: json.RawMessage(`{}`)})
			case "reserve-insert", "reserve-read":
				err = s.mutate(ctx, r, update{UsageID: uuid.New(), Reserve: 15000})
			case "execute-load", "execute-start":
				err = s.Execute(ctx, id)
			case "auth-principal", "auth-rights", "auth-settings", "auth-scope":
				_, err = auth.SessionActor(ctx, e.Q, persisted, false)
			case "tool-session":
				_, err = s.Call(ctx, a, "get_changeset", mustJSON(map[string]any{"changesetId": cs}), "failure", nil)
			}
			if err == nil {
				t.Fatal("database failure reported success")
			}
		})
	}
}
func TestAgentRunInputAndSessionBoundaries(t *testing.T) {
	e, s, h, cs, doc := setupAgent(t)
	p := runPayload{Scope: scopeFor(cs, doc), Prompt: "task", ExpectedSeq: 1}
	for _, bad := range []runPayload{{Scope: p.Scope, Prompt: ""}, {Scope: p.Scope, Prompt: strings.Repeat("x", 4001)}, {Scope: p.Scope, Prompt: "ok", ExpectedSeq: -1}} {
		if err := e.Do(h, "start-agent-run", bad, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
			t.Fatal(err)
		}
	}
	if err := e.Do(e.Admin, "start-agent-run", p, nil); cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal(err)
	}
	stale := p
	stale.ExpectedSeq = 0
	if err := e.Do(h, "start-agent-run", stale, nil); cmstest.Code(err) != "AGENT_SCOPE_CHANGED" {
		t.Fatal(err)
	}
	badScope := p
	badScope.Environment = "missing"
	if err := e.Do(h, "start-agent-run", badScope, nil); err == nil {
		t.Fatal("invalid scope")
	}
	s.Provider = nil
	if err := e.Do(h, "start-agent-run", p, nil); cmstest.Code(err) != "AGENT_NOT_CONFIGURED" {
		t.Fatal(err)
	}
	if err := e.Do(h, "cancel-agent-run", runRef{uuid.New()}, nil); cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal(err)
	}
	if err := e.Do(h, "end-agent-session", sessionRef{uuid.New()}, nil); cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal(err)
	}
	id := registered(t, e)
	weak := h
	weak.Rights = auth.RightSet{auth.AgentDelegate: true}
	if err := e.Do(weak, "start-agent-session", sessionPayload{TokenHash: testTokenHash(), AgentID: id, TTLSeconds: 60, OpsQuota: 1, Rights: []string{string(auth.DesignRead)}}, nil); cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal("token scope escalated", err)
	}
	e.Exec(`UPDATE projects SET settings='[]' WHERE id=$1`, h.ProjectID)
	if _, err := s.Config(ctx, h); err == nil {
		t.Fatal("invalid settings decoded")
	}
}
func TestDistinctAgentSessionsAndMandatoryReview(t *testing.T) { // AGT-001/022/031, PUB-003.
	e, s, h, cs, doc := setupAgent(t)
	id := registered(t, e)
	e.Exec(`UPDATE projects SET settings='{"approvals":{"low":0,"medium":0,"high":0}}' WHERE id=$1`, h.ProjectID)
	start := func(right auth.Right) auth.Actor {
		var session Session
		secret, hash := auth.NewToken(auth.AgentTokenPrefix)
		e.Must(h, "start-agent-session", sessionPayload{TokenHash: hash, AgentID: id, Changesets: []uuid.UUID{cs}, TTLSeconds: 60, OpsQuota: 5, Rights: []string{string(right), string(auth.DesignRead)}}, &session)
		session.Token = secret
		return agentActor(t, e, session)
	}
	contentActor := start(auth.ContentWrite)
	designActor := start(auth.DesignCompose)
	input := mustJSON(map[string]any{"changesetId": cs, "documentId": doc, "expectedSeq": 1, "reason": "Change local text", "payload": map[string]any{"key": "t_agent", "type": "text", "locale": "ru", "value": "Agent text"}})
	if _, err := s.Call(ctx, contentActor, "set_local_content", input, "content", nil); err != nil {
		t.Fatal(err)
	}
	input = mustJSON(map[string]any{"changesetId": cs, "documentId": doc, "expectedSeq": 2, "reason": "Change composition", "payload": map[string]any{"nodeId": "n_root", "set": map[string]any{"as": "section"}}})
	if _, err := s.Call(ctx, designActor, "update_props", input, "design", nil); err != nil {
		t.Fatal(err)
	}
	var review workflow.Review
	e.Must(h, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": 3}, &review)
	if review.RequiredApprovals != 1 || review.Changeset.State != "in_review" {
		t.Fatal("agent bypassed review", review)
	}
	for _, check := range review.Checks {
		if check.Stage == "policy" && check.Status != "passed" {
			t.Fatal("session rights cached by actor", check)
		}
	}
	detail, err := workflow.GetReview(ctx, e.Q, h.ProjectID, cs)
	if err != nil || detail.RequiredApprovals != 1 {
		t.Fatal(detail, err)
	}
	reviewer := e.Human("Reviewer", auth.ContentPublish, auth.DesignRead)
	e.Must(reviewer, "approve-changeset", map[string]any{"changesetId": cs, "expectedSeq": 3}, &review)
	if review.RequiredApprovals != 1 || review.Changeset.State != "approved" {
		t.Fatal(review)
	}
	rawCS, err := e.Q.GetChangeset(ctx, store.GetChangesetParams{ID: cs, ProjectID: h.ProjectID})
	if err != nil {
		t.Fatal(err)
	}
	if err = workflow.RequireZoneApprovals(ctx, e.Q, rawCS, "staging"); err != nil {
		t.Fatal(err)
	}
	e.Must(e.Admin, "set-agent-policy", auth.AgentPolicy{Allow: []string{string(auth.DesignRead)}}, nil)
	if err = workflow.RequireZoneApprovals(ctx, e.Q, rawCS, "staging"); cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal("old policy still published", err)
	}
}
func scopeFor(cs, doc uuid.UUID) realtime.Scope {
	return realtime.Scope{ChangesetID: cs, DocumentID: doc, Environment: "staging"}
}

func TestAgentGuardAndLivePolicyEdges(t *testing.T) {
	e, s, h, cs, doc := setupAgent(t)
	id := registered(t, e)
	session := delegation(t, e, h, id, cs, 10)
	a := agentActor(t, e, session)
	transaction, err := e.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(ctx)
	q := store.New(transaction)
	for _, test := range []struct {
		actor        auth.Actor
		name, reason string
		payload      []byte
		expected     error
	}{
		{auth.Actor{ID: a.ID, Kind: auth.ActorAgent, ProjectID: h.ProjectID}, "create-changeset", "reason", []byte(`{}`), auth.ErrUnauthenticated},
		{auth.Actor{ID: uuid.New(), Kind: auth.ActorAgent, ProjectID: h.ProjectID, AgentSessionID: a.AgentSessionID}, "create-changeset", "reason", []byte(`{}`), auth.ErrUnauthenticated},
		{a, "apply-operations", "reason", []byte(`bad`), auth.ErrAgentPolicy},
		{a, "create-changeset", "", []byte(`{}`), auth.ErrAgentPolicy},
	} {
		_, _, _, err := auth.GuardAgent(ctx, q, test.actor, test.name, test.reason, test.payload)
		if !errors.Is(err, test.expected) {
			t.Fatal(err)
		}
	}
	if _, _, count, err := auth.GuardAgent(ctx, q, a, "create-preview-token", "preview", mustJSON(map[string]any{"changesetId": cs})); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	transaction.Rollback(ctx)
	// Scope prevents use of both another project and another user's CS.
	other := e.Human("Other", auth.AgentDelegate, auth.DesignCompose, auth.DesignRead)
	var otherCS struct{ ID uuid.UUID }
	e.Must(other, "create-changeset", map[string]any{"title": "other", "targets": []string{"staging"}}, &otherCS)
	if err = e.Do(h, "start-agent-session", sessionPayload{TokenHash: testTokenHash(), AgentID: id, Changesets: []uuid.UUID{otherCS.ID}, TTLSeconds: 60, OpsQuota: 1}, nil); cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal(err)
	}
	if err = e.Do(h, "start-agent-session", sessionPayload{TokenHash: testTokenHash(), AgentID: uuid.New(), TTLSeconds: 60, OpsQuota: 1}, nil); cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal(err)
	}
	var created commandbus.Response
	created, err = dispatch(e, a, "create-changeset", map[string]any{"title": "Agent-created", "targets": []string{"staging"}}, "create", "Create delegated draft")
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Result struct {
			ID      uuid.UUID
			OwnerID uuid.UUID
		}
	}
	json.Unmarshal(created.Body, &decoded)
	if decoded.Result.OwnerID != h.ID {
		t.Fatal("delegated owner")
	}
	fresh, err := auth.Authenticate(ctx, e.Q, "Bearer "+session.Token)
	if err != nil || !auth.AgentAllowsCS(fresh, decoded.Result.ID) {
		t.Fatal(fresh, err)
	}
	_, err = dispatch(e, a, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": 1, "operations": []any{map[string]any{"type": "node.rename", "target": doc, "payload": map[string]any{"nodeId": "n_root", "name": "Agent name"}}}}, "edit", "Agent authored change")
	if err != nil {
		t.Fatal(err)
	}
	// Immediate policy narrowing also applies to an already authenticated actor.
	e.Must(e.Admin, "set-agent-policy", auth.AgentPolicy{Allow: []string{string(auth.ContentWrite)}}, nil)
	for _, name := range []string{"get_changeset", "get_page_structure", "get_capabilities", "list_schemas"} {
		args := map[string]any{"changesetId": cs, "documentId": doc, "environment": "staging"}
		_, err = s.Call(ctx, a, name, mustJSON(args), "read", nil)
		if cmstest.Code(err) != "FORBIDDEN" {
			t.Fatal(name, err)
		}
	}
	if len(auth.PolicyRights([]byte(`bad`))) != 0 {
		t.Fatal("invalid policy failed open")
	}
	// Authentication never returns a raw database error for a missing or expired token.
	if _, err = auth.Authenticate(ctx, e.Q, "Bearer cms_agt_unknown"); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal(err)
	}
	e.Exec(`UPDATE agent_sessions SET expires_at=now()-interval '1 second' WHERE id=$1`, session.ID)
	if _, err = auth.Authenticate(ctx, e.Q, "Bearer "+session.Token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal(err)
	}
}
