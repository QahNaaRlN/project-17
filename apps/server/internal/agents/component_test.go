package agents

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

// STU-417: план и create_component работают в делегированной сессии с provenance агента.
func TestCreateComponentToolSTU417(t *testing.T) {
	s := &Service{DefaultModel: "test/model", Provider: &fakeProvider{fn: func(context.Context, []Message) (Answer, error) { return Answer{}, nil }}}
	e := cmstest.New(t, s.Register, workflow.Register)
	s.Pool = e.Pool
	h := e.Human("Designer", auth.AgentDelegate, auth.DesignRead, auth.DesignCompose, auth.ComponentWrite)
	var created changes.Changeset
	e.Must(h, "create-changeset", map[string]any{"title": "Agent draft", "targets": []string{"staging"}}, &created)
	cs := created.ID
	var applied changes.ApplyResult
	e.Must(h, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": 0, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": "/agent", "root": map[string]any{"id": "n_root", "type": "Box"}}}}}, &applied)
	doc := applied.Operations[0].Target
	e.Must(h, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": 1, "operations": []any{map[string]any{"type": "node.insert", "target": doc, "payload": map[string]any{
		"parentId": "n_root", "subtree": map[string]any{"id": "n_block", "type": "Stack", "children": []any{map[string]any{"id": "n_head", "type": "Heading", "props": map[string]any{"level": 2}}}}}}}}, &applied)
	id := registered(t, e)
	start := func(rights ...auth.Right) auth.Actor {
		var out Session
		secret, hash := auth.NewToken(auth.AgentTokenPrefix)
		names := []string{}
		for _, r := range rights {
			names = append(names, string(r))
		}
		e.Must(h, "start-agent-session", sessionPayload{TokenHash: hash, AgentID: id, Changesets: []uuid.UUID{cs}, Rights: names, TTLSeconds: 3600, OpsQuota: 5}, &out)
		out.Token = secret
		return agentActor(t, e, out)
	}
	plan := mustJSON(map[string]any{"changesetId": cs, "documentId": doc, "nodeId": "n_block"})
	reader := start(auth.DesignRead, auth.DesignCompose)
	value, err := s.Call(ctx, reader, "plan_component_extract", plan, "plan", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(value)
	var view struct {
		Blockers []struct{ Code string }
	}
	_ = json.Unmarshal(raw, &view)
	if len(view.Blockers) != 1 || view.Blockers[0].Code != "FORBIDDEN" {
		t.Fatalf("plan without component.write: %s", raw)
	}
	input := mustJSON(map[string]any{"changesetId": cs, "documentId": doc, "expectedSeq": 2, "reason": "Повторное использование блока", "payload": map[string]any{
		"nodeId": "n_block", "name": "Block", "inputs": map[string]any{"level": map[string]any{"from": []any{map[string]any{"nodeId": "n_head", "prop": "level"}}}}}})
	// Без component.write операция отклоняется.
	if _, err := s.Call(ctx, reader, "create_component", input, "denied", nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	agent := start(auth.DesignRead, auth.DesignCompose, auth.ComponentWrite)
	value, err = s.Call(ctx, agent, "create_component", input, "create", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(value)
	var wrapped struct{ Result changes.ApplyResult }
	_ = json.Unmarshal(raw, &wrapped)
	result := wrapped.Result
	if len(result.Operations) == 0 {
		t.Fatalf("response: %s", raw)
	}
	after, _ := result.Operations[0].After.(map[string]any)
	component, err := uuid.Parse(after["componentId"].(string))
	if err != nil || after["instanceNodeId"] == "" || result.Changeset.Seq != 3 {
		t.Fatalf("response: %s", raw)
	}
	var source string
	var behalf uuid.UUID
	if err := e.Pool.QueryRow(ctx, `SELECT source,on_behalf_of FROM operations WHERE changeset_id=$1 AND type='component.extract'`, cs).Scan(&source, &behalf); err != nil || source != "agent" || behalf != h.ID {
		t.Fatal("provenance", err)
	}
	// Встроенный Agent читает документ компонента, на который ссылается страница сессии.
	bound := &Scope{cs, doc, "staging"}
	if _, err := s.Call(ctx, agent, "get_page_structure", mustJSON(map[string]any{"changesetId": cs, "documentId": component}), "read", bound); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Call(ctx, agent, "get_page_structure", mustJSON(map[string]any{"changesetId": cs, "documentId": uuid.New()}), "read", bound); cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal(err)
	}
	found := false
	for _, tool := range Tools() {
		found = found || tool.Name == "create_component"
	}
	if !found {
		t.Fatal("catalog")
	}
}
