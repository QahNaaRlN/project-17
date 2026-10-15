package publishing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/reviewdiff"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

func historyError(status int, code string) error {
	return commandbus.NewError(status, code, "История публикаций недоступна", "")
}
func historySize(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(raw) > reviewdiff.MaxBytes {
		return historyError(413, "PUBLICATION_HISTORY_LIMIT")
	}
	return nil
}
func currentHistoryActor(ctx context.Context, q *store.Queries, a auth.Actor) (auth.Actor, error) {
	caps, err := q.ActorCapabilities(ctx, store.ActorCapabilitiesParams{ProjectID: a.ProjectID, ActorID: a.ID})
	if err != nil {
		return a, err
	}
	current := auth.EffectiveRights(caps, []string{auth.ScopeAll})
	rights := auth.RightSet{}
	for r, allowed := range a.Rights {
		if allowed && current.Has(r) {
			rights[r] = true
		}
	}
	a.Rights = rights
	return a, nil
}
func AuthorizeHistory(ctx context.Context, q *store.Queries, a auth.Actor, id uuid.UUID) error {
	var err error
	a, err = currentHistoryActor(ctx, q, a)
	if err != nil {
		return err
	}
	if a.Kind == auth.ActorAgent || !a.Rights.Has(auth.DesignRead) && !a.Rights.Has(auth.ContentRead) {
		return historyError(403, "FORBIDDEN")
	}
	kinds, err := q.HistoryKinds(ctx, store.HistoryKindsParams{ProjectID: a.ProjectID, ID: id})
	if err != nil {
		return err
	}
	for _, kind := range kinds {
		right := auth.ContentRead
		if kind == "document" {
			right = auth.DesignRead
		}
		if err := commandbus.Require(a, right); err != nil {
			return err
		}
	}
	return nil
}

type HistoryEntry struct {
	Publication
	ObjectCount int32 `json:"objectCount"`
}
type HistoryList struct {
	Items      []HistoryEntry `json:"items"`
	NextCursor *string        `json:"nextCursor"`
}
type HistoryObject struct {
	ID              uuid.UUID          `json:"id"`
	Kind            string             `json:"kind"`
	DocKind         *string            `json:"docKind,omitempty"`
	Title           string             `json:"title"`
	BeforeVersionID *uuid.UUID         `json:"beforeVersionId"`
	AfterVersionID  *uuid.UUID         `json:"afterVersionId"`
	BeforePath      *string            `json:"beforePath"`
	AfterPath       *string            `json:"afterPath"`
	Summary         reviewdiff.Summary `json:"summary"`
}
type HistoryCard struct {
	Publication Publication     `json:"publication"`
	Objects     []HistoryObject `json:"objects"`
	Snapshot    string          `json:"snapshot"`
	Complete    bool            `json:"complete"`
	CanRollback bool            `json:"canRollback"`
	Blockers    []string        `json:"blockers"`
}
type HistoryPage struct {
	PublicationID uuid.UUID           `json:"publicationId"`
	ObjectID      uuid.UUID           `json:"objectId"`
	Snapshot      string              `json:"snapshot"`
	Items         []reviewdiff.Change `json:"items"`
	TotalChanges  int                 `json:"totalChanges"`
	NextCursor    *string             `json:"nextCursor"`
}
type historyCursor struct {
	Context string
	Time    time.Time
	ID      uuid.UUID
}

