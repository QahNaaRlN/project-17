package publishing

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

type promotePayload struct {
	PublicationID uuid.UUID `json:"publicationId"`
	ToEnvironment string    `json:"toEnvironment"`
}

// handlePromote переносит версии публикации в другое окружение (06 §7.2): указатели
// целевого окружения сдвигаются на current публикации, новых версий не создаётся.
// Перенос, который вернул бы окружение к более старой версии объекта, отклоняется
// (PROMOTE_OUTDATED): для возврата служит rollback.
func handlePromote(ctx context.Context, tx pgx.Tx, actor auth.Actor, p promotePayload) (Publication, error) {
	q := store.New(tx)
	src, err := q.GetPublication(ctx, store.GetPublicationParams{ID: p.PublicationID, ProjectID: actor.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Publication{}, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Публикация не найдена",
			fmt.Sprintf("Публикация %s не найдена", p.PublicationID))
	}
	if err != nil {
		return Publication{}, err
	}
	if src.ManifestChanged {
		return Publication{}, commandbus.NewError(http.StatusConflict, "PROMOTE_SCHEMA_NOT_SUPPORTED", "Схемная публикация привязана к окружению", "Зарегистрируйте manifest и согласуйте кандидат в целевом окружении")
	}
	if src.Kind != "publish" && src.Kind != "promote" {
		return Publication{}, commandbus.NewError(http.StatusUnprocessableEntity, "PROMOTE_NOT_SUPPORTED", "Публикацию нельзя продвинуть",
			fmt.Sprintf("Продвигаются публикации видов publish и promote; эта — %s", src.Kind))
	}
	env, err := environment(ctx, q, actor.ProjectID, p.ToEnvironment)
	if err != nil {
		return Publication{}, err
	}
	if env.ID == src.EnvironmentID {
		return Publication{}, commandbus.NewError(http.StatusUnprocessableEntity, "PROMOTE_SAME_ENVIRONMENT", "То же окружение",
			fmt.Sprintf("Публикация уже сделана в окружение %q", env.Name))
	}
	items, err := q.ListPublicationItems(ctx, src.ID) // упорядочены по ID объекта
	if err != nil {
		return Publication{}, err
	}

	var moves []Item
	var outdated []uuid.UUID
	for _, it := range items {
		if _, err := q.LockObject(ctx, it.ObjectID); err != nil {
			return Publication{}, err
		}
		current, err := pointer(ctx, q, env.ID, it.ObjectID)
		if err != nil {
			return Publication{}, err
		}
		if sameID(current, it.CurrentVersionID) {
			continue // в целевом окружении уже эта версия
		}
		if current != nil {
			newer, err := isNewer(ctx, q, *current, *it.CurrentVersionID)
			if err != nil {
				return Publication{}, err
			}
			if newer {
				outdated = append(outdated, it.ObjectID)
				continue
			}
		}
		moves = append(moves, Item{ObjectID: it.ObjectID, PreviousVersionID: current, CurrentVersionID: it.CurrentVersionID})
	}
	if len(outdated) > 0 {
		return Publication{}, commandbus.NewError(http.StatusConflict, "PROMOTE_OUTDATED", "В окружении более новые версии",
			fmt.Sprintf("В окружении %q опубликованы более новые версии объектов; продвиньте последнюю публикацию", env.Name)).
			WithParams(map[string]any{"objects": outdated})
	}
	if len(moves) == 0 {
		return Publication{}, commandbus.NewError(http.StatusConflict, "PROMOTE_NOTHING", "Нечего продвигать",
			fmt.Sprintf("В окружении %q уже опубликованы версии этой публикации", env.Name))
	}
	check, err := validation.Load(ctx, q, actor.ProjectID, env.Name, nil)
	if err != nil {
		return Publication{}, err
	}
	docs := make([]validation.Document, 0, len(items))
	for _, it := range items {
		if it.CurrentVersionID == nil {
			continue
		}
		v, err := q.GetVersion(ctx, *it.CurrentVersionID)
		if err != nil {
			return Publication{}, err
		}
		docs = append(docs, validation.Document{ObjectID: it.ObjectID, VersionID: v.ID, Body: v.Body, Path: v.Path, Certified: v.Certified})
	}
	problems, err := check.CheckPublication(ctx, docs)
	if err != nil {
		return Publication{}, err
	}
	if err := validation.RequireValid(problems); err != nil {
		return Publication{}, err
	}

	if src.ChangesetID != nil {
		cs, err := q.GetChangeset(ctx, store.GetChangesetParams{ID: *src.ChangesetID, ProjectID: actor.ProjectID})
		if err != nil {
			return Publication{}, err
		}
		if err := workflow.RequireZoneApprovals(ctx, q, cs, env.Name); err != nil {
			return Publication{}, err
		}
	}
	pub, err := q.CreatePublication(ctx, store.CreatePublicationParams{
		ID: uuid.Must(uuid.NewV7()), ProjectID: actor.ProjectID, EnvironmentID: env.ID, ChangesetID: src.ChangesetID,
		Kind: "promote", SourcePublicationID: &src.ID, ActorID: actor.ID, Reason: reasonPtr(ctx),
	})
	if err != nil {
		return Publication{}, err
	}
	for _, item := range moves {
		if err := movePointer(ctx, q, env.ID, pub.ID, item); err != nil {
			return Publication{}, err
		}
	}
	if err := enqueuePurge(ctx, tx, actor.ProjectSlug, env.Name, moves); err != nil {
		return Publication{}, err
	}
	return toPublication(pub, env.Name, moves), nil
}

// isNewer — версия a новее версии b того же объекта (по номеру фиксации).
func isNewer(ctx context.Context, q *store.Queries, a, b uuid.UUID) (bool, error) {
	va, err := q.GetVersion(ctx, a)
	if err != nil {
		return false, err
	}
	vb, err := q.GetVersion(ctx, b)
	if err != nil {
		return false, err
	}
	return *va.Number > *vb.Number, nil
}
