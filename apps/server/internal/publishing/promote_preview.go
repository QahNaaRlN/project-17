package publishing

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/reviewdiff"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

// PromoteObject — объект публикации в предпросмотре переноса: before — версия, опубликованная
// сейчас в цели, after — версия публикации. Skipped — в цели уже эта версия.
type PromoteObject struct {
	HistoryObject
	Skipped bool `json:"skipped"`
}

// PromoteDependency — объект, на который ссылается переносимая версия, но которого нет в цели;
// PublicationID — публикация источника, последней опубликовавшая его (STU-468).
type PromoteDependency struct {
	ObjectID      uuid.UUID  `json:"objectId"`
	PublicationID *uuid.UUID `json:"publicationId"`
}

// PromoteCard — read-only предпросмотр переноса публикации в целевое окружение (STU-462).
type PromoteCard struct {
	Publication   Publication                `json:"publication"`
	ToEnvironment string                     `json:"toEnvironment"`
	Objects       []PromoteObject            `json:"objects"`
	Snapshot      string                     `json:"snapshot"`
	CanPromote    bool                       `json:"canPromote"`
	Blockers      []string                   `json:"blockers"`
	Diagnostics   map[string][]ir.Diagnostic `json:"diagnostics"`
	Dependencies  []PromoteDependency        `json:"dependencies"`
}

func (c *PromoteCard) block(code string) {
	c.CanPromote = false
	if !slices.Contains(c.Blockers, code) {
		c.Blockers = append(c.Blockers, code)
	}
}

