// Package publishing — публикация Change Set в окружение и откат публикации
// (docs/spec/06-changes-publishing.md §7).
//
// Публикация фиксирует рабочие версии, сдвигает head и указатели published окружения в одной
// транзакции (PUB-020). Откат только переключает указатели и не создаёт версий (PUB-030).
package publishing

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/jobs"
	"github.com/qahnaarln/project-17/apps/server/internal/platform/postgres"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Item — изменение указателя объекта в публикации.
type Item struct {
	ObjectID          uuid.UUID  `json:"objectId"`
	PreviousVersionID *uuid.UUID `json:"previousVersionId"`
	CurrentVersionID  *uuid.UUID `json:"currentVersionId"`
}

// Publication — публикация в ответах API.
type Publication struct {
	ID                  uuid.UUID  `json:"id"`
	Environment         string     `json:"environment"`
	Kind                string     `json:"kind"`
	ChangesetID         *uuid.UUID `json:"changesetId"`
	SourcePublicationID *uuid.UUID `json:"sourcePublicationId"`
	ActorID             uuid.UUID  `json:"actorId"`
	Reason              *string    `json:"reason"`
	CreatedAt           time.Time  `json:"createdAt"`
	Items               []Item     `json:"items,omitempty"`
}

// Register регистрирует команды модуля.
func Register(bus *commandbus.Bus) {
	commandbus.Register(bus, commandbus.Command[publishPayload, Publication]{
		Name:   "publish",
		Right:  auth.ContentPublish,
		Handle: handlePublish,
	})
	commandbus.Register(bus, commandbus.Command[rollbackPayload, Publication]{
		Name:   "rollback",
		Right:  auth.ContentPublish,
		Handle: handleRollback,
	})
	commandbus.Register(bus, commandbus.Command[promotePayload, Publication]{
		Name:   "promote",
		Right:  auth.ContentPublish,
		Handle: handlePromote,
	})
}

type publishPayload struct {
	ChangesetID uuid.UUID `json:"changesetId"`
	Environment string    `json:"environment"`
}

func reasonPtr(ctx context.Context) *string {
	if r := commandbus.ReasonFrom(ctx); r != "" {
		return &r
	}
	return nil
}

// environment находит окружение, в которое можно публиковать (MF-031: не preview).
func environment(ctx context.Context, q *store.Queries, projectID uuid.UUID, name string) (store.Environment, error) {
	env, err := q.LockManifestEnvironment(ctx, store.LockManifestEnvironmentParams{ProjectID: projectID, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return env, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Окружение не найдено",
			fmt.Sprintf("Окружение %q не найдено", name))
	}
	if err != nil {
		return env, err
	}
	if env.Kind != "standard" {
		return env, commandbus.NewError(http.StatusUnprocessableEntity, "ENVIRONMENT_NOT_PUBLISHABLE", "В окружение нельзя публиковать",
			fmt.Sprintf("Окружение %q вида %s служит только для preview (MF-031)", name, env.Kind))
	}
	return env, nil
}

