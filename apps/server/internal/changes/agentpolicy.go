package changes

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/agentpolicy"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/policydoc"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// PolicySituation — what an operation touches (STU-423): the document path,
// the effective zone of the node it names, the operation type and schema.
// Environments, agent, source and risk are set by the caller.
func PolicySituation(opType string, payload json.RawMessage, path *string, body map[string]any, schema string, settings map[string]any) agentpolicy.Situation {
	s := agentpolicy.Situation{OperationType: opType, Schema: schema}
	if path != nil {
		s.Path = *path
	}
	var named struct {
		NodeID   string  `json:"nodeId"`
		ParentID string  `json:"parentId"`
		Path     *string `json:"path"`
	}
	_ = json.Unmarshal(payload, &named)
	if (opType == DocumentCreate || opType == DocumentSetRoute) && named.Path != nil && s.Path == "" {
		s.Path = *named.Path
	}
	if body != nil {
		node := named.NodeID
		if node == "" {
			node = named.ParentID
		}
		if node == "" {
			node, _ = body["root"].(string)
		}
		if mode, zone, ok := policydoc.NodeZone(body, node, settings); ok {
			s.ZoneMode, s.ZoneID = mode.String(), zone
		}
	}
	return s
}

// PolicyDenied — 403 with the right, the rule and the condition (STU-426);
// never the text of other rules or Change Sets.
func PolicyDenied(d agentpolicy.Decision, right string, index int) error {
	params := map[string]any{"right": right, "reason": d.Reason}
	detail := "Право " + right + " вне разрешений агента"
	if d.RuleID != "" {
		params["ruleId"] = d.RuleID
		detail = "Запрещено политикой: правило " + d.RuleID
	}
	if d.Condition != "" {
		params["condition"] = d.Condition
		detail += ", " + d.Condition
	}
	if index >= 0 {
		params["operationIndex"] = index
	}
	return commandbus.NewError(http.StatusForbidden, "POLICY_DENIED", "Действие агента запрещено политикой", detail).WithParams(params)
}

// agentPolicy — the current policy loaded once per agent command.
type agentPolicy struct {
	doc       agentpolicy.Document
	settings  map[string]any
	approvals []string
}

func loadAgentPolicy(ctx context.Context, q *store.Queries, project uuid.UUID) (*agentPolicy, error) {
	d, _, err := agentpolicy.Load(ctx, q, project)
	if err != nil {
		return nil, err
	}
	raw, err := q.GetProjectSettings(ctx, project)
	if err != nil {
		return nil, err
	}
	settings := map[string]any{}
	_ = json.Unmarshal(raw, &settings)
	return &agentPolicy{doc: d, settings: settings}, nil
}

// checkAgentPolicy — STU-422/425: the operation is evaluated before it is applied.
func (s *session) checkAgentPolicy(p *agentPolicy, index int, in OperationInput) error {
	right, _ := OperationRight(in.Type)
	envs := s.cs.Targets
	var path *string
	var body map[string]any
	schema := ""
	switch {
	case in.SchemaTarget != nil:
		schema = in.SchemaTarget.SchemaName
	case in.Type == EntityCreate:
		var e struct {
			Schema      string `json:"schema"`
			Environment string `json:"environment"`
		}
		_ = json.Unmarshal(in.Payload, &e)
		schema = e.Schema
		if len(envs) == 0 && e.Environment != "" {
			envs = []string{e.Environment}
		}
	case in.Target != nil:
		obj, err := s.q.ContentIdentity(s.ctx, store.ContentIdentityParams{ProjectID: s.actor.ProjectID, ID: *in.Target})
		if err != nil {
			return nil // the operation itself reports a missing object
		}
		if obj.SchemaName != nil {
			schema = *obj.SchemaName
		}
		if obj.Kind == "document" {
			d, err := s.load(*in.Target)
			if err != nil {
				return err
			}
			path, body = d.path, d.body
		}
	}
	situations := []agentpolicy.Situation{PolicySituation(in.Type, in.Payload, path, body, schema, p.settings)}
	// A route change is checked at the old and at the new path.
	if in.Type == DocumentSetRoute && path != nil {
		situations = append(situations, PolicySituation(in.Type, in.Payload, nil, body, schema, p.settings))
	}
	for _, situation := range situations {
		situation.Agent, situation.Environments, situation.Source = s.actor.DisplayName, envs, "agent"
		d := agentpolicy.Decide(p.doc, auth.CeilingNames(), string(right), situation)
		if !d.Allowed {
			return PolicyDenied(d, string(right), index)
		}
		if d.Approval {
			p.approvals = append(p.approvals, d.RuleID)
		}
	}
	return nil
}

func (s *session) recordPolicyApproval(p *agentPolicy) error {
	if p == nil || len(p.approvals) == 0 {
		return nil
	}
	return s.q.AddPolicyApproval(s.ctx, store.AddPolicyApprovalParams{ID: s.cs.ID, Rules: p.approvals})
}

// CheckAgentRight — STU-425 for agent commands outside apply-operations: the
// right is evaluated at the document and node the command touches.
func CheckAgentRight(ctx context.Context, q *store.Queries, actor auth.Actor, cs store.Changeset, right auth.Right, path *string, body map[string]any, nodeID string) error {
	if actor.Kind != auth.ActorAgent {
		return nil
	}
	p, err := loadAgentPolicy(ctx, q, actor.ProjectID)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"nodeId": nodeID})
	situation := PolicySituation("", payload, path, body, "", p.settings)
	situation.Agent, situation.Environments, situation.Source = actor.DisplayName, cs.Targets, "agent"
	d := agentpolicy.Decide(p.doc, auth.CeilingNames(), string(right), situation)
	if !d.Allowed {
		return PolicyDenied(d, string(right), -1)
	}
	if d.Approval {
		return q.AddPolicyApproval(ctx, store.AddPolicyApprovalParams{ID: cs.ID, Rules: []string{d.RuleID}})
	}
	return nil
}
