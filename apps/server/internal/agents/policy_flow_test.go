package agents

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/agentpolicy"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
	"github.com/riverqueue/river"
)

func setProps(node string) map[string]any {
	return map[string]any{"type": "node.setProps", "payload": map[string]any{"nodeId": node, "set": map[string]any{"as": "section"}}}
}

// STU-422…430/434/437: agent operations under a conditional policy, review
// and publication with the current version.
func TestSTU425AgentPolicyOperationsAndReview(t *testing.T) {
	e, _, h, cs, doc := setupAgent(t)
	var created changes.ApplyResult
	e.Must(h, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": 1, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": "/checkout", "root": map[string]any{"id": "n_co", "type": "Box"}}}}}, &created)
	checkout := created.Operations[0].Target
	policy := agentpolicy.Document{Version: 1,
		Rules: []agentpolicy.Rule{{ID: "no-checkout", Subject: agentpolicy.Subject{Agent: "*"}, Deny: []string{"design.compose"}, When: &agentpolicy.When{PathPrefix: []string{"/checkout"}}},
			{ID: "staging-review", Subject: agentpolicy.Subject{Agent: "Test agent"}, RequireApproval: []string{"design.compose"}, When: &agentpolicy.When{Environment: []string{"staging"}}},
			{ID: "no-request", Subject: agentpolicy.Subject{Agent: "*"}, Deny: []string{"capability.request"}, When: &agentpolicy.When{PathPrefix: []string{"/checkout"}, ZoneMode: []string{"SYSTEM", "STRICT", "FREE", "CODE"}}}},
		Risk:      []agentpolicy.RiskRule{{ID: "agent-risk", When: &agentpolicy.When{PathPrefix: []string{"/agent"}, Source: []string{"agent"}}, Min: "high"}},
		Approvals: []agentpolicy.ApprovalRule{{ID: "agent-two", When: &agentpolicy.When{Source: []string{"agent"}, Risk: []string{"high"}}, Require: agentpolicy.Require{Count: 2, Roles: []string{"editor"}}}},
	}
	if _, err := savePolicy(e, "p1", "freeze checkout", policy, 0); err != nil {
		t.Fatal(err)
	}
	session := delegation(t, e, h, registered(t, e), cs, 10)
	a := agentActor(t, e, session)
	apply := func(key string, seq int, ops ...map[string]any) error {
		items := []any{}
		for _, op := range ops {
			target := doc
			if op["checkout"] == true {
				target = checkout
			}
			items = append(items, map[string]any{"type": op["type"], "target": target, "payload": op["payload"]})
		}
		_, err := dispatch(e, a, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": seq, "operations": items}, key, "agent edit")
		return err
	}
	denied := setProps("n_co")
	denied["checkout"] = true
	err := apply("a1", 2, setProps("n_root"), denied)
	var ce *commandbus.Error
	if cmstest.Code(err) != "POLICY_DENIED" || !errors.As(err, &ce) || ce.Params["ruleId"] != "no-checkout" || ce.Params["operationIndex"] != 1 || ce.Params["reason"] != agentpolicy.ReasonDenied || ce.Params["condition"] != "pathPrefix=/checkout" {
		t.Fatal("batch refused with the index", err)
	}
	var event struct {
		Params map[string]any `json:"params"`
	}
	if err := e.Pool.QueryRow(ctx, `SELECT data FROM security_events WHERE actor_id=$1 ORDER BY created_at DESC LIMIT 1`, a.ID).Scan(&event); err != nil || event.Params["ruleId"] != "no-checkout" || event.Params["operationIndex"] != float64(1) {
		t.Fatal("security event", event, err)
	}
	persisted, err := e.Q.GetAgentSession(ctx, store.GetAgentSessionParams{ID: session.ID, ProjectID: h.ProjectID})
	if err != nil || persisted.OpsUsed != 0 {
		t.Fatal("quota charged for a refusal", persisted.OpsUsed, err)
	}
	if err := apply("a2", 2, setProps("n_root")); err != nil {
		t.Fatal(err)
	}
	changeset, err := e.Q.GetChangeset(ctx, store.GetChangesetParams{ID: cs, ProjectID: h.ProjectID})
	if err != nil || !slices.Equal(changeset.PolicyApproval, []string{"staging-review"}) {
		t.Fatal("requireApproval label", changeset.PolicyApproval, err)
	}
	// STU-437: people are not subject to agent rules.
	e.Must(h, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": 3, "operations": []any{map[string]any{"type": "node.setProps", "target": checkout, "payload": map[string]any{"nodeId": "n_co", "set": map[string]any{"as": "section"}}}}}, nil)
	// STU-425: a capability request of the agent is evaluated at its node.
	co := "/checkout"
	if err := changes.CheckAgentRight(ctx, e.Q, a, changeset, auth.CapabilityRequest, &co, map[string]any{"root": "n_co", "nodes": map[string]any{"n_co": map[string]any{"id": "n_co", "type": "Box"}}}, "n_co"); cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal("capability request at checkout", err)
	}
	if err := changes.CheckAgentRight(ctx, e.Q, a, changeset, auth.DesignCompose, nil, nil, ""); err != nil {
		t.Fatal("approval label for an agent command", err)
	}
	if err := changes.CheckAgentRight(ctx, e.Q, h, changeset, auth.CapabilityRequest, &co, nil, ""); err != nil {
		t.Fatal("people pass", err)
	}

	var review workflow.Review
	e.Must(h, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": 4}, &review)
	if review.Changeset.State != "in_review" || *review.Risk != workflow.RiskHigh || review.RequiredApprovals < 2 || !slices.Contains(review.RequiredRoles, "editor") {
		t.Fatal("risk and approval rules", review)
	}
	reviewer := e.Human("Reviewer", auth.ContentPublish, auth.DesignRead, auth.ContentRead)
	e.Must(reviewer, "approve-changeset", map[string]any{"changesetId": cs, "expectedSeq": 4}, &review)
	if review.Changeset.State == "approved" {
		t.Fatal("two approvals and the editor role are required", review)
	}
	// STU-430: a refusing version makes the submitted Change Set failed;
	// publication evaluates the current version too.
	policy.Rules = append(policy.Rules, agentpolicy.Rule{ID: "no-agent-page", Subject: agentpolicy.Subject{Agent: "*"}, Deny: []string{"design.compose"}, When: &agentpolicy.When{PathPrefix: []string{"/agent"}}})
	if _, err := savePolicy(e, "p2", "freeze agent page", policy, 1); err != nil {
		t.Fatal(err)
	}
	changeset, err = e.Q.GetChangeset(ctx, store.GetChangesetParams{ID: cs, ProjectID: h.ProjectID})
	if err != nil || changeset.State != "failed" {
		t.Fatal("refusing version", changeset.State, err)
	}
	var check struct {
		AgentPolicy struct {
			Violations []map[string]any `json:"violations"`
		} `json:"agentPolicy"`
	}
	if err := e.Pool.QueryRow(ctx, `SELECT details FROM checks WHERE changeset_id=$1 AND stage='policy' ORDER BY id DESC LIMIT 1`, cs).Scan(&check); err != nil || len(check.AgentPolicy.Violations) != 1 || check.AgentPolicy.Violations[0]["ruleId"] != "no-agent-page" {
		t.Fatal("failure names the rule", check, err)
	}
	if err := workflow.RequireZoneApprovals(ctx, e.Q, changeset, "staging"); cmstest.Code(err) != "POLICY_DENIED" {
		t.Fatal("publication with the current version", err)
	}
}

func policyFixture(e *cmstest.Env, version int, body string) {
	e.Exec(`INSERT INTO agent_policy_versions(project_id,version,body,hash,reason) VALUES($1,$2,$3,'sha256:x','fixture')`, e.Admin.ProjectID, version, body)
}

// STU-423/438: the context of entity, schema, creation and route operations;
// failures of the policy tables never report success.
func TestSTU423OperationContextAndFailures(t *testing.T) {
	e, _, h, cs, doc := setupAgent(t)
	policyFixture(e, 1, `{"version":1,"rules":[
		{"id":"no-product","subject":{"agent":"*"},"deny":["content.write"],"when":{"schema":["Product"]}},
		{"id":"no-created","subject":{"agent":"*"},"deny":["design.compose"],"when":{"pathPrefix":["/created","/moved"]}}],
		"risk":[],"approvals":[]}`)
	session := delegation(t, e, h, registered(t, e), cs, 10)
	a := agentActor(t, e, session)
	entity := e.ContentFixture("entity", "Product", map[string]any{"title": "x"}, "staging")
	apply := func(key string, op map[string]any) error {
		_, err := dispatch(e, a, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": 1, "operations": []any{op}}, key, "agent edit")
		return err
	}
	for key, test := range map[string]struct {
		op        map[string]any
		rule      string
		condition string
	}{
		"fields": {map[string]any{"type": "entity.setFields", "target": entity, "payload": map[string]any{"data": map[string]any{"title": "y"}}}, "no-product", "schema=Product"},
		"create": {map[string]any{"type": "entity.create", "payload": map[string]any{"schema": "Product", "environment": "staging", "data": map[string]any{}}}, "no-product", "schema=Product"},
		"page":   {map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": "/created", "root": map[string]any{"id": "n_new", "type": "Box"}}}, "no-created", "pathPrefix=/created,/moved"},
		"route":  {map[string]any{"type": "document.setRoute", "target": doc, "payload": map[string]any{"path": "/moved"}}, "no-created", "pathPrefix=/created,/moved"},
	} {
		err := apply(key, test.op)
		var ce *commandbus.Error
		if !errors.As(err, &ce) || ce.Code != "POLICY_DENIED" || ce.Params["ruleId"] != test.rule || ce.Params["condition"] != test.condition {
			t.Fatal(key, err)
		}
	}
	if err := apply("missing", setPropsAt(uuid.New())); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal("an unknown target is reported by the operation", err)
	}
	// The built-in Agent and review read the same tables: failures stay failures.
	e.Must(h, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1}, nil)
	changeset, err := e.Q.GetChangeset(ctx, store.GetChangesetParams{ID: cs, ProjectID: h.ProjectID})
	if err != nil {
		t.Fatal(err)
	}
	e.Break("agent_policy_versions", "")
	cmstest.ExpectDBError(t, workflow.RequireZoneApprovals(ctx, e.Q, changeset, "staging"))
	cmstest.ExpectDBError(t, changes.CheckAgentRight(ctx, e.Q, a, changeset, auth.CapabilityRequest, nil, nil, ""))
	e.Break("changesets", "")
	cmstest.ExpectDBError(t, workflow.RecheckAgentPolicy(ctx, e.Q, h.ProjectID))
}

func setPropsAt(target uuid.UUID) map[string]any {
	op := setProps("n_root")
	op["target"] = target
	return op
}

// STU-434: the built-in Agent records the refusal in its step and gives the
// model only the rule and the condition.
func TestSTU434BuiltInAgentSeesTheRefusal(t *testing.T) {
	e, s, h, cs, doc := setupAgent(t)
	policyFixture(e, 1, `{"version":1,"rules":[{"id":"no-agent-page","subject":{"agent":"*"},"deny":["design.compose"],"when":{"pathPrefix":["/agent"]}}],"risk":[],"approvals":[]}`)
	id := runStart(t, e, h, cs, doc)
	var told string
	s.Provider.(*fakeProvider).fn = func(_ context.Context, m []Message) (Answer, error) {
		if len(m) == 2 {
			var call ToolCall
			call.ID, call.Type, call.Function.Name = "tool-1", "function", "update_props"
			call.Function.Arguments = string(mustJSON(map[string]any{"changesetId": cs, "documentId": doc, "expectedSeq": 1, "reason": "heading", "payload": map[string]any{"nodeId": "n_root", "set": map[string]any{"as": "section"}}}))
			return Answer{Message: Message{Role: "assistant", ToolCalls: []ToolCall{call}}, Usage: Usage{100, 20}}, nil
		}
		told = m[len(m)-1].Content
		return Answer{Message: Message{Role: "assistant", Content: "Не могу"}, Usage: Usage{100, 20}}, nil
	}
	if err := (&Worker{Service: s}).Work(ctx, &river.Job[RunArgs]{Args: RunArgs{RunID: id}}); err != nil {
		t.Fatal(err)
	}
	detail, err := s.Detail(ctx, h, id)
	if err != nil {
		t.Fatal(err)
	}
	var denial map[string]any
	for _, step := range detail.Steps {
		var data struct{ Denial map[string]any }
		_ = json.Unmarshal(step.Data, &data)
		if data.Denial != nil {
			denial = data.Denial
		}
	}
	if denial["ruleId"] != "no-agent-page" || denial["condition"] != "pathPrefix=/agent" || !strings.Contains(told, "no-agent-page") || !strings.Contains(told, "POLICY_DENIED") {
		t.Fatal("refusal in the step and for the model", denial, told)
	}
}
