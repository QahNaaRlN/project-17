package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/agentpolicy"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

// policyPayload — set-agent-policy: a v1 document with expectedVersion, or
// the MVP {allow, deny} kept for CLI and tests (STU-431/432).
type policyPayload struct {
	Allow           []string        `json:"allow,omitempty"`
	Deny            []string        `json:"deny,omitempty"`
	Policy          json.RawMessage `json:"policy,omitempty"`
	ExpectedVersion *int32          `json:"expectedVersion,omitempty"`
}

// PolicyVersion — an immutable version of the agent policy (STU-431).
type PolicyVersion struct {
	Version    int32                `json:"version"`
	Hash       string               `json:"hash"`
	Policy     agentpolicy.Document `json:"policy"`
	AuthorID   *uuid.UUID           `json:"authorId"`
	AuthorName string               `json:"authorName"`
	Reason     string               `json:"reason"`
	CreatedAt  time.Time            `json:"createdAt"`
}

// PolicyState — GET /agent-policy: the current version and the values the
// Studio forms offer (STU-435).
type PolicyState struct {
	PolicyVersion
	Known PolicyKnown `json:"known"`
}

// PolicyKnown — rights of the ceiling and project values of conditions.
type PolicyKnown struct {
	Rights         []string `json:"rights"`
	Environments   []string `json:"environments"`
	OperationTypes []string `json:"operationTypes"`
	Schemas        []string `json:"schemas"`
	Agents         []string `json:"agents"`
}

// ExplainSituation — the dry-run situation of STU-433.
type ExplainSituation struct {
	Agent         string `json:"agent"`
	Right         string `json:"right"`
	Environment   string `json:"environment"`
	Path          string `json:"path"`
	ZoneMode      string `json:"zoneMode"`
	ZoneID        string `json:"zoneId"`
	OperationType string `json:"operationType"`
	Schema        string `json:"schema"`
	Risk          string `json:"risk"`
}

// ExplainRequest — POST /agent-policy/explain.
type ExplainRequest struct {
	Policy    json.RawMessage  `json:"policy,omitempty"`
	Situation ExplainSituation `json:"situation"`
}

// Explanation — the decision, the matching risk and approval rules.
type Explanation struct {
	Decision      agentpolicy.Decision `json:"decision"`
	Risk          string               `json:"risk,omitempty"`
	RiskRule      string               `json:"riskRule,omitempty"`
	Approvals     int                  `json:"approvals"`
	ApprovalRoles []string             `json:"approvalRoles"`
	ApprovalRules []string             `json:"approvalRules"`
	Version       int32                `json:"version"`
}

func policyInvalid(err error) error {
	var invalid *agentpolicy.Invalid
	if errors.As(err, &invalid) {
		return commandbus.NewError(http.StatusUnprocessableEntity, "POLICY_INVALID", "Политика агентов некорректна", invalid.Error()).
			WithParams(map[string]any{"field": invalid.Field, "cause": invalid.Cause})
	}
	return err
}

func validatePolicyPayload(p policyPayload) error {
	if len(p.Policy) > 0 {
		if len(p.Allow) > 0 || len(p.Deny) > 0 || p.ExpectedVersion == nil {
			return commandbus.Validation(map[string]string{"policy": "policy with expectedVersion, without allow/deny"})
		}
		_, err := agentpolicy.Parse(p.Policy)
		return policyInvalid(err)
	}
	if len(p.Allow) > len(auth.AllRights) || len(p.Deny) > len(auth.AllRights) {
		return commandbus.Validation(map[string]string{"allow": "known rights only"})
	}
	for _, r := range append(append([]string{}, p.Allow...), p.Deny...) {
		if !auth.EffectiveRights([]string{r}, []string{auth.ScopeAll}).Has(auth.Right(r)) {
			return commandbus.Validation(map[string]string{"allow": "unknown right"})
		}
	}
	return nil
}

// known — the project values conditions may name (STU-421).
func known(ctx context.Context, q *store.Queries, project uuid.UUID, rights []string) (agentpolicy.Known, error) {
	k := agentpolicy.Known{Rights: rights, OperationTypes: changes.OperationTypes()}
	envs, err := q.ListEnvironments(ctx, project)
	if err != nil {
		return k, err
	}
	for _, env := range envs {
		k.Environments = append(k.Environments, env.Name)
	}
	k.Schemas, err = q.PolicySchemaNames(ctx, project)
	return k, err
}

