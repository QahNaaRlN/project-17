package agents

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"strings"
	"testing"
)

func TestRunStopsBeforeUnsafeContinuation(t *testing.T) {
	for _, mode := range []string{"context", "result", "provider", "session", "accounting", "step", "expired"} {
		t.Run(mode, func(t *testing.T) {
			e, s, h, cs, doc := setupAgent(t)
			fake := s.Provider.(*fakeProvider)
			if mode == "context" || mode == "result" {
				size := 18000
				if mode == "result" {
					size = 25000
				}
				e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{localContent}', $2) WHERE id IN (SELECT working_version_id FROM changeset_objects WHERE changeset_id=$1)`, cs, mustJSON(map[string]any{"t_large": map[string]any{"type": "text", "value": map[string]string{"ru": strings.Repeat("a", size)}}}))
			}
			id := runStart(t, e, h, cs, doc)
			if mode == "expired" {
				e.Exec(`UPDATE agent_runs SET deadline=now()-interval '1 second' WHERE id=$1`, id)
			}
			if mode == "session" {
				e.Exec(`UPDATE agent_sessions SET revoked_at=now() WHERE id=(SELECT session_id FROM agent_runs WHERE id=$1)`, id)
			}
			fake.fn = func(context.Context, []Message) (Answer, error) {
				if mode == "provider" {
					return Answer{Usage: Usage{100, 10}}, ErrProvider
				}
				if mode == "accounting" {
					e.Break("agent_usage", "UPDATE")
					return Answer{Message: Message{Role: "assistant", Content: "never execute"}, Usage: Usage{100, 10}}, nil
				}
				if mode == "step" {
					e.Break("agent_steps", "INSERT")
					return Answer{Message: Message{Role: "assistant", Content: "never succeed"}, Usage: Usage{100, 10}}, nil
				}
				var call ToolCall
				call.ID = "read-page"
				call.Type = "function"
				call.Function.Name = "get_page_structure"
				call.Function.Arguments = string(mustJSON(map[string]any{"changesetId": cs, "documentId": doc}))
				return Answer{Message: Message{Role: "assistant", ToolCalls: []ToolCall{call}}, Usage: Usage{100, 10}}, nil
			}
			err := s.Execute(ctx, id)
			if mode == "accounting" || mode == "step" {
				if err == nil {
					t.Fatal("transactional failure hidden")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			detail, err := s.Detail(ctx, h, id)
			if err != nil {
				t.Fatal(err)
			}
			expected := map[string]string{"context": "AGENT_CONTEXT_LIMIT", "result": "AGENT_CONTEXT_LIMIT", "provider": "AGENT_PROVIDER_FAILED", "session": "AGENT_SESSION_INVALID", "expired": "AGENT_INTERRUPTED"}[mode]
			expectedState := "failed"
			if mode == "expired" {
				expectedState = "interrupted"
			}
			if detail.ErrorCode == nil || *detail.ErrorCode != expected || detail.State != expectedState {
				t.Fatal(detail)
			}
			if (mode == "session" || mode == "expired") && fake.calls.Load() != 0 {
				t.Fatal("revoked delegation called provider")
			}
			if mode == "context" && fake.calls.Load() > 3 {
				t.Fatal("unbounded context")
			}
		})
	}
}
func TestPrivateRunMetadataStillChecksOwnerAndTransition(t *testing.T) {
	e, s, h, cs, doc := setupAgent(t)
	id := runStart(t, e, h, cs, doc)
	r, err := loadRun(ctx, e.Pool, id)
	if err != nil {
		t.Fatal(err)
	}
	other := e.Human("Other", auth.AgentDelegate, auth.DesignRead)
	payload := mustJSON(update{RunID: id, State: "completed"})
	_, err = e.Bus.Dispatch(context.WithValue(ctx, internalKey{}, s), other, commandbus.Request{Name: "agent-internal-update", IdempotencyKey: uuid.NewString(), Payload: payload})
	if cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal(err)
	}
	if err = s.mutate(ctx, r, update{State: "running", Kind: "started", Data: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err = s.mutate(ctx, r, update{State: "running"}); cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal("duplicate worker claim", err)
	}
	if err = s.mutate(ctx, r, update{UsageID: uuid.New()}); cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal("missing accounting", err)
	}
	if _, err = s.Detail(ctx, auth.Actor{ID: h.ID, ProjectID: h.ProjectID}, id); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	if _, err = s.List(ctx, auth.Actor{ID: h.ID, ProjectID: h.ProjectID}, Scope{cs, doc, "staging"}); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
}