// LoadPromotePreview вычисляет перенос виртуально: та же проверка будущего состояния цели,
// что у команды promote, без записи pointers, маршрутов, публикаций и outbox. Query вызывает его
// в read-only REPEATABLE READ транзакции, guarded promote — после блокировки окружений проекта.
func LoadPromotePreview(ctx context.Context, q *store.Queries, a auth.Actor, id uuid.UUID, target string, wanted *uuid.UUID) (PromoteCard, []reviewdiff.Change, []byte, error) {
	out := PromoteCard{ToEnvironment: target, Objects: []PromoteObject{}, Blockers: []string{}, Diagnostics: map[string][]ir.Diagnostic{}, Dependencies: []PromoteDependency{}, CanPromote: true}
	var err error
	a, err = currentHistoryActor(ctx, q, a)
	if err != nil {
		return out, nil, nil, err
	}
	row, err := q.GetPublication(ctx, store.GetPublicationParams{ProjectID: a.ProjectID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil, nil, historyError(404, "NOT_FOUND")
	}
	if err != nil {
		return out, nil, nil, err
	}
	p := toPublication(store.Publication{ID: row.ID, Kind: row.Kind, ChangesetID: row.ChangesetID, SourcePublicationID: row.SourcePublicationID, ActorID: row.ActorID, Reason: row.Reason, CreatedAt: row.CreatedAt, ManifestChanged: row.ManifestChanged, PreviousManifestID: row.PreviousManifestID, CurrentManifestID: row.CurrentManifestID}, row.EnvironmentName, nil)
	out.Publication = p
	if err := AuthorizeHistory(ctx, q, a, id); err != nil {
		return PromoteCard{}, nil, nil, err
	}
	env, err := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: a.ProjectID, Name: target})
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil, nil, historyError(404, "NOT_FOUND")
	}
	if err != nil {
		return out, nil, nil, err
	}
	rows, err := q.HistoryItems(ctx, store.HistoryItemsParams{ProjectID: a.ProjectID, ID: id})
	if err != nil {
		return out, nil, nil, err
	}
	if len(rows) > 1000 {
		return out, nil, nil, historyError(413, "PUBLICATION_HISTORY_LIMIT")
	}
	if wanted != nil && !slices.ContainsFunc(rows, func(r store.HistoryItemsRow) bool { return r.ObjectID == *wanted }) {
		return out, nil, nil, historyError(404, "NOT_FOUND")
	}
	if !a.Rights.Has(auth.ContentPublish) {
		out.block("PROMOTE_FORBIDDEN")
	}
	if env.Kind != "standard" {
		out.block("ENVIRONMENT_NOT_PUBLISHABLE")
	}
	if p.Environment == target {
		out.block("PROMOTE_SAME_ENVIRONMENT")
	}
	if p.Kind != "publish" && p.Kind != "promote" {
		out.block("PROMOTE_NOT_SUPPORTED")
	}
	if p.ManifestChanged {
		out.block("PROMOTE_SCHEMA_NOT_SUPPORTED")
	}

	pointers, err := q.HistoryPointers(ctx, store.HistoryPointersParams{ProjectID: a.ProjectID, Name: target})
	if err != nil {
		return out, nil, nil, err
	}
	published := map[uuid.UUID]*uuid.UUID{}
	for _, v := range pointers {
		x := v.VersionID
		published[v.ObjectID] = &x
	}
	routes, err := ListRoutes(ctx, q, a.ProjectID, target)
	if err != nil {
		return out, nil, nil, err
	}
	details := []reviewdiff.Change{}
	versions := []any{}
	docs := []validation.Document{}
	moved := map[uuid.UUID]*string{}
	for _, r := range rows {
		beforeID, afterID := published[r.ObjectID], r.CurrentVersionID
		before, err := reviewdiff.HistoricalImage(ctx, q, a.ProjectID, r.ObjectID, beforeID, r.SchemaName, r.Kind)
		if err != nil {
			return out, nil, nil, err
		}
		after, err := reviewdiff.HistoricalImage(ctx, q, a.ProjectID, r.ObjectID, afterID, r.SchemaName, r.Kind)
		if err != nil {
			return out, nil, nil, err
		}
		changes, summary := reviewdiff.Diff(r.Kind, before, after)
		for _, change := range changes {
			if err := historySize(change); err != nil {
				return out, nil, nil, err
			}
		}
		titleImage := after
		if titleImage == nil {
			titleImage = before
		}
		skipped := sameID(beforeID, afterID)
		out.Objects = append(out.Objects, PromoteObject{HistoryObject: HistoryObject{ID: r.ObjectID, Kind: r.Kind, DocKind: r.DocKind, Title: reviewdiff.HistoricalTitle(r.Kind, titleImage, historyPath(titleImage), r.ObjectID), BeforeVersionID: beforeID, AfterVersionID: afterID, BeforePath: historyPath(before), AfterPath: historyPath(after), Summary: summary}, Skipped: skipped})
		versions = append(versions, []any{r.ObjectID, before, after})
		if wanted != nil && r.ObjectID == *wanted {
			details = changes
		}
		if afterID == nil {
			continue
		}
		v, err := q.GetDiffVersion(ctx, store.GetDiffVersionParams{ProjectID: a.ProjectID, ObjectID: r.ObjectID, ID: *afterID})
		if err != nil {
			return out, nil, nil, err
		}
		docs = append(docs, validation.Document{ObjectID: r.ObjectID, VersionID: v.ID, Body: v.Body, Path: historyPath(after), Certified: v.Certified})
		if skipped {
			continue
		}
		if beforeID != nil {
			newer, err := isNewer(ctx, q, *beforeID, *afterID)
			if err != nil {
				return out, nil, nil, err
			}
			if newer {
				out.block("PROMOTE_OUTDATED:" + r.ObjectID.String())
				continue
			}
		}
		moved[r.ObjectID] = historyPath(after)
	}
	if len(moved) == 0 && !slices.ContainsFunc(out.Blockers, func(b string) bool { return strings.HasPrefix(b, "PROMOTE_OUTDATED:") }) {
		out.block("PROMOTE_NOTHING")
	}

	var cs *store.Changeset
	var approvals []store.Approval
	if p.ChangesetID != nil {
		c, err := q.GetChangeset(ctx, store.GetChangesetParams{ID: *p.ChangesetID, ProjectID: a.ProjectID})
		if err != nil {
			return out, nil, nil, err
		}
		cs = &c
		if approvals, err = q.ListApprovals(ctx, c.ID); err != nil {
			return out, nil, nil, err
		}
	}
	settings, err := q.GetProjectSettings(ctx, a.ProjectID)
	if err != nil {
		return out, nil, nil, err
	}
	var agentPolicy any
	if current, err := q.CurrentAgentPolicy(ctx, a.ProjectID); err == nil {
		agentPolicy = []any{current.Version, current.Hash}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return out, nil, nil, err
	}

	if out.CanPromote {
		c, err := validation.LoadSnapshot(ctx, q, a.ProjectID, target, nil)
		if err != nil {
			return out, nil, nil, err
		}
		c.PreserveHead = true
		problems, err := c.CheckPublication(ctx, docs)
		if err != nil {
			return out, nil, nil, err
		}
		if validation.HasErrors(problems) {
			out.block("VALIDATION_FAILED")
			out.Diagnostics = problems
			source := func() ([]store.PublishedPointer, error) {
				return q.HistoryPointers(ctx, store.HistoryPointersParams{ProjectID: a.ProjectID, Name: p.Environment})
			}
			if out.Dependencies, err = promoteDependencies(problems, source); err != nil {
				return out, nil, nil, err
			}
		}
		// Маршруты уникальны в окружении, включая объекты вне публикации.
		paths := map[string]uuid.UUID{}
		for _, r := range routes {
			if _, ok := moved[r.ObjectID]; !ok {
				paths[r.Path] = r.ObjectID
			}
		}
		for _, r := range rows {
			path, ok := moved[r.ObjectID]
			if !ok || path == nil {
				continue
			}
			if old, ok := paths[*path]; ok && old != r.ObjectID {
				out.block("PATH_TAKEN")
			}
			paths[*path] = r.ObjectID
		}
		if cs != nil {
			var ce *commandbus.Error
			if err := workflow.RequireZoneApprovals(ctx, q, *cs, target); errors.As(err, &ce) {
				out.block(ce.Code)
			} else if err != nil {
				return out, nil, nil, err
			}
		}
	}
	out.Snapshot = reviewdiff.Snapshot([]any{"promote", a.ProjectID, a.ID, a.Rights.Sorted(), p, versions, target, env.ActiveManifestID, env.Kind, settings, agentPolicy, cs, approvals, pointers, routes})
	if err := historySize(out); err != nil {
		return PromoteCard{}, nil, nil, err
	}
	return out, details, env.PreviewKey, nil
}

