package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"net/http"
	"slices"
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
	if len(requirements.Roles) == 0 {
		return nil
	}
	status, err := coverage(ctx, q, cs.ProjectID, cs.ID, cs.ContentHash, requirements.Roles)
	if err != nil {
		return err
	}
	policy, err := LoadApprovalPolicy(ctx, q, cs.ProjectID)
	if err != nil {
		return err
	}
	risk := deref(cs.Risk)
	if requirements.Strict {
		risk = RiskHigh
	}
	required := zoneRequired(policy.Required(risk), requirements.Roles)
	if len(status.missing) > 0 || status.count < required {
		return commandbus.NewError(http.StatusConflict, "APPROVAL_ROLES_MISSING", "Требуется согласование защищённых зон", "Проверьте действующие роли согласующих").WithParams(map[string]any{"requiredRoles": requirements.Roles, "missingRequiredRoles": status.missing, "approvals": status.count, "requiredApprovals": required})
	}
	return nil
}