func signHistory(c historyCursor, key []byte) string {
	raw, _ := json.Marshal(c)
	p := base64.RawURLEncoding.EncodeToString(raw)
	m := hmac.New(sha256.New, key)
	m.Write([]byte(p))
	return p + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func parseHistory(raw, context string, key []byte) (historyCursor, error) {
	var c historyCursor
	parts := strings.Split(raw, ".")
	if len(parts) != 2 || len(raw) > 2048 {
		return c, historyError(400, "INVALID_ARGUMENT")
	}
	sig, e := base64.RawURLEncoding.DecodeString(parts[1])
	m := hmac.New(sha256.New, key)
	m.Write([]byte(parts[0]))
	if e != nil || !hmac.Equal(sig, m.Sum(nil)) {
		return c, historyError(400, "INVALID_ARGUMENT")
	}
	body, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil || json.Unmarshal(body, &c) != nil || c.Context != context || c.ID == uuid.Nil || c.Time.IsZero() {
		return c, historyError(400, "INVALID_ARGUMENT")
	}
	return c, nil
}
func ReadHistory(ctx context.Context, q *store.Queries, a auth.Actor, environment, cursor string, limit int) (HistoryList, error) {
	out := HistoryList{Items: []HistoryEntry{}}
	var actorErr error
	a, actorErr = currentHistoryActor(ctx, q, a)
	if actorErr != nil {
		return out, actorErr
	}
	if err := AuthorizeHistory(ctx, q, a, uuid.Nil); err != nil {
		return out, err
	}
	if environment == "" || limit < 1 || limit > 100 {
		return out, historyError(400, "INVALID_ARGUMENT")
	}
	env, err := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: a.ProjectID, Name: environment})
	if errors.Is(err, pgx.ErrNoRows) {
		return out, historyError(404, "NOT_FOUND")
	}
	if err != nil {
		return out, err
	}
	context := a.ProjectID.String() + ":" + a.ID.String() + ":" + environment + ":" + fmt.Sprint(limit)
	args := store.HistoryPublicationsParams{ProjectID: a.ProjectID, Environment: environment, PageLimit: int32(limit + 1)}
	if cursor != "" {
		c, err := parseHistory(cursor, context, env.PreviewKey)
		if err != nil {
			return out, err
		}
		args.BeforeTime = &c.Time
		args.BeforeID = &c.ID
	}
	rows, err := q.HistoryPublications(ctx, args)
	if err != nil {
		return out, err
	}
	for i, r := range rows {
		if i == limit {
			last := rows[i-1]
			next := signHistory(historyCursor{context, last.CreatedAt, last.ID}, env.PreviewKey)
			out.NextCursor = &next
			break
		}
		if err := AuthorizeHistory(ctx, q, a, r.ID); err != nil {
			return HistoryList{}, err
		}
		out.Items = append(out.Items, HistoryEntry{Publication: Publication{ID: r.ID, Environment: r.EnvironmentName, Kind: r.Kind, ChangesetID: r.ChangesetID, SourcePublicationID: r.SourcePublicationID, ActorID: r.ActorID, Reason: r.Reason, CreatedAt: r.CreatedAt, ManifestChanged: r.ManifestChanged, PreviousManifestID: r.PreviousManifestID, CurrentManifestID: r.CurrentManifestID}, ObjectCount: r.ObjectCount})
	}
	return out, historySize(out)
}
func historyPath(img *reviewdiff.Image) *string {
	if img == nil || img.Deleted {
		return nil
	}
	p, _ := img.Meta["path"].(*string)
	return p
}

