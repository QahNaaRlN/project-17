package changes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

func handleRedo(ctx context.Context, tx pgx.Tx, actor auth.Actor, p seqPayload) (ApplyResult, error) {
	return handleHistory(ctx, tx, actor, p, true)
}
func historyAction(ctx context.Context, q *store.Queries, id uuid.UUID, redo bool) ([]store.Operation, error) {
	var last store.Operation
	var err error
	name := "UNDO"
	if redo {
		name = "REDO"
		last, err = q.LastRedoableOperation(ctx, id)
	} else {
		last, err = q.LastUndoableOperation(ctx, id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, commandbus.NewError(http.StatusConflict, "NOTHING_TO_"+name, "Нет действия", "Нет доступного действия")
	}
	if err != nil {
		return nil, err
	}
	if last.BatchID == nil {
		return []store.Operation{last}, nil
	}
	return q.ActionOperations(ctx, store.ActionOperationsParams{ChangesetID: id, ActionID: *last.BatchID})
}
func inverseAction(rows []store.Operation, actor auth.Actor, redo bool) ([]ops.Op, error) {
	name := "UNDO"
	if redo {
		name = "REDO"
	}
	result := make([]ops.Op, len(rows))
	for i, row := range rows {
		if row.Status != "applied" || row.Inverse == nil || row.TargetObjectID == nil {
			return nil, commandbus.NewError(http.StatusConflict, name+"_NOT_SUPPORTED", "Действие не поддерживается", "Вся группа должна поддерживать обратные операции").WithParams(map[string]any{"operationId": row.ID, "type": row.Type})
		}
		if err := json.Unmarshal(row.Inverse, &result[i]); err != nil {
			return nil, err
		}
		if right, ok := OperationRight(result[i].Type); ok {
			if err := commandbus.Require(actor, right); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}
func handleHistory(ctx context.Context, tx pgx.Tx, actor auth.Actor, p seqPayload, redo bool) (ApplyResult, error) {
	q := store.New(tx)
	if err := validation.LockProjectEnvironments(ctx, q, actor.ProjectID); err != nil {
		return ApplyResult{}, err
	}
	cs, err := lockOpen(ctx, q, actor, p.ChangesetID)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := checkSeq(cs, p.ExpectedSeq); err != nil {
		return ApplyResult{}, err
	}
	if cs.HasConflicts {
		return ApplyResult{}, commandbus.NewError(http.StatusConflict, "CHANGESET_HAS_CONFLICTS", "Конфликты", "Разрешите конфликты перед отменой или повтором")
	}
	rows, err := historyAction(ctx, q, cs.ID, redo)
	if err != nil {
		return ApplyResult{}, err
	}
	inverse, err := inverseAction(rows, actor, redo)
	if err != nil {
		return ApplyResult{}, err
	}
	s := &session{ctx: ctx, q: q, actor: actor, cs: cs, docs: map[uuid.UUID]*workingDoc{}, historical: true}
	if rows[0].BatchID != nil {
		id := uuid.Must(uuid.NewV7())
		s.batchID = &id
	}
	applied := make([]AppliedOperation, 0, len(rows))
	for i, row := range rows {
		rec, err := s.applyToDocument(*row.TargetObjectID, inverse[i])
		if err != nil {
			return ApplyResult{}, operationError(i, err)
		}
		if redo {
			rec.redoOf = &row.ID
		} else {
			rec.undoOf = &row.ID
		}
		op, err := s.record(rec)
		if err != nil {
			return ApplyResult{}, err
		}
		applied = append(applied, op)
	}
	return s.finish(applied)
}

type HistoryOperation struct {
	Type   string     `json:"type"`
	Target *uuid.UUID `json:"target"`
	Status string     `json:"status"`
}
type HistoryAction struct {
	ID         uuid.UUID          `json:"id"`
	FirstSeq   int32              `json:"firstSeq"`
	LastSeq    int32              `json:"lastSeq"`
	ActorID    uuid.UUID          `json:"actorId"`
	ActorName  string             `json:"actorName"`
	CreatedAt  time.Time          `json:"createdAt"`
	Kind       string             `json:"kind"`
	Reason     *string            `json:"reason"`
	Operations []HistoryOperation `json:"operations"`
}
type History struct {
	Seq           int32           `json:"seq"`
	Items         []HistoryAction `json:"items"`
	NextBeforeSeq *int32          `json:"nextBeforeSeq"`
	CanUndo       bool            `json:"canUndo"`
	CanRedo       bool            `json:"canRedo"`
	UndoReason    *string         `json:"undoReason"`
	RedoReason    *string         `json:"redoReason"`
}

// GetHistory reads only metadata; the caller supplies a consistent read-only snapshot.
func GetHistory(ctx context.Context, q *store.Queries, actor auth.Actor, id uuid.UUID, before int32) (History, error) {
	if err := commandbus.Require(actor, auth.DesignRead); err != nil && !actor.Rights.Has(auth.ContentRead) {
		return History{}, err
	}
	cs, err := q.GetChangeset(ctx, store.GetChangesetParams{ID: id, ProjectID: actor.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return History{}, notFound("Change Set", id)
	}
	if err != nil {
		return History{}, err
	}
	result := History{Seq: cs.Seq, Items: []HistoryAction{}}
	objects, err := q.WorkQueueObjects(ctx, store.WorkQueueObjectsParams{ProjectID: actor.ProjectID, ChangesetID: id})
	if err != nil {
		return History{}, err
	}
	if len(objects) > 1000 {
		return History{}, commandbus.NewError(413, "WORK_QUEUE_LIMIT_EXCEEDED", "Слишком много объектов", "")
	}
	for _, object := range objects {
		right := auth.ContentRead
		if object.Kind == "document" {
			right = auth.DesignRead
		}
		if err := commandbus.Require(actor, right); err != nil {
			return History{}, err
		}
	}
	heads, err := q.HistoryHeads(ctx, store.HistoryHeadsParams{ChangesetID: id, BeforeSeq: before})
	if err != nil {
		return History{}, err
	}
	if len(heads) > 20 {
		cursor := heads[19].Seq
		result.NextBeforeSeq = &cursor
		heads = heads[:20]
	}
	for _, head := range heads {
		aid := head.ID
		if head.BatchID != nil {
			aid = *head.BatchID
		}
		rows, err := q.ActionOperations(ctx, store.ActionOperationsParams{ChangesetID: id, ActionID: aid})
		if err != nil {
			return History{}, err
		}
		action := HistoryAction{ID: aid, FirstSeq: rows[len(rows)-1].Seq, LastSeq: head.Seq, ActorID: head.ActorID, ActorName: head.DisplayName, CreatedAt: head.CreatedAt, Reason: head.Reason, Kind: "edit", Operations: []HistoryOperation{}}
		if head.UndoOf != nil {
			action.Kind = "undo"
		}
		if head.RedoOf != nil {
			action.Kind = "redo"
		}
		for i := len(rows) - 1; i >= 0; i-- {
			row := rows[i]
			action.Operations = append(action.Operations, HistoryOperation{Type: row.Type, Target: row.TargetObjectID, Status: row.Status})
		}
		result.Items = append(result.Items, action)
	}
	reason := func(redo bool) (*string, error) {
		var err error
		if err = RequireOwner(cs, actor); err == nil {
			err = RequireState(cs, "История", "open")
		}
		if err == nil && cs.HasConflicts {
			err = commandbus.NewError(409, "CHANGESET_HAS_CONFLICTS", "Конфликты", "")
		}
		if err == nil {
			var rows []store.Operation
			rows, err = historyAction(ctx, q, id, redo)
			if err == nil {
				_, err = inverseAction(rows, actor, redo)
			}
		}
		if err == nil {
			return nil, nil
		}
		var cerr *commandbus.Error
		if errors.As(err, &cerr) {
			return &cerr.Code, nil
		}
		return nil, err
	}
	result.UndoReason, err = reason(false)
	if err != nil {
		return History{}, err
	}
	result.RedoReason, err = reason(true)
	if err != nil {
		return History{}, err
	}
	result.CanUndo = result.UndoReason == nil
	result.CanRedo = result.RedoReason == nil
	return result, nil
}
