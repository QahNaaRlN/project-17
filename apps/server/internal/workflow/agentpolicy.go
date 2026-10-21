package workflow

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/agentpolicy"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// policyReview — the current agent policy over the operations of a Change Set
// (STU-428…430): refusals of agent operations, the raised risk and approvals.
type policyReview struct {
	violations []map[string]any
	risk       string
	riskRule   string
	count      int
	roles      []string
	rules      []string
}

func reviewAgentPolicy(ctx context.Context, q *store.Queries, cs store.Changeset, targets []string, versions []store.ChangesetWorkingVersionsRow, risk string) (policyReview, error) {
	out := policyReview{risk: risk, roles: []string{}, rules: []string{}}
	d, _, err := agentpolicy.Load(ctx, q, cs.ProjectID)
	if err != nil {
		return out, err
	}
	if len(d.Rules) == 0 && len(d.Risk) == 0 && len(d.Approvals) == 0 {
		return out, nil
	}
	operations, err := q.ChangesetPolicyOperations(ctx, cs.ID)
	if err != nil {
		return out, err
	}
	raw, err := q.GetProjectSettings(ctx, cs.ProjectID)
	if err != nil {
		return out, err
	}
	settings := map[string]any{}
	_ = json.Unmarshal(raw, &settings)
	type document struct {
		path *string
		body map[string]any
	}
	docs := map[uuid.UUID]document{}
	for _, v := range versions {
		body := map[string]any{}
		_ = json.Unmarshal(v.Body, &body)
		docs[v.ObjectID] = document{v.Path, body}
	}
	situations := make([]agentpolicy.Situation, len(operations))
	for i, op := range operations {
		var doc document
		if op.TargetObjectID != nil {
			doc = docs[*op.TargetObjectID]
		}
		if doc.body != nil && doc.body["root"] == nil {
			doc.body = nil // entities and assets have no nodes
		}
		s := changes.PolicySituation(op.Type, op.Payload, doc.path, doc.body, op.SchemaName, settings)
		s.Agent, s.Environments, s.Source = op.ActorName, targets, op.Source
		situations[i] = s
		if r, rule := agentpolicy.MinRisk(d, s, out.risk); r != out.risk {
			out.risk, out.riskRule = r, rule
		}
	}
	labels := []string{}
	for i, op := range operations {
		s := situations[i]
		s.Risk = out.risk
		if op.Source == "agent" {
			right, _ := changes.OperationRight(op.Type)
			decision := agentpolicy.Decide(d, auth.CeilingNames(), string(right), s)
			// Rights outside the ceiling are reported by the rights check itself.
			if !decision.Allowed && decision.Reason != agentpolicy.ReasonOutsideCeiling {
				out.violations = append(out.violations, map[string]any{"operationId": op.ID, "operation": op.Type, "right": string(right),
					"ruleId": decision.RuleID, "condition": decision.Condition, "reason": decision.Reason})
			}
			if decision.Approval && !slices.Contains(labels, decision.RuleID) {
				labels = append(labels, decision.RuleID)
			}
		}
		count, roles, ids := agentpolicy.Approvals(d, s)
		out.count = max(out.count, count)
		for _, role := range roles {
			if !slices.Contains(out.roles, role) {
				out.roles = append(out.roles, role)
			}
		}
		for _, id := range ids {
			if !slices.Contains(out.rules, id) {
				out.rules = append(out.rules, id)
			}
		}
	}
	if len(labels) > 0 {
		err = q.AddPolicyApproval(ctx, store.AddPolicyApprovalParams{ID: cs.ID, Rules: labels})
	}
	return out, err
}

// RecheckAgentPolicy — STU-430: a new policy version re-evaluates submitted
// Change Sets with agent operations; a refusing version makes them failed.
func RecheckAgentPolicy(ctx context.Context, q *store.Queries, project uuid.UUID) error {
	ids, err := q.ReviewedAgentChangesets(ctx, project)
	if err != nil {
		return err
	}
	for _, id := range ids {
		cs, err := changes.Lock(ctx, q, project, id)
		if err != nil {
			return err
		}
		if _, err = reevaluate(ctx, q, project, &cs); err != nil {
			return err
		}
	}
	return nil
}
