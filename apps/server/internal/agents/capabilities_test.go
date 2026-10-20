package agents

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/capabilities"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/manifestregistry"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

func TestMissingCapabilityToolsAreDraftOnlyScopedAndQuotaBound(t *testing.T) {
	e, s, h, _, _ := setupAgent(t)
	capabilities.Register(e.Bus)
	e.Exec(`UPDATE roles SET capabilities=array_append(capabilities,'capability.request') WHERE name='role-Owner'`)
	h.Rights[auth.CapabilityRequest] = true
	e.Exec(`UPDATE projects SET settings=settings || '{"defaultMode":"CODE","maxMode":"CODE"}' WHERE id=$1`, h.ProjectID)
	var cs changes.Changeset
	e.Must(h, "create-changeset", map[string]any{"title": "missing", "targets": []string{"staging"}}, &cs)
	var applied changes.ApplyResult
	e.Must(h, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 0, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": "/missing", "root": map[string]any{"id": "n_root", "type": "Button"}}}}}, &applied)
	doc := applied.Operations[0].Target
	id := registered(t, e)
	secret, hash := auth.NewToken(auth.AgentTokenPrefix)
	var session Session
	e.Must(h, "start-agent-session", sessionPayload{TokenHash: hash, AgentID: id, Changesets: []uuid.UUID{cs.ID}, Rights: []string{string(auth.DesignRead), string(auth.CapabilityRequest)}, TTLSeconds: 3600, OpsQuota: 1}, &session)
	session.Token = secret
	a := agentActor(t, e, session)
	active, err := manifestregistry.GetActive(ctx, e.Q, h.ProjectID, "staging")
	if err != nil {
		t.Fatal(err)
	}
	p := map[string]any{"changesetId": cs.ID, "documentId": doc, "environment": "staging", "expectedSeq": 1, "reason": "A typed action is missing", "payload": map[string]any{"expectedManifestHash": active.ManifestHash, "nodeId": "n_root", "event": "click", "key": "store.favorite", "title": "Favorite", "description": "Save a favorite", "proposedContract": map[string]any{"args": map[string]any{}}, "acceptance": []string{"Only an explicit click saves"}}}
	bound := Scope{ChangesetID: cs.ID, DocumentID: doc, Environment: "staging"}
	result, err := s.Call(ctx, a, "report_missing_capability", mustJSON(p), "draft-exact", &bound)
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Result struct {
			Request struct {
				ID       uuid.UUID
				Status   string
				Revision int64
			}
		}
	}
	body, _ := json.Marshal(result)
	if err = json.Unmarshal(body, &value); err != nil || value.Result.Request.Status != "draft" {
		t.Fatal(result, err)
	}
	if _, err = s.Call(ctx, a, "report_missing_capability", mustJSON(p), "draft-exact", &bound); err != nil {
		t.Fatal("replay", err)
	}
	var used int
	e.Pool.QueryRow(ctx, `SELECT ops_used FROM agent_sessions WHERE id=$1`, session.ID).Scan(&used)
	if used != 1 {
		t.Fatal("draft quota", used)
	}
	get := map[string]any{"id": value.Result.Request.ID, "changesetId": cs.ID, "documentId": doc, "environment": "staging"}
	if _, err = s.Call(ctx, a, "get_capability_request", mustJSON(get), "read", &bound); err != nil {
		t.Fatal(err)
	}
	get["documentId"] = uuid.New()
	if _, err = s.Call(ctx, a, "get_capability_request", mustJSON(get), "read-other", nil); cmstest.Code(err) != "AGENT_POLICY_FORBIDDEN" && cmstest.Code(err) != "FORBIDDEN" {
		if err == nil {
			t.Fatal("wrong anchor allowed")
		}
	}
	// STU-371: malformed draft calls are refused before any command.
	for _, broken := range []map[string]any{
		{"changesetId": cs.ID, "environment": "staging", "expectedSeq": 1, "reason": "x", "payload": p["payload"]},
		{"changesetId": cs.ID, "documentId": doc, "environment": "staging", "expectedSeq": 1, "reason": "x", "payload": []any{}},
		{"changesetId": cs.ID, "documentId": doc, "environment": "staging", "expectedSeq": 1, "reason": "x", "payload": map[string]any{"environment": "production"}},
	} {
		if _, err = s.Call(ctx, a, "report_missing_capability", mustJSON(broken), "broken", &bound); err == nil {
			t.Fatal("malformed draft accepted", broken)
		}
	}
	if _, err = s.Call(ctx, a, "report_missing_capability", mustJSON(p), "quota", &bound); cmstest.Code(err) != "AGENT_QUOTA_EXCEEDED" {
		t.Fatal(err)
	}
	for _, name := range []string{"submit-capability-request", "resolve-capability-request", "update-capability-request"} {
		if _, err = s.Call(ctx, a, name, mustJSON(map[string]any{"changesetId": cs.ID}), "forbidden", &bound); cmstest.Code(err) != "TOOL_NOT_FOUND" {
			t.Fatal(name, err)
		}
	}
	e.Exec(`UPDATE roles SET capabilities=array_remove(capabilities,'capability.request') WHERE name='role-Owner'`)
	get["documentId"] = doc
	if _, err = s.Call(ctx, a, "get_capability_request", mustJSON(get), "revoked", &bound); err == nil {
		t.Fatal("delegator revocation ignored")
	}
}