func handlePublish(ctx context.Context, tx pgx.Tx, actor auth.Actor, p publishPayload) (Publication, error) {
	q := store.New(tx)
	env, err := environment(ctx, q, actor.ProjectID, p.Environment)
	if err != nil {
		return Publication{}, err
	}
	cs, err := changes.Lock(ctx, q, actor.ProjectID, p.ChangesetID)
	if err != nil {
		return Publication{}, err
	}
	if err := changes.RequireState(cs, "Публикация", "approved"); err != nil {
		return Publication{}, err
	}
	versions, err := q.ChangesetWorkingVersions(ctx, cs.ID) // упорядочены по ID объекта
	if err != nil {
		return Publication{}, err
	}

	// Блокируем объекты в порядке ID (§7.1) и проверяем, что head не сдвинулся.
	var stale []uuid.UUID
	for _, v := range versions {
		obj, err := q.LockObject(ctx, v.ObjectID)
		if err != nil {
			return Publication{}, err
		}
		if !sameID(obj.HeadVersionID, v.BaseVersionID) {
			stale = append(stale, v.ObjectID)
		}
	}
	if len(stale) > 0 {
		return Publication{}, commandbus.NewError(http.StatusConflict, "REBASE_REQUIRED", "Требуется rebase",
			"Head объектов изменился после начала работы над Change Set").WithParams(map[string]any{"objects": stale})
	}
	check, err := validation.Load(ctx, q, actor.ProjectID, env.Name, &cs.ID)
	if err != nil {
		return Publication{}, err
	}
	// Candidate activation and schema publication are a separate atomic lifecycle package.
	// Never publish content under a candidate while leaving the active contract unchanged.
	if cs.Kind == "schema" || check.Candidate != nil {
		return Publication{}, commandbus.NewError(http.StatusConflict, "SCHEMA_PUBLICATION_NOT_READY", "Схемная публикация ещё не подключена", "Требуется атомарная активация manifest и схем")
	}
	docs := make([]validation.Document, len(versions))
	for i, v := range versions {
		docs[i] = validation.Document{ObjectID: v.ObjectID, VersionID: v.VersionID, Body: v.Body}
	}
	problems, err := check.CheckPublication(ctx, docs)
	if err != nil {
		return Publication{}, err
	}
	if err := validation.RequireValid(problems); err != nil {
		return Publication{}, err
	}

	pub, err := q.CreatePublication(ctx, store.CreatePublicationParams{
		ID: uuid.Must(uuid.NewV7()), ProjectID: actor.ProjectID, EnvironmentID: env.ID, ChangesetID: &cs.ID,
		Kind: "publish", ActorID: actor.ID, Reason: reasonPtr(ctx),
	})
	if err != nil {
		return Publication{}, err
	}
	items := make([]Item, 0, len(versions))
	for _, v := range versions {
		number, err := q.NextVersionNumber(ctx, v.ObjectID)
		if err != nil {
			return Publication{}, err
		}
		if err := q.CommitVersion(ctx, store.CommitVersionParams{ID: v.VersionID, Number: &number}); err != nil {
			return Publication{}, err
		}
		if err := setHead(ctx, q, v.ObjectID, &v.VersionID); err != nil {
			return Publication{}, err
		}
		previous, err := pointer(ctx, q, env.ID, v.ObjectID)
		if err != nil {
			return Publication{}, err
		}
		item := Item{ObjectID: v.ObjectID, PreviousVersionID: previous, CurrentVersionID: &v.VersionID}
		if err := movePointer(ctx, q, env.ID, pub.ID, item); err != nil {
			return Publication{}, err
		}
		items = append(items, item)
	}
	if err := q.MergeChangeset(ctx, cs.ID); err != nil {
		return Publication{}, err
	}
	if err := enqueuePurge(ctx, tx, actor.ProjectSlug, env.Name, items); err != nil {
		return Publication{}, err
	}
	return toPublication(pub, env.Name, items), nil
}

