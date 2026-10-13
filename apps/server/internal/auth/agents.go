package auth

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

const AgentTokenPrefix = "cms_agt_"

var ErrAgentPolicy = errors.New("POLICY_DENIED")
var ErrAgentQuota = errors.New("AGENT_QUOTA_EXCEEDED")

// AgentRights is an immutable ceiling, not a route to privileged rights.
var AgentRights = []Right{ContentRead, ContentWrite, DesignRead, DesignCompose, ComponentWrite, BehaviorUse, SchemaRead, CapabilityRequest}

type AgentPolicy struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}

func PolicyRights(settings []byte) RightSet {
	var p struct {
		Policy *AgentPolicy `json:"agentPolicy"`
	}
	if json.Unmarshal(settings, &p) != nil {
		return RightSet{}
	}
	all := []string{}
	for _, right := range AgentRights {
		all = append(all, string(right))
	}
	if p.Policy == nil {
		return EffectiveRights(all, []string{ScopeAll})
	}
	result := EffectiveRights(p.Policy.Allow, all)
	for _, right := range p.Policy.Deny {
		delete(result, Right(right))
	}
	return result
}
func intersect(a, b RightSet) RightSet {
	result := RightSet{}
	for right := range a {
		if a.Has(right) && b.Has(right) {
			result[right] = true
		}
	}
	return result
}

// SessionActor also serves publication validation. Closing a session does not
// erase valid provenance; current rights/policy still constrain old operations.
func SessionActor(ctx context.Context, q *store.Queries, s store.AgentSession, historical bool) (Actor, error) {
	principal, err := q.AgentPrincipal(ctx, store.AgentPrincipalParams{ID: s.AgentID, ProjectID: &s.ProjectID})
	if err != nil {
		return Actor{}, err
	}
	delegator, err := q.AgentDelegator(ctx, s.StartedBy)
	if err != nil {
		return Actor{}, err
	}
	if principal.DisabledAt != nil || delegator.DisabledAt != nil || (!historical && (s.RevokedAt != nil || !s.ExpiresAt.After(time.Now()))) {
		return Actor{}, ErrUnauthenticated
	}
	caps, err := q.ActorCapabilities(ctx, store.ActorCapabilitiesParams{ProjectID: s.ProjectID, ActorID: s.StartedBy})
	if err != nil {
		return Actor{}, err
	}
	live := EffectiveRights(caps, []string{ScopeAll})
	if !historical && !live.Has(AgentDelegate) {
		return Actor{}, ErrUnauthenticated
	}
	settings, err := q.AgentProjectSettings(ctx, s.ProjectID)
	if err != nil {
		return Actor{}, err
	}
	rights := intersect(intersect(EffectiveRights(s.Capabilities, []string{ScopeAll}), live), PolicyRights(settings))
	allowed, err := q.AgentChangesets(ctx, s.ID)
	if err != nil {
		return Actor{}, err
	}
	return Actor{ID: s.AgentID, Kind: ActorAgent, DisplayName: principal.DisplayName, ProjectID: s.ProjectID, ProjectSlug: principal.Slug,
		Rights: rights, AgentSessionID: &s.ID, OnBehalfOf: s.OnBehalfOf, DelegatorID: &s.StartedBy, AllowedChangesets: allowed}, nil
}
func AgentAllowsCS(actor Actor, id uuid.UUID) bool {
	return actor.Kind != ActorAgent || slices.Contains(actor.AllowedChangesets, id)
}

// GuardAgent runs under the session row lock in the command's transaction.
func GuardAgent(ctx context.Context, q *store.Queries, actor Actor, name, reason string, payload []byte) (Actor, store.AgentSession, int32, error) {
	if actor.AgentSessionID == nil {
		return Actor{}, store.AgentSession{}, 0, ErrUnauthenticated
	}
	s, err := q.LockAgentSession(ctx, store.LockAgentSessionParams{ID: *actor.AgentSessionID, ProjectID: actor.ProjectID})
	if err != nil {
		return Actor{}, s, 0, err
	}
	current, err := SessionActor(ctx, q, s, false)
	if err != nil {
		return Actor{}, s, 0, err
	}
	if current.ID != actor.ID {
		return Actor{}, s, 0, ErrUnauthenticated
	}
	if !slices.Contains([]string{"apply-operations", "create-changeset", "create-preview-token", "submit-changeset"}, name) || strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return current, s, 0, ErrAgentPolicy
	}
	var p struct {
		ChangesetID uuid.UUID         `json:"changesetId"`
		Operations  []json.RawMessage `json:"operations"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return current, s, 0, ErrAgentPolicy
	}
	if name != "create-changeset" && !AgentAllowsCS(current, p.ChangesetID) {
		return current, s, 0, ErrAgentPolicy
	}
	count := int32(1)
	if name == "apply-operations" {
		count = int32(len(p.Operations))
	}
	if name == "create-preview-token" {
		count = 0
	}
	return current, s, count, nil
}
