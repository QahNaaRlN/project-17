// Package agents implements delegated agents through the common command bus.
package agents

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

type Service struct {
	Pool         *pgxpool.Pool
	Bus          *commandbus.Bus
	Provider     Provider
	DefaultModel string
}
type Settings struct {
	Model             string `json:"model"`
	MonthlyTokens     int    `json:"monthlyTokens"`
	UserMonthlyTokens int    `json:"userMonthlyTokens"`
}

func (s *Service) settings(ctx context.Context, q *store.Queries, project uuid.UUID) (Settings, error) {
	raw, err := q.AgentProjectSettings(ctx, project)
	if err != nil {
		return Settings{}, err
	}
	cfg := Settings{Model: s.DefaultModel, MonthlyTokens: 1000000, UserMonthlyTokens: 100000}
	var envelope struct {
		Agent *Settings `json:"agent"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil {
		return cfg, err
	}
	if envelope.Agent != nil {
		cfg = *envelope.Agent
	}
	return cfg, nil
}
func denied() error {
	return commandbus.NewError(403, "POLICY_DENIED", "Действие агента запрещено", "Проверьте пользователя, проект и CS")
}

type registerPayload struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
type sessionPayload struct {
	TokenHash  []byte      `json:"tokenHash"`
	AgentID    uuid.UUID   `json:"agentId"`
	Changesets []uuid.UUID `json:"changesetIds"`
	Rights     []string    `json:"rights"`
	TTLSeconds int         `json:"ttlSeconds"`
	OpsQuota   int         `json:"opsQuota"`
}
type Session struct {
	ID        uuid.UUID `json:"id"`
	AgentID   uuid.UUID `json:"agentId"`
	Token     string    `json:"-"` // Kept only by the credential's client; never serialized.
	ExpiresAt time.Time `json:"expiresAt"`
}
type sessionRef struct {
	SessionID uuid.UUID `json:"sessionId"`
}

func (s *Service) Register(bus *commandbus.Bus) {
	s.Bus = bus
	commandbus.Register(bus, commandbus.Command[registerPayload, map[string]any]{Name: "register-agent", Right: auth.ProjectAdmin,
		Validate: func(p registerPayload) error {
			if strings.TrimSpace(p.Name) == "" || len([]rune(p.Name)) > 100 || len([]rune(p.Description)) > 1000 {
				return commandbus.Validation(map[string]string{"name": "1…100; description ≤1000"})
			}
			return nil
		},
		Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p registerPayload) (map[string]any, error) {
			id := uuid.New()
			_, err := tx.Exec(ctx, `INSERT INTO actors(id,kind,display_name,project_id) VALUES($1,'agent',$2,$3)`, id, p.Name, a.ProjectID)
			if err != nil {
				return nil, err
			}
			_, err = tx.Exec(ctx, `INSERT INTO agent_definitions(actor_id,project_id,name,description) VALUES($1,$2,$3,$4)`, id, a.ProjectID, p.Name, p.Description)
			return map[string]any{"id": id, "name": p.Name}, err
		}})
	commandbus.Register(bus, commandbus.Command[sessionPayload, Session]{Name: "start-agent-session", Right: auth.AgentDelegate, Validate: validateSession, Handle: s.startSession})
	commandbus.Register(bus, commandbus.Command[sessionRef, map[string]any]{Name: "end-agent-session", Right: auth.AgentDelegate,
		Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p sessionRef) (map[string]any, error) {
			session, err := store.New(tx).LockAgentSession(ctx, store.LockAgentSessionParams{ID: p.SessionID, ProjectID: a.ProjectID})
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, denied()
			}
			if err != nil {
				return nil, err
			}
			if session.StartedBy != a.ID && !a.Rights.Has(auth.ProjectAdmin) {
				return nil, denied()
			}
			_, err = tx.Exec(ctx, `UPDATE agent_sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE id=$1`, session.ID)
			return map[string]any{"id": session.ID, "revoked": true}, err
		}})
	commandbus.Register(bus, commandbus.Command[policyPayload, PolicyVersion]{Name: "set-agent-policy", Right: auth.ProjectAdmin,
		Validate: validatePolicyPayload, Handle: s.setPolicy})
	commandbus.Register(bus, commandbus.Command[Settings, Settings]{Name: "set-agent-config", Right: auth.ProjectAdmin,
		Validate: func(p Settings) error {
			if !validModel(p.Model) || p.MonthlyTokens < 1 || p.MonthlyTokens > 100000000 || p.UserMonthlyTokens < 1 || p.UserMonthlyTokens > p.MonthlyTokens {
				return commandbus.Validation(map[string]string{"model": "model required; valid monthly project/user limits"})
			}
			return nil
		},
		Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p Settings) (Settings, error) {
			raw, _ := json.Marshal(p)
			_, err := tx.Exec(ctx, `UPDATE projects SET settings=jsonb_set(settings,'{agent}',$2) WHERE id=$1`, a.ProjectID, raw)
			return p, err
		}})
	s.registerRuns(bus)
}
func validateSession(p sessionPayload) error {
	if len(p.TokenHash) != 32 || p.AgentID == uuid.Nil || p.TTLSeconds < 1 || p.TTLSeconds > 28800 || p.OpsQuota < 1 || p.OpsQuota > 1000 || len(p.Changesets) > 32 || len(p.Rights) > len(auth.AllRights) {
		return commandbus.Validation(map[string]string{"session": "SHA-256 tokenHash (base64), agentId, TTL 1…28800, quota 1…1000, ≤32 CS"})
	}
	return nil
}
func (s *Service) startSession(ctx context.Context, tx pgx.Tx, a auth.Actor, p sessionPayload) (Session, error) {
	if a.Kind != auth.ActorHuman && a.Kind != auth.ActorService {
		return Session{}, denied()
	}
	q := store.New(tx)
	var id uuid.UUID
	var name string
	if err := tx.QueryRow(ctx, `SELECT d.actor_id, a.display_name FROM agent_definitions d JOIN actors a ON a.id=d.actor_id WHERE d.actor_id=$1 AND d.project_id=$2 AND a.disabled_at IS NULL FOR SHARE OF a`, p.AgentID, a.ProjectID).Scan(&id, &name); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, denied()
		}
		return Session{}, err
	}
	// Never restore rights narrowed by the delegator's bearer token.
	policy, err := auth.PolicyCeiling(ctx, q, a.ProjectID, name)
	if err != nil {
		return Session{}, err
	}
	caps := []string{}
	for _, right := range p.Rights {
		if !policy.Has(auth.Right(right)) || !a.Rights.Has(auth.Right(right)) {
			return Session{}, denied()
		}
		caps = append(caps, right)
	}
	session := Session{ID: uuid.New(), AgentID: id, ExpiresAt: time.Now().Add(time.Duration(p.TTLSeconds) * time.Second)}
	if err = tx.QueryRow(ctx, `SELECT now()+$1*interval '1 second'`, p.TTLSeconds).Scan(&session.ExpiresAt); err != nil {
		return Session{}, err
	}
	hash := p.TokenHash
	if len(hash) == 0 {
		_, hash = auth.NewToken(auth.AgentTokenPrefix)
	} // Embedded runs never expose a credential.
	var behalf *uuid.UUID
	if a.Kind == auth.ActorHuman {
		behalf = &a.ID
	}
	_, err = tx.Exec(ctx, `INSERT INTO agent_sessions(id,project_id,agent_id,started_by,on_behalf_of,token_hash,capabilities,ops_quota,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, session.ID, a.ProjectID, id, a.ID, behalf, hash, caps, p.OpsQuota, session.ExpiresAt)
	if err != nil {
		return Session{}, err
	}
	for _, csid := range p.Changesets {
		cs, err := changes.Lock(ctx, q, a.ProjectID, csid)
		if err != nil {
			return Session{}, err
		}
		if cs.OwnerID != a.ID || cs.State != "open" || cs.Kind != "standard" {
			return Session{}, denied()
		}
		if err = q.AddAgentChangeset(ctx, store.AddAgentChangesetParams{SessionID: session.ID, ChangesetID: csid}); err != nil {
			return Session{}, err
		}
	}
	return session, nil
}