// LoadPublicationHistory binds historical versions to the publication, never arbitrary UUIDs.
// Queries call it under a repeatable-read, read-only transaction. Guarded rollback calls
// it after acquiring the same environment locks as every publication writer.
func LoadPublicationHistory(ctx context.Context, q *store.Queries, a auth.Actor, id uuid.UUID, preview bool, wanted *uuid.UUID) (HistoryCard, []reviewdiff.Change, []byte, error) {
	out := HistoryCard{Objects: []HistoryObject{}, Blockers: []string{}, Complete: true}
	var actorErr error
	a, actorErr = currentHistoryActor(ctx, q, a)
	if actorErr != nil {
		return out, nil, nil, actorErr
	}
	details := []reviewdiff.Change{}
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
		return HistoryCard{}, nil, nil, err
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
	env, err := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: a.ProjectID, Name: p.Environment})
	if err != nil {
		return out, nil, nil, err
	}
	out.CanRollback = a.Rights.Has(auth.ContentPublish) && env.Kind == "standard" && !p.ManifestChanged && slices.Contains([]string{"publish", "promote", "rollback"}, p.Kind)
	if !out.CanRollback {
		out.Blockers = append(out.Blockers, "ROLLBACK_UI_UNSUPPORTED")
	}
	if p.ManifestChanged {
		return out, details, env.PreviewKey, nil
	}
	pointers, err := q.HistoryPointers(ctx, store.HistoryPointersParams{ProjectID: a.ProjectID, Name: p.Environment})
	if err != nil {
		return out, nil, nil, err
	}
	published := map[uuid.UUID]*uuid.UUID{}
	for _, v := range pointers {
		x := v.VersionID
		published[v.ObjectID] = &x
	}
	settings, err := q.GetProjectSettings(ctx, a.ProjectID)
	if err != nil {
		return out, nil, nil, err
	}
	var policy any
	decoder := json.NewDecoder(bytes.NewReader(settings))
	decoder.UseNumber()
	if err = decoder.Decode(&policy); err != nil {
		return out, nil, nil, err
	}
	routes, err := ListRoutes(ctx, q, a.ProjectID, p.Environment)
	if err != nil {
		return out, nil, nil, err
	}
	versions := []any{}
	restore := []validation.Document{}
	for _, r := range rows {
		beforeID, afterID := r.PreviousVersionID, r.CurrentVersionID
		if preview {
			beforeID, afterID = published[r.ObjectID], r.PreviousVersionID
		}
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
		out.Objects = append(out.Objects, HistoryObject{ID: r.ObjectID, Kind: r.Kind, DocKind: r.DocKind, Title: reviewdiff.HistoricalTitle(r.Kind, titleImage, historyPath(titleImage), r.ObjectID), BeforeVersionID: beforeID, AfterVersionID: afterID, BeforePath: historyPath(before), AfterPath: historyPath(after), Summary: summary})
		versions = append(versions, []any{r.ObjectID, before, after})
		if wanted != nil && r.ObjectID == *wanted {
			details = changes
		}
		if !sameID(published[r.ObjectID], r.CurrentVersionID) {
			out.CanRollback = false
			out.Blockers = append(out.Blockers, "ROLLBACK_SUPERSEDED:"+r.ObjectID.String())
		}
		if preview {
			d := validation.Document{ObjectID: r.ObjectID, Deleted: after == nil || after.Deleted}
			if afterID != nil {
				v, err := q.GetDiffVersion(ctx, store.GetDiffVersionParams{ProjectID: a.ProjectID, ObjectID: r.ObjectID, ID: *afterID})
				if err != nil {
					return out, nil, nil, err
				}
				d.VersionID = v.ID
				d.Body = v.Body
				d.Path = historyPath(after)
				d.Certified = v.Certified
			}
			restore = append(restore, d)
		}
	}
	historical := []any{a.ProjectID, a.ID, a.Rights.Sorted(), p, versions}
	parts := historical
	if preview {
		parts = append(parts, env.ActiveManifestID, env.Kind, policy, pointers, routes, false)
	}
	out.Snapshot = reviewdiff.Snapshot(parts)
	if preview && out.CanRollback {
		c, err := validation.LoadSnapshot(ctx, q, a.ProjectID, p.Environment, nil)
		if err != nil {
			return out, nil, nil, err
		}
		c.PreserveHead = true
		c.PublishedOnly = true
		problems, err := c.CheckPublication(ctx, restore)
		if err != nil {
			return out, nil, nil, err
		}
		if validation.HasErrors(problems) {
			out.CanRollback = false
			out.Blockers = append(out.Blockers, "VALIDATION_FAILED")
		}
		// Routes are unique per environment, including objects outside this publication.
		paths := map[string]uuid.UUID{}
		replaced := map[uuid.UUID]bool{}
		for _, d := range restore {
			replaced[d.ObjectID] = true
		}
		for _, r := range routes {
			if !replaced[r.ObjectID] {
				paths[r.Path] = r.ObjectID
			}
		}
		for _, d := range restore {
			if d.Deleted || d.Path == nil {
				continue
			}
			if old, ok := paths[*d.Path]; ok && old != d.ObjectID {
				out.CanRollback = false
				out.Blockers = append(out.Blockers, "ROUTE_CONFLICT")
			}
			paths[*d.Path] = d.ObjectID
		}
	}
	if err := historySize(out); err != nil {
		return HistoryCard{}, nil, nil, err
	}
	return out, details, env.PreviewKey, nil
}
func ReadHistoryPage(ctx context.Context, q *store.Queries, a auth.Actor, id, obj uuid.UUID, preview bool, snapshot, cursor string, limit int) (HistoryPage, error) {
	out := HistoryPage{PublicationID: id, ObjectID: obj, Items: []reviewdiff.Change{}}
	if limit < 1 || limit > 100 {
		return out, historyError(400, "INVALID_ARGUMENT")
	}
	card, changes, key, err := LoadPublicationHistory(ctx, q, a, id, preview, &obj)
	if err != nil {
		return out, err
	}
	if preview && snapshot != card.Snapshot {
		return out, historyError(409, "ROLLBACK_SNAPSHOT_STALE")
	}
	context := fmt.Sprint(a.ProjectID, ":", a.ID, ":", id, ":", obj, ":", preview, ":", card.Snapshot, ":", limit)
	n, err := reviewdiff.CursorOffset(cursor, context, key)
	if err != nil {
		return out, historyError(400, "INVALID_ARGUMENT")
	}
	if n > len(changes) {
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
