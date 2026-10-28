package agents

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/agentpolicy"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

func savePolicy(e *cmstest.Env, key, reason string, d any, expected int32) (PolicyVersion, error) {
	raw, _ := json.Marshal(map[string]any{"policy": d, "expectedVersion": expected})
	resp, err := e.Bus.Dispatch(context.Background(), e.Admin, commandbus.Request{Name: "set-agent-policy", IdempotencyKey: key, Payload: raw, Reason: reason})
	var out struct{ Result PolicyVersion }
	if err == nil {
		err = json.Unmarshal(resp.Body, &out)
	}
	return out.Result, err
}

var checkoutPolicy = agentpolicy.Document{Version: 1,
	Rules: []agentpolicy.Rule{{ID: "no-checkout", Subject: agentpolicy.Subject{Agent: "*"}, Deny: []string{"design.compose"}, When: &agentpolicy.When{PathPrefix: []string{"/checkout"}}},
		{ID: "prod-review", Subject: agentpolicy.Subject{Agent: "*"}, RequireApproval: []string{"content.write"}, When: &agentpolicy.When{Environment: []string{"production"}}}},
	Risk:      []agentpolicy.RiskRule{{ID: "checkout-risk", When: &agentpolicy.When{PathPrefix: []string{"/checkout"}}, Min: "high"}},
	Approvals: []agentpolicy.ApprovalRule{{ID: "agent-prod", When: &agentpolicy.When{Environment: []string{"production"}, Source: []string{"agent"}}, Require: agentpolicy.Require{Count: 1}}},
}