func sameID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// pointer — текущая опубликованная версия объекта в окружении (nil — не опубликован).
func pointer(ctx context.Context, q *store.Queries, envID, objectID uuid.UUID) (*uuid.UUID, error) {
	pp, err := q.GetPublishedPointer(ctx, store.GetPublishedPointerParams{EnvironmentID: envID, ObjectID: objectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &pp.VersionID, nil
}

// movePointer устанавливает published[env] = item.Current (nil — снять с публикации)
// и записывает элемент публикации.
func movePointer(ctx context.Context, q *store.Queries, envID, publicationID uuid.UUID, item Item) error {
	var err error
	if item.CurrentVersionID == nil {
		err = q.DeletePublishedPointer(ctx, store.DeletePublishedPointerParams{EnvironmentID: envID, ObjectID: item.ObjectID})
	} else {
		err = q.UpsertPublishedPointer(ctx, store.UpsertPublishedPointerParams{
			EnvironmentID: envID, ObjectID: item.ObjectID, VersionID: *item.CurrentVersionID, PublicationID: publicationID,
		})
	}
	if err != nil {
		return err
	}
	if err := syncRoute(ctx, q, envID, item); err != nil {
		return err
	}
	return q.AddPublicationItem(ctx, store.AddPublicationItemParams{
		PublicationID: publicationID, ObjectID: item.ObjectID,
		PreviousVersionID: item.PreviousVersionID, CurrentVersionID: item.CurrentVersionID,
	})
}

// enqueuePurge ставит в outbox purge CDN по объектам публикации и таблице маршрутов окружения
// (06 §7.1 п. 6, PUB-021): задача появится, только если транзакция публикации зафиксирована.
func enqueuePurge(ctx context.Context, tx pgx.Tx, project, env string, items []Item) error {
	keys := []string{jobs.SurrogateKey(project, env, "routes")}
	for _, it := range items {
		keys = append(keys, jobs.SurrogateKey(project, env, it.ObjectID.String()))
	}
	return jobs.Enqueue(ctx, tx, jobs.PurgeArgs{Keys: keys})
}

// syncRoute приводит маршрут объекта в окружении к пути опубликованной версии.
func syncRoute(ctx context.Context, q *store.Queries, envID uuid.UUID, item Item) error {
	if err := q.DeleteRoute(ctx, store.DeleteRouteParams{EnvironmentID: envID, ObjectID: item.ObjectID}); err != nil {
		return err
	}
	if item.CurrentVersionID == nil {
		return nil
	}
	err := q.InsertRoute(ctx, store.InsertRouteParams{EnvironmentID: envID, VersionID: *item.CurrentVersionID})
	if postgres.IsUniqueViolation(err) {
		return pathTaken(item.ObjectID, "в окружении")
	}
	return err
}

// setHead сдвигает head объекта вместе с его маршрутом.
func setHead(ctx context.Context, q *store.Queries, objectID uuid.UUID, version *uuid.UUID) error {
	err := q.SetHead(ctx, store.SetHeadParams{ID: objectID, VersionID: version})
	if postgres.IsUniqueViolation(err) {
		return pathTaken(objectID, "в head проекта")
	}
	return err
}

func pathTaken(objectID uuid.UUID, where string) error {
	return commandbus.NewError(http.StatusConflict, "PATH_TAKEN", "Маршрут занят",
		fmt.Sprintf("Маршрут страницы %s уже занят %s другой страницей (с точностью до имён параметров)", objectID, where)).
		WithParams(map[string]any{"objectId": objectID})
}

// --- rollback ------------------------------------------------------------------------

type rollbackPayload struct {
	PublicationID uuid.UUID `json:"publicationId"`
	// ResetHead — вернуть и head, если после публикации его никто не сдвигал (§7.3). По умолчанию true.
	ResetHead *bool `json:"resetHead"`
}

func handleRollback(ctx context.Context, tx pgx.Tx, actor auth.Actor, p rollbackPayload) (Publication, error) {
	q := store.New(tx)
	src, err := q.GetPublication(ctx, store.GetPublicationParams{ID: p.PublicationID, ProjectID: actor.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Publication{}, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Публикация не найдена",
			fmt.Sprintf("Публикация %s не найдена", p.PublicationID))
	}
	if err != nil {
		return Publication{}, err
	}
	if _, err := q.LockManifestEnvironment(ctx, store.LockManifestEnvironmentParams{ProjectID: actor.ProjectID, Name: src.EnvironmentName}); err != nil {
		return Publication{}, err
	}
	items, err := q.ListPublicationItems(ctx, src.ID) // упорядочены по ID объекта
	if err != nil {
		return Publication{}, err
	}

	// Откатывать можно, только если указатели всё ещё указывают на результат этой публикации.
	var superseded []uuid.UUID
	heads := make(map[uuid.UUID]*uuid.UUID, len(items)) // head не изменится: объекты заблокированы
	for _, it := range items {
		obj, err := q.LockObject(ctx, it.ObjectID)
		if err != nil {
			return Publication{}, err
		}
		heads[it.ObjectID] = obj.HeadVersionID
		current, err := pointer(ctx, q, src.EnvironmentID, it.ObjectID)
		if err != nil {
			return Publication{}, err
		}
		if !sameID(current, it.CurrentVersionID) {
			superseded = append(superseded, it.ObjectID)
		}
	}
	if len(superseded) > 0 {
		return Publication{}, commandbus.NewError(http.StatusConflict, "ROLLBACK_SUPERSEDED", "Публикация уже перекрыта",
			"Объекты этой публикации позже изменены другой публикацией; откатывайте цепочку с последней").
			WithParams(map[string]any{"objects": superseded})
	}

	pub, err := q.CreatePublication(ctx, store.CreatePublicationParams{
		ID: uuid.Must(uuid.NewV7()), ProjectID: actor.ProjectID, EnvironmentID: src.EnvironmentID,
		Kind: "rollback", SourcePublicationID: &src.ID, ActorID: actor.ID, Reason: reasonPtr(ctx),
	})
	if err != nil {
		return Publication{}, err
	}
	// Head сдвигает только publish; promote и rollback его не трогали — и их откат тоже.
	resetHead := (p.ResetHead == nil || *p.ResetHead) && src.Kind == "publish"
	out := make([]Item, 0, len(items))
	for _, it := range items {
		back := Item{ObjectID: it.ObjectID, PreviousVersionID: it.CurrentVersionID, CurrentVersionID: it.PreviousVersionID}
		if err := movePointer(ctx, q, src.EnvironmentID, pub.ID, back); err != nil {
			return Publication{}, err
		}
		if resetHead && sameID(heads[it.ObjectID], it.CurrentVersionID) {
			if err := setHead(ctx, q, it.ObjectID, it.PreviousVersionID); err != nil {
				return Publication{}, err
			}
		}
		out = append(out, back)
	}
	if err := enqueuePurge(ctx, tx, actor.ProjectSlug, src.EnvironmentName, out); err != nil {
		return Publication{}, err
	}
	return toPublication(pub, src.EnvironmentName, out), nil
}

// --- запросы -------------------------------------------------------------------------

func toPublication(p store.Publication, env string, items []Item) Publication {
	return Publication{ID: p.ID, Environment: env, Kind: p.Kind, ChangesetID: p.ChangesetID,
		SourcePublicationID: p.SourcePublicationID, ActorID: p.ActorID, Reason: p.Reason, CreatedAt: p.CreatedAt, Items: items}
}

// GetPublication — публикация с изменёнными указателями.
func GetPublication(ctx context.Context, q *store.Queries, projectID, id uuid.UUID) (Publication, error) {
	row, err := q.GetPublication(ctx, store.GetPublicationParams{ID: id, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Publication{}, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Публикация не найдена",
			fmt.Sprintf("Публикация %s не найдена", id))
	}
	if err != nil {
		return Publication{}, err
	}
	rows, err := q.ListPublicationItems(ctx, id)
	if err != nil {
		return Publication{}, err
	}
	items := make([]Item, len(rows))
	for i, r := range rows {
		items[i] = Item{ObjectID: r.ObjectID, PreviousVersionID: r.PreviousVersionID, CurrentVersionID: r.CurrentVersionID}
	}
	return toPublication(store.Publication{ID: row.ID, Kind: row.Kind, ChangesetID: row.ChangesetID,
		SourcePublicationID: row.SourcePublicationID, ActorID: row.ActorID, Reason: row.Reason, CreatedAt: row.CreatedAt},
		row.EnvironmentName, items), nil
}

// ListPublications — история публикаций проекта (новые первыми), опционально по окружению.
func ListPublications(ctx context.Context, q *store.Queries, projectID uuid.UUID, env *string) ([]Publication, error) {
	rows, err := q.ListPublications(ctx, store.ListPublicationsParams{ProjectID: projectID, Environment: env})
	if err != nil {
		return nil, err
	}
	out := make([]Publication, len(rows))
	for i, r := range rows {
		out[i] = toPublication(store.Publication{ID: r.ID, Kind: r.Kind, ChangesetID: r.ChangesetID,
			SourcePublicationID: r.SourcePublicationID, ActorID: r.ActorID, Reason: r.Reason, CreatedAt: r.CreatedAt},
			r.EnvironmentName, nil)
	}
	return out, nil
}

// GetPublishedDocument — документ в версии, опубликованной в окружении.
func GetPublishedDocument(ctx context.Context, q *store.Queries, projectID, id uuid.UUID, env string) (changes.Document, error) {
	d, err := q.GetPublishedDocument(ctx, store.GetPublishedDocumentParams{ObjectID: id, ProjectID: projectID, Name: env})
	if errors.Is(err, pgx.ErrNoRows) {
		return changes.Document{}, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Документ не опубликован",
			fmt.Sprintf("Документ %s не опубликован в окружении %q", id, env))
	}
	if err != nil {
		return changes.Document{}, err
	}
	return changes.Document{ID: d.ID, Kind: d.DocKind, VersionID: d.VersionID, State: d.State, Path: d.Path, Body: d.Body}, nil
}

// Route — маршрут опубликованной страницы окружения.
type Route struct {
	Path      string    `json:"path"`
	ObjectID  uuid.UUID `json:"objectId"`
	VersionID uuid.UUID `json:"versionId"`
}

// ListRoutes — таблица маршрутов окружения (по пути).
func ListRoutes(ctx context.Context, q *store.Queries, projectID uuid.UUID, env string) ([]Route, error) {
	rows, err := q.ListRoutes(ctx, store.ListRoutesParams{ProjectID: projectID, Name: env})
	if err != nil {
		return nil, err
	}
	out := make([]Route, len(rows))
	for i, r := range rows {
		out[i] = Route{Path: r.Path, ObjectID: r.ObjectID, VersionID: r.VersionID}
	}
	return out, nil
}