func TestEmbeddedRunStopsAfterCreatingRequestDraft(t *testing.T) {
	e, s, h, _, _ := setupAgent(t)
	capabilities.Register(e.Bus)
	e.Exec(`UPDATE roles SET capabilities=array_append(capabilities,'capability.request') WHERE name='role-Owner'`)
	h.Rights[auth.CapabilityRequest] = true
	e.Exec(`UPDATE projects SET settings=settings || '{"defaultMode":"CODE","maxMode":"CODE"}' WHERE id=$1`, h.ProjectID)
	var cs changes.Changeset
	e.Must(h, "create-changeset", map[string]any{"title": "draft only", "targets": []string{"staging"}}, &cs)
	var made changes.ApplyResult
	e.Must(h, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 0, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": "/draft-only", "root": map[string]any{"id": "n_root", "type": "Button"}}}}}, &made)
	doc := made.Operations[0].Target
	active, err := manifestregistry.GetActive(ctx, e.Q, h.ProjectID, "staging")
	if err != nil {
		t.Fatal(err)
	}
	fake := s.Provider.(*fakeProvider)
	fake.fn = func(context.Context, []Message) (Answer, error) {
		var call ToolCall
		call.ID = "draft"
		call.Type = "function"
		call.Function.Name = "report_missing_capability"
		call.Function.Arguments = string(mustJSON(map[string]any{"changesetId": cs.ID, "documentId": doc, "environment": "staging", "expectedSeq": 1, "reason": "Missing typed action", "payload": map[string]any{"expectedManifestHash": active.ManifestHash, "nodeId": "n_root", "event": "click", "key": "store.new", "title": "New action", "description": "A typed action", "proposedContract": map[string]any{"args": map[string]any{}}, "acceptance": []string{"Explicit click only"}}}))
		return Answer{Message: Message{Role: "assistant", ToolCalls: []ToolCall{call}}, Usage: Usage{100, 20}}, nil
	}
	id := runStart(t, e, h, cs.ID, doc)
	if err = s.Execute(ctx, id); err != nil {
		t.Fatal(err)
	}
	detail, err := s.Detail(ctx, h, id)
	if err != nil || detail.State != "completed" || fake.calls.Load() != 1 {
		t.Fatal(detail, err, fake.calls.Load())
	}
	var requestID string
	for _, step := range detail.Steps {
		var data struct {
			RequestID string `json:"requestId"`
		}
		_ = json.Unmarshal(step.Data, &data)
		if data.RequestID != "" {
			requestID = data.RequestID
		}
	}
	if requestID == "" {
		t.Fatal("draft not shown in run")
	}
	var status string
	var seq int32
	if err = e.Pool.QueryRow(ctx, `SELECT status FROM capability_requests WHERE id=$1`, requestID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err = e.Pool.QueryRow(ctx, `SELECT seq FROM changesets WHERE id=$1`, cs.ID).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	if status != "draft" || seq != 1 {
		t.Fatal(status, seq)
	}
}