// STU-431/432/433: versions, conflicts, idempotent retry, the legacy payload and the dry run.
// AGT-013: set-policy creates a new policy version for later actions.
func TestSTU431PolicyVersionsAndExplain(t *testing.T) {
	e, s, h, _, _ := setupAgent(t)
	ctx := context.Background()
	if cur, err := s.Policy(ctx, e.Admin); err != nil || cur.Version != 0 || len(cur.Policy.Rules) != 0 || len(cur.Known.Environments) != 2 || len(cur.Known.Rights) == 0 || cur.Known.Agents == nil {
		t.Fatal("a new project has the empty policy", cur, err)
	}
	if _, err := s.Policy(ctx, h); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal("project.admin only", err)
	}
	if _, err := savePolicy(e, "k0", "", checkoutPolicy, 0); cmstest.Code(err) != "VALIDATION_FAILED" {
		t.Fatal("reason required", err)
	}
	v, err := savePolicy(e, "k1", "checkout freeze", checkoutPolicy, 0)
	if err != nil || v.Version != 1 || len(v.Hash) != 71 {
		t.Fatal(v, err)
	}
	if again, err := savePolicy(e, "k1", "checkout freeze", checkoutPolicy, 0); err != nil || again.Version != 1 {
		t.Fatal("idempotent retry", again, err)
	}
	if _, err := savePolicy(e, "k2", "stale", checkoutPolicy, 0); cmstest.Code(err) != "POLICY_VERSION_CONFLICT" {
		t.Fatal("stale expectedVersion", err)
	}
	bad := checkoutPolicy
	bad.Rules = []agentpolicy.Rule{{ID: "x", Subject: agentpolicy.Subject{Agent: "*"}, Deny: []string{"design.compose"}, When: &agentpolicy.When{Environment: []string{"moon"}}}}
	if _, err := savePolicy(e, "k3", "bad", bad, 1); cmstest.Code(err) != "POLICY_INVALID" {
		t.Fatal("unknown environment", err)
	}
	for _, payload := range []map[string]any{{"policy": map[string]any{"version": 1, "extra": true}, "expectedVersion": 1}, {"policy": checkoutPolicy}, {"policy": checkoutPolicy, "expectedVersion": 1, "allow": []string{"design.read"}}} {
		if err := e.Do(e.Admin, "set-agent-policy", payload, nil); cmstest.Code(err) != "POLICY_INVALID" && cmstest.Code(err) != "VALIDATION_FAILED" {
			t.Fatal("refused payload", payload, err)
		}
	}
	e.Must(e.Admin, "set-agent-policy", auth.AgentPolicy{Allow: []string{string(auth.DesignRead)}}, nil)
	versions, err := s.PolicyVersions(ctx, e.Admin)
	if err != nil || len(versions) != 2 || versions[0].Policy.Rules[0].ID != "legacy" || versions[1].Reason != "checkout freeze" || versions[1].AuthorName == "" {
		t.Fatal("history", versions, err)
	}

	explain := func(policy any, situation ExplainSituation) Explanation {
		t.Helper()
		req := ExplainRequest{Situation: situation}
		if policy != nil {
			req.Policy, _ = json.Marshal(policy)
		}
		out, err := s.Explain(ctx, e.Admin, req)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	got := explain(checkoutPolicy, ExplainSituation{Agent: "copy", Right: "design.compose", Path: "/checkout/cart", Risk: "low"})
	if got.Decision.Reason != agentpolicy.ReasonDenied || got.Decision.RuleID != "no-checkout" || got.Risk != "high" || got.RiskRule != "checkout-risk" {
		t.Fatal("deny explained", got)
	}
	got = explain(checkoutPolicy, ExplainSituation{Agent: "copy", Right: "content.write", Environment: "production"})
	if !got.Decision.Allowed || !got.Decision.Approval || got.Approvals != 1 || got.ApprovalRules[0] != "agent-prod" {
		t.Fatal("approval explained", got)
	}
	if got = explain(nil, ExplainSituation{Agent: "copy", Right: "design.compose"}); got.Decision.Reason != agentpolicy.ReasonNoRule || got.Version != 2 {
		t.Fatal("current legacy version explained", got)
	}
	if _, err := s.Explain(ctx, e.Admin, ExplainRequest{Policy: json.RawMessage(`{"version":2}`)}); cmstest.Code(err) != "POLICY_INVALID" {
		t.Fatal("invalid draft", err)
	}
	if _, err := s.Explain(ctx, e.Admin, ExplainRequest{Policy: json.RawMessage(`[`)}); cmstest.Code(err) != "POLICY_INVALID" {
		t.Fatal("broken draft", err)
	}
	if _, err := s.Explain(ctx, h, ExplainRequest{}); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal("explain is project.admin only", err)
	}
	// Failure injection: reading versions and known schemas never reports success.
	e.Exec(`INSERT INTO agent_policy_versions(project_id,version,body,hash,reason) VALUES($1,99,'{"version":1,"bad":1}','sha256:x','')`, e.Admin.ProjectID)
	if _, err := s.PolicyVersions(ctx, e.Admin); err == nil {
		t.Fatal("stored version is invalid")
	}
	if _, err := s.Explain(ctx, e.Admin, ExplainRequest{}); err == nil {
		t.Fatal("current version is invalid")
	}
	e.Exec(`DELETE FROM agent_policy_versions WHERE version=99`)
	e.Break("agent_definitions", "")
	if _, err := s.Policy(ctx, e.Admin); err == nil || cmstest.Code(err) != "" {
		t.Fatal("agent names", err)
	}
	e.Break("schema_versions", "")
	if _, err := s.Policy(ctx, e.Admin); err == nil || cmstest.Code(err) != "" {
		t.Fatal("known values of the state", err)
	}
	if _, err := s.Explain(ctx, e.Admin, ExplainRequest{Policy: json.RawMessage(`{"version":1}`)}); err == nil || cmstest.Code(err) != "" {
		t.Fatal("known values", err)
	}
	if _, err := savePolicy(e, "k4", "r", checkoutPolicy, 2); err == nil || cmstest.Code(err) != "" {
		t.Fatal("save with broken known values", err)
	}
	e.Break("environments", "")
	if _, err := s.Explain(ctx, e.Admin, ExplainRequest{Policy: json.RawMessage(`{"version":1}`)}); err == nil || cmstest.Code(err) != "" {
		t.Fatal("known environments", err)
	}
	e.Break("agent_policy_versions", "")
	for _, err := range []error{func() error { _, err := s.PolicyVersions(ctx, e.Admin); return err }(), func() error { _, err := s.Explain(ctx, e.Admin, ExplainRequest{}); return err }(), e.Do(e.Admin, "set-agent-policy", auth.AgentPolicy{}, nil)} {
		cmstest.ExpectDBError(t, err)
	}
}
