// Package realtime provides coalesced invalidations and advisory presence.
package realtime

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

type Scope struct {
	DocumentID  uuid.UUID `json:"documentId"`
	ChangesetID uuid.UUID `json:"changesetId"`
	Environment string    `json:"environment"`
}
type Heartbeat struct {
	Scope
	LeaseID uuid.UUID `json:"leaseId"`
}
type Leave struct {
	LeaseID uuid.UUID `json:"leaseId"`
}
type Presence struct {
	ActorID        uuid.UUID `json:"actorId"`
	ActorName      string    `json:"actorName"`
	ChangesetID    uuid.UUID `json:"changesetId"`
	ChangesetTitle string    `json:"changesetTitle"`
}

// ValidateScope is shared by streams and heartbeats. Presence confers no edit rights.
func ValidateScope(ctx context.Context, q *store.Queries, actor auth.Actor, p Scope) error {
	if err := commandbus.Require(actor, auth.DesignRead); err != nil {
		return err
	}
	if p.DocumentID == uuid.Nil || p.ChangesetID == uuid.Nil || p.Environment == "" {
		return commandbus.NewError(400, "PARAM_INVALID", "Некорректная область присутствия", "Нужны documentId, changesetId, environment")
	}
	env, err := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: actor.ProjectID, Name: p.Environment})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && env.Kind != "standard") {
		return commandbus.NewError(400, "PARAM_INVALID", "Окружение недоступно", "Нужно standard окружение проекта")
	}
	if err != nil {
		return err
	}
	cs, err := changes.GetChangeset(ctx, q, actor.ProjectID, p.ChangesetID)
	if err != nil {
		return err
	}
	if cs.Kind != "standard" || !slices.Contains(cs.Targets, p.Environment) {
		return commandbus.NewError(403, "PRESENCE_SCOPE_FORBIDDEN", "CS недоступен в окружении", "Проверьте targets")
	}
	doc, err := changes.GetDocument(ctx, q, actor.ProjectID, p.DocumentID, &p.ChangesetID)
	if err != nil {
		return err
	}
	if doc.Kind == nil || *doc.Kind != "page" || doc.Path == nil {
		return commandbus.NewError(400, "PARAM_INVALID", "Нужна страница", "Документ должен иметь маршрут")
	}
	return nil
}

func Register(bus *commandbus.Bus) {
	commandbus.Register(bus, commandbus.Command[Heartbeat, map[string]int]{
		Name: "presence-heartbeat", Right: auth.DesignRead,
		Validate: func(p Heartbeat) error { return leaseValid(p.LeaseID) },
		Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p Heartbeat) (map[string]int, error) {
			if err := ValidateScope(ctx, store.New(tx), a, p.Scope); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM presence_leases WHERE expires_at<=now()`); err != nil {
				return nil, err
			}
			// Ephemeral heartbeats should not retain a new idempotency row every
			// ten seconds forever. A retry within one minute replays as usual.
			if _, err := tx.Exec(ctx, `DELETE FROM idempotency_keys WHERE actor_id=$1
			 AND command IN ('presence-heartbeat','presence-leave') AND created_at<now()-interval '1 minute'`, a.ID); err != nil {
				return nil, err
			}
			_, err := tx.Exec(ctx, `INSERT INTO presence_leases(project_id,actor_id,lease_id,document_id,changeset_id,environment,expires_at)
    VALUES($1,$2,$3,$4,$5,$6,now()+interval '30 seconds') ON CONFLICT(project_id,actor_id,lease_id)
    DO UPDATE SET document_id=excluded.document_id,changeset_id=excluded.changeset_id,environment=excluded.environment,expires_at=excluded.expires_at`,
				a.ProjectID, a.ID, p.LeaseID, p.DocumentID, p.ChangesetID, p.Environment)
			return map[string]int{"ttlSeconds": 30}, err
		},
	})
	commandbus.Register(bus, commandbus.Command[Leave, map[string]bool]{
		Name: "presence-leave", Right: auth.DesignRead,
		Validate: func(p Leave) error { return leaseValid(p.LeaseID) },
		Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p Leave) (map[string]bool, error) {
			_, err := tx.Exec(ctx, `DELETE FROM presence_leases WHERE project_id=$1 AND actor_id=$2 AND lease_id=$3`, a.ProjectID, a.ID, p.LeaseID)
			return map[string]bool{"left": true}, err
		},
	})
}
func leaseValid(id uuid.UUID) error {
	if id == uuid.Nil {
		return commandbus.NewError(400, "PARAM_INVALID", "Нужен leaseId", "Передайте UUID вкладки")
	}
	return nil
}
