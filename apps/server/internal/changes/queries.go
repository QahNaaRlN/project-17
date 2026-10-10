package changes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

func notFound(what string, id uuid.UUID) error {
	return commandbus.NewError(http.StatusNotFound, "NOT_FOUND", what+" не найден", fmt.Sprintf("%s %s не найден", what, id))
}

// ChangesetObject — объект, изменённый в Change Set.
type ChangesetObject struct {
	ObjectID         uuid.UUID  `json:"objectId"`
	DocKind          *string    `json:"docKind"`
	BaseVersionID    *uuid.UUID `json:"baseVersionId"`
	WorkingVersionID uuid.UUID  `json:"workingVersionId"`
}

// ChangesetDetail — Change Set с изменёнными объектами.
type ChangesetDetail struct {
	Changeset
	Objects               []ChangesetObject                   `json:"objects"`
	NeedsAttention        bool                                `json:"needsAttention"`
	ManifestDiagnostics   []validation.EnvironmentDiagnostics `json:"manifestDiagnostics"`
	CandidateManifestHash *string                             `json:"candidateManifestHash,omitempty"`
	BaseManifestID        *uuid.UUID                          `json:"baseManifestId,omitempty"`
}

// GetChangeset возвращает Change Set проекта.
func GetChangeset(ctx context.Context, q *store.Queries, projectID, id uuid.UUID) (ChangesetDetail, error) {
	cs, err := q.GetChangeset(ctx, store.GetChangesetParams{ID: id, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ChangesetDetail{}, notFound("Change Set", id)
	}
	if err != nil {
		return ChangesetDetail{}, err
	}
	rows, err := q.ListChangesetObjects(ctx, id)
	if err != nil {
		return ChangesetDetail{}, err
	}
	objects := make([]ChangesetObject, len(rows))
	for i, r := range rows {
		objects[i] = ChangesetObject{ObjectID: r.ObjectID, DocKind: r.DocKind, BaseVersionID: r.BaseVersionID, WorkingVersionID: r.WorkingVersionID}
	}
	diagnostics, attention, err := validation.Diagnostics(ctx, q, projectID, id)
	if err != nil {
		return ChangesetDetail{}, err
	}
	out := ChangesetDetail{Changeset: toChangeset(cs), Objects: objects, NeedsAttention: attention, ManifestDiagnostics: diagnostics}
	if cs.Kind == "schema" {
		candidate, err := q.ValidationCandidate(ctx, store.ValidationCandidateParams{ProjectID: projectID, ChangesetID: id})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return ChangesetDetail{}, err
		}
		if err == nil {
			out.CandidateManifestHash = &candidate.Hash
			out.BaseManifestID = candidate.BaseManifestID
		}
	}
	return out, nil
}

// ListChangesets — Change Set'ы проекта (новые первыми), опционально по состоянию.
func ListChangesets(ctx context.Context, q *store.Queries, projectID uuid.UUID, state *string) ([]Changeset, error) {
	rows, err := q.ListChangesets(ctx, store.ListChangesetsParams{ProjectID: projectID, State: state})
	if err != nil {
		return nil, err
	}
	out := make([]Changeset, len(rows))
	for i, r := range rows {
		out[i] = toChangeset(r)
	}
	return out, nil
}

// Operation — запись журнала операций.
type Operation struct {
	ID         uuid.UUID       `json:"id"`
	Seq        int32           `json:"seq"`
	ActorID    uuid.UUID       `json:"actorId"`
	Source     string          `json:"source"`
	Target     uuid.UUID       `json:"target"`
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload"`
	Before     json.RawMessage `json:"before"`
	After      json.RawMessage `json:"after"`
	Reason     *string         `json:"reason"`
	ClientOpID *string         `json:"clientOpId"`
	UndoOf     *uuid.UUID      `json:"undoOf"`
	Status     string          `json:"status"` // applied | conflict | dropped (rebase, 06 §4)
	CreatedAt  time.Time       `json:"createdAt"`
}

// ListOperations — операции Change Set с seq > afterSeq (до 500).
func ListOperations(ctx context.Context, q *store.Queries, projectID, changesetID uuid.UUID, afterSeq int32) ([]Operation, error) {
	if _, err := GetChangeset(ctx, q, projectID, changesetID); err != nil {
		return nil, err
	}
	rows, err := q.ListOperations(ctx, store.ListOperationsParams{ChangesetID: changesetID, Seq: afterSeq})
	if err != nil {
		return nil, err
	}
	out := make([]Operation, len(rows))
	for i, r := range rows {
		out[i] = Operation{ID: r.ID, Seq: r.Seq, ActorID: r.ActorID, Source: r.Source, Target: r.TargetObjectID, Status: r.Status,
			Type: r.Type, Payload: r.Payload, Before: r.Before, After: r.After, Reason: r.Reason,
			ClientOpID: r.ClientOpID, UndoOf: r.UndoOf, CreatedAt: r.CreatedAt}
	}
	return out, nil
}

// Document — документ IR: head или рабочая версия в Change Set.
type Document struct {
	ID        uuid.UUID       `json:"id"`
	Kind      *string         `json:"kind"`
	VersionID uuid.UUID       `json:"versionId"`
	State     string          `json:"state"` // working | committed
	Path      *string         `json:"path"`  // маршрут страницы (null — без маршрута)
	Body      json.RawMessage `json:"body"`
}

// GetDocument возвращает документ. С changesetID — рабочую версию в этом Change Set, а если
// документ в нём не менялся — head (CHG-002, API-040).
func GetDocument(ctx context.Context, q *store.Queries, projectID, id uuid.UUID, changesetID *uuid.UUID) (Document, error) {
	if changesetID != nil {
		if _, err := GetChangeset(ctx, q, projectID, *changesetID); err != nil {
			return Document{}, err
		}
		w, err := q.GetWorkingDocument(ctx, store.GetWorkingDocumentParams{ChangesetID: *changesetID, ObjectID: id, ProjectID: projectID})
		if err == nil {
			return Document{ID: w.ID, Kind: w.DocKind, VersionID: w.VersionID, State: w.State, Path: w.Path, Body: w.Body}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return Document{}, err
		}
	}
	h, err := q.GetHeadDocument(ctx, store.GetHeadDocumentParams{ID: id, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, notFound("Документ", id)
	}
	if err != nil {
		return Document{}, err
	}
	return Document{ID: h.ID, Kind: h.DocKind, VersionID: h.VersionID, State: h.State, Path: h.Path, Body: h.Body}, nil
}
