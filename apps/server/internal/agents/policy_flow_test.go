package agents

import (
	"errors"
	"slices"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/agentpolicy"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
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
