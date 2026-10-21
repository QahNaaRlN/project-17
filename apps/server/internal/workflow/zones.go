package workflow

import (
	"context"
	"crypto/sha256"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/zonepolicy"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

type approvalCoverage struct {
	count   int
	missing []string
}

func zoneRequired(required int, roles []string) int {
	if len(roles) > 0 {
		return max(1, required)
	}
	return required
}
func agentRequired(ctx context.Context, q *store.Queries, cs uuid.UUID, required int) (int, error) {
	actors, err := q.ChangesetOperationActors(ctx, cs)
	if err != nil {
		return 0, err
	}
	for _, a := range actors {
		if a.Source == "agent" {
			return max(1, required), nil
		}
	}
	return required, nil
}
func coverage(ctx context.Context, q *store.Queries, project, cs uuid.UUID, hash []byte, roles []string) (approvalCoverage, error) {
	out := approvalCoverage{}
	if len(roles) == 0 {
		n, err := q.CountValidApprovals(ctx, store.CountValidApprovalsParams{ChangesetID: cs, ContentHash: hash})
		out.count = int(n)
		return out, err
	}
	actors, err := q.ValidApprovalActors(ctx, store.ValidApprovalActorsParams{ChangesetID: cs, ContentHash: hash})
	if err != nil {
		return out, err
	}
	covered := map[string]bool{}
	for _, id := range actors {
		caps, err := q.ActorCapabilities(ctx, store.ActorCapabilitiesParams{ProjectID: project, ActorID: id})
		if err != nil {
			return out, err
		}
		if !auth.EffectiveRights(caps, []string{auth.ScopeAll}).Has(auth.ContentPublish) {
			continue
		}
		names, err := q.ActorRoleNames(ctx, store.ActorRoleNamesParams{ProjectID: project, ActorID: id})
		if err != nil {
			return out, err
		}
		out.count++
		for _, name := range names {
			covered[name] = true
		}
	}
	for _, role := range roles {
		if !covered[role] {
			out.missing = append(out.missing, role)
		}
	}
	return out, nil
}

// bindZoneHash preserves legacy hashes for changes with no zone requirements.
func bindZoneHash(hash []byte, r zonepolicy.Result) []byte {
	if len(r.Roles) == 0 && !r.Strict {
		return hash
	}
	h := sha256.New()
	h.Write(hash)
	h.Write(mustJSON(map[string]any{"roles": r.Roles, "strict": r.Strict}))
	return h.Sum(nil)
}

// RequireZoneApprovals rechecks current human membership before any publication writes.
func RequireZoneApprovals(ctx context.Context, q *store.Queries, cs store.Changeset, target string) error {
	targets := slices.Clone(cs.Targets)
	if !slices.Contains(targets, target) {
		targets = append(targets, target)
	}
	requirements, err := validation.ZoneRequirements(ctx, q, cs.ProjectID, cs.ID, targets)
	if err != nil {
		return err
	}
	agentMinimum, err := agentRequired(ctx, q, cs.ID, 0)
	if err != nil {
		return err
	}
	versions, err := q.ChangesetWorkingVersions(ctx, cs.ID)
	if err != nil {
		return err
	}
	// STU-430: publication evaluates the current agent policy version.
	agent, err := reviewAgentPolicy(ctx, q, cs, targets, versions, deref(cs.Risk))
	if err != nil {
		return err
	}
	if len(agent.violations) > 0 {
		return commandbus.NewError(403, "POLICY_DENIED", "Политика агентов изменилась", "Текущая политика запрещает операции агента; верните CS в работу").WithParams(map[string]any{"violations": agent.violations})
	}
	if len(requirements.Roles) == 0 && !requirements.Strict && agentMinimum == 0 && agent.count == 0 && len(agent.roles) == 0 {
		return nil
	}
	if agentMinimum > 0 {
		actors, err := q.ChangesetOperationActors(ctx, cs.ID)
		if err != nil {
			return err
		}
		policy, err := policyCheck(ctx, q, cs.ProjectID, cs.ID, actors)
		if err != nil {
			return err
		}
		if policy.Status != "passed" {
			return commandbus.NewError(403, "POLICY_DENIED", "Права авторов изменились", "Повторно подайте CS с действующими правами")
		}
	}
	if string(bindZoneHash(contentHash(versions), requirements)) != string(cs.ContentHash) {
		return commandbus.NewError(http.StatusConflict, "CHANGESET_CONTENT_CHANGED", "Требования согласования изменились", "Повторно подайте Change Set с действующими требованиями зон").WithParams(map[string]any{"requiredRoles": requirements.Roles, "strict": requirements.Strict})
	}
	roles := union(requirements.Roles, agent.roles)
	status, err := coverage(ctx, q, cs.ProjectID, cs.ID, cs.ContentHash, roles)
	if err != nil {
		return err
	}
	policy, err := LoadApprovalPolicy(ctx, q, cs.ProjectID)
	if err != nil {
		return err
	}
	risk := agent.risk
	if requirements.Strict {
		risk = RiskHigh
	}
	required := zoneRequired(policy.Required(risk), roles)
	required = max(required, agentMinimum, agent.count)
	if len(status.missing) > 0 || status.count < required {
		return commandbus.NewError(http.StatusConflict, "APPROVAL_ROLES_MISSING", "Требуется согласование защищённых зон", "Проверьте действующие роли согласующих").WithParams(map[string]any{"requiredRoles": roles, "missingRequiredRoles": status.missing, "approvals": status.count, "requiredApprovals": required})
	}
	return nil
}

func union(a, b []string) []string {
	out := slices.Clone(a)
	for _, v := range b {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}