// promoteDependencies связывает диагностику CONTENT_REFERENCE_UNPUBLISHED с публикацией
// источника, которая опубликовала недостающий объект.
func promoteDependencies(problems map[string][]ir.Diagnostic, source func() ([]store.PublishedPointer, error)) ([]PromoteDependency, error) {
	out := []PromoteDependency{}
	var pointers []store.PublishedPointer
	seen := map[uuid.UUID]bool{}
	for _, ds := range problems {
		for _, d := range ds {
			if d.Code != "CONTENT_REFERENCE_UNPUBLISHED" {
				continue
			}
			id, err := uuid.Parse(fmt.Sprint(d.Params["target"]))
			if err != nil || seen[id] {
				continue
			}
			seen[id] = true
			if pointers == nil {
				if pointers, err = source(); err != nil {
					return nil, err
				}
			}
			dep := PromoteDependency{ObjectID: id}
			for _, pp := range pointers {
				if pp.ObjectID == id {
					x := pp.PublicationID
					dep.PublicationID = &x
				}
			}
			out = append(out, dep)
		}
	}
	slices.SortFunc(out, func(a, b PromoteDependency) int { return slices.Compare(a.ObjectID[:], b.ObjectID[:]) })
	return out, nil
}

// ReadPromotePage — постраничный diff «сейчас в цели → версия публикации» (STU-463).
func ReadPromotePage(ctx context.Context, q *store.Queries, a auth.Actor, id, obj uuid.UUID, target, snapshot, cursor string, limit int) (HistoryPage, error) {
	out := HistoryPage{PublicationID: id, ObjectID: obj, Items: []reviewdiff.Change{}}
	if limit < 1 || limit > 100 {
		return out, historyError(400, "INVALID_ARGUMENT")
	}
	card, changes, key, err := LoadPromotePreview(ctx, q, a, id, target, &obj)
	if err != nil {
		return out, err
	}
	if snapshot != card.Snapshot {
		return out, historyError(409, "PROMOTE_SNAPSHOT_STALE")
	}
	context := fmt.Sprint(a.ProjectID, ":", a.ID, ":", id, ":", obj, ":promote:", target, ":", card.Snapshot, ":", limit)
	n, err := reviewdiff.CursorOffset(cursor, context, key)
	if err != nil || n > len(changes) {
		return out, historyError(400, "INVALID_ARGUMENT")
	}
	out.Snapshot = card.Snapshot
	out.TotalChanges = len(changes)
	for end := n; end < len(changes) && end < n+limit; end++ {
		out.Items = append(out.Items, changes[end])
		if err := historySize(out); err != nil {
			return HistoryPage{}, err
		}
	}
	if end := n + len(out.Items); end < len(changes) {
		next := reviewdiff.SignCursor(context, end, key)
		out.NextCursor = &next
	}
	return out, nil
}