func hashPolicy(d agentpolicy.Document) string {
	raw, _ := json.Marshal(d)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *Service) setPolicy(ctx context.Context, tx pgx.Tx, a auth.Actor, p policyPayload) (PolicyVersion, error) {
	q := store.New(tx)
	// Serializes versions of the project: two saves never share a number.
	if err := validation.LockProjectEnvironments(ctx, q, a.ProjectID); err != nil {
		return PolicyVersion{}, err
	}
	_, current, err := agentpolicy.Load(ctx, q, a.ProjectID)
	if err != nil {
		return PolicyVersion{}, err
	}
	d, rights := agentpolicy.Legacy(p.Allow, p.Deny), make([]string, len(auth.AllRights))
	for i, r := range auth.AllRights {
		rights[i] = string(r)
	}
	if len(p.Policy) > 0 {
		if *p.ExpectedVersion != current {
			return PolicyVersion{}, commandbus.NewError(http.StatusConflict, "POLICY_VERSION_CONFLICT", "Политика агентов изменилась",
				"Перечитайте текущую версию и повторите сохранение").WithParams(map[string]any{"expectedVersion": *p.ExpectedVersion, "version": current})
		}
		reason := strings.TrimSpace(commandbus.ReasonFrom(ctx))
		if reason == "" {
			return PolicyVersion{}, commandbus.Validation(map[string]string{"reason": "reason required"})
		}
		d, _ = agentpolicy.Parse(p.Policy)
		rights = auth.CeilingNames()
	}
	k, err := known(ctx, q, a.ProjectID, rights)
	if err != nil {
		return PolicyVersion{}, err
	}
	if err = agentpolicy.Validate(d, k); err != nil {
		return PolicyVersion{}, policyInvalid(err)
	}
	body, _ := json.Marshal(d)
	author := a.ID
	row, err := q.InsertAgentPolicyVersion(ctx, store.InsertAgentPolicyVersionParams{ProjectID: a.ProjectID, Version: current + 1, Body: body,
		Hash: hashPolicy(d), AuthorID: &author, Reason: strings.TrimSpace(commandbus.ReasonFrom(ctx))})
	if err != nil {
		return PolicyVersion{}, err
	}
	if err = workflow.RecheckAgentPolicy(ctx, q, a.ProjectID); err != nil {
		return PolicyVersion{}, err
	}
	return PolicyVersion{Version: row.Version, Hash: row.Hash, Policy: d, AuthorID: row.AuthorID, Reason: row.Reason, CreatedAt: row.CreatedAt}, nil
}

// Policy — GET /agent-policy: the current version (STU-431).
func (s *Service) Policy(ctx context.Context, a auth.Actor) (PolicyState, error) {
	versions, err := s.PolicyVersions(ctx, a)
	if err != nil {
		return PolicyState{}, err
	}
	out := PolicyState{PolicyVersion: PolicyVersion{Policy: agentpolicy.Legacy(nil, nil)}}
	if len(versions) > 0 {
		out.PolicyVersion = versions[0]
	}
	q := store.New(s.Pool)
	k, err := known(ctx, q, a.ProjectID, auth.CeilingNames())
	if err != nil {
		return PolicyState{}, err
	}
	agents, err := q.PolicyAgentNames(ctx, a.ProjectID)
	out.Known = PolicyKnown{Rights: k.Rights, Environments: append([]string{}, k.Environments...), OperationTypes: k.OperationTypes, Schemas: append([]string{}, k.Schemas...), Agents: append([]string{}, agents...)}
	return out, err
}

// PolicyVersions — GET /agent-policy/versions, newest first (STU-431).
func (s *Service) PolicyVersions(ctx context.Context, a auth.Actor) ([]PolicyVersion, error) {
	if err := commandbus.Require(a, auth.ProjectAdmin); err != nil {
		return nil, err
	}
	rows, err := store.New(s.Pool).AgentPolicyVersions(ctx, a.ProjectID)
	if err != nil {
		return nil, err
	}
	out := make([]PolicyVersion, 0, len(rows))
	for _, r := range rows {
		d, err := agentpolicy.Parse(r.Body)
		if err != nil {
			return nil, err
		}
		out = append(out, PolicyVersion{Version: r.Version, Hash: r.Hash, Policy: d, AuthorID: r.AuthorID, AuthorName: r.AuthorName, Reason: r.Reason, CreatedAt: r.CreatedAt})
	}
	return out, nil
}

// Explain — POST /agent-policy/explain: a dry run that writes nothing (STU-433).
func (s *Service) Explain(ctx context.Context, a auth.Actor, req ExplainRequest) (Explanation, error) {
	if err := commandbus.Require(a, auth.ProjectAdmin); err != nil {
		return Explanation{}, err
	}
	q := store.New(s.Pool)
	d, version, err := agentpolicy.Load(ctx, q, a.ProjectID)
	if err != nil {
		return Explanation{}, err
	}
	if len(req.Policy) > 0 {
		if d, err = agentpolicy.Parse(req.Policy); err != nil {
			return Explanation{}, policyInvalid(err)
		}
		k, err := known(ctx, q, a.ProjectID, auth.CeilingNames())
		if err != nil {
			return Explanation{}, err
		}
		if err = agentpolicy.Validate(d, k); err != nil {
			return Explanation{}, policyInvalid(err)
		}
	}
	in := req.Situation
	situation := agentpolicy.Situation{Agent: in.Agent, Path: in.Path, ZoneMode: in.ZoneMode, ZoneID: in.ZoneID, OperationType: in.OperationType, Schema: in.Schema, Risk: in.Risk, Source: "agent"}
	if in.Environment != "" {
		situation.Environments = []string{in.Environment}
	}
	out := Explanation{Decision: agentpolicy.Decide(d, auth.CeilingNames(), in.Right, situation), Version: version}
	if in.Risk != "" {
		out.Risk, out.RiskRule = agentpolicy.MinRisk(d, situation, in.Risk)
	}
	out.Approvals, out.ApprovalRoles, out.ApprovalRules = agentpolicy.Approvals(d, situation)
	return out, nil
}
