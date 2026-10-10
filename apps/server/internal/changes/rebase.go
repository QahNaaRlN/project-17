package changes

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/policydoc"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Решения по конфликтам rebase (06 §4.2).
const (
	ResolutionMine   = "mine"   // переиграть операцию поверх текущего значения
	ResolutionTheirs = "theirs" // исключить операцию из Change Set
)

// Conflict — операция, которую нельзя переиграть на новой базе.
type Conflict struct {
	OperationID uuid.UUID `json:"operationId"`
	Seq         int32     `json:"seq"`
	Type        string    `json:"type"`
	Target      uuid.UUID `json:"target"`
	// Code: BEFORE_MISMATCH — значение изменено на новой базе (доступны mine и theirs);
	// OPERATION_INAPPLICABLE — операция неприменима (узел удалён и т. п., только theirs);
	// OBJECT_GONE — у объекта больше нет head (только theirs).
	Code     string `json:"code"`
	Message  string `json:"message"`
	Expected any    `json:"expected,omitempty"` // «до» операции
	Current  any    `json:"current,omitempty"`  // то же место на новой базе
}

// RebaseResult — итог переигрывания операций.
type RebaseResult struct {
	Rebased   []uuid.UUID `json:"rebased"`   // объекты, перенесённые на новый head
	Removed   []uuid.UUID `json:"removed"`   // объекты, все операции над которыми исключены
	Conflicts []Conflict  `json:"conflicts"` // непусто — Change Set не изменён, кроме отметок
}

// structural — операции, для которых предусловие — только существование узлов (§4.1, п. 3).
var structural = map[string]bool{ops.NodeInsert: true, ops.NodeRemove: true, ops.NodeMove: true}

// opPlan — что сделать с операцией после успешного rebase.
type opPlan struct {
	op     store.Operation
	status string      // applied | dropped
	replay *ops.Result // для applied: «до», «после» и обратная операция на новой базе
}

// Rebase переигрывает операции Change Set по объектам, head которых сдвинулся (06 §4.1).
// resolutions — решения по конфликтам прошлого rebase (ID операции → mine | theirs).
// При неразрешённых конфликтах рабочие версии не меняются: операции помечаются conflict,
// а у Change Set ставится hasConflicts. Вызывающий держит блокировку Change Set.
func Rebase(ctx context.Context, q *store.Queries, cs store.Changeset, resolutions map[uuid.UUID]string, actor auth.Actor) (RebaseResult, error) {
	res := RebaseResult{Rebased: []uuid.UUID{}, Removed: []uuid.UUID{}, Conflicts: []Conflict{}}
	stale, err := q.ChangesetBaseMismatches(ctx, cs.ID)
	if err != nil {
		return res, err
	}
	type objectPlan struct {
		certified bool
		id        uuid.UUID
		head      *uuid.UUID
		path      *string // маршрут на новой базе с переигранными document.setRoute
		body      map[string]any
		working   uuid.UUID
		ops       []opPlan
	}
	var plans []objectPlan
	for _, objectID := range stale {
		obj, err := q.LockObject(ctx, objectID)
		if err != nil {
			return res, err
		}
		co, err := q.GetChangesetObject(ctx, store.GetChangesetObjectParams{ChangesetID: cs.ID, ObjectID: objectID})
		if err != nil {
			return res, err
		}
		list, err := q.ObjectOperations(ctx, store.ObjectOperationsParams{ChangesetID: cs.ID, TargetObjectID: objectID})
		if err != nil {
			return res, err
		}
		plan := objectPlan{id: objectID, head: obj.HeadVersionID, working: co.WorkingVersionID}
		if obj.HeadVersionID == nil {
			for _, op := range list {
				if resolutions[op.ID] == ResolutionTheirs {
					plan.ops = append(plan.ops, opPlan{op: op, status: "dropped"})
					continue
				}
				res.Conflicts = append(res.Conflicts, Conflict{OperationID: op.ID, Seq: op.Seq, Type: op.Type, Target: objectID,
					Code: "OBJECT_GONE", Message: "У объекта больше нет head: его первая публикация откачена"})
			}
			plans = append(plans, plan)
			continue
		}
		head, err := q.GetVersion(ctx, *obj.HeadVersionID)
		if err != nil {
			return res, err
		}
		plan.body, plan.path, plan.certified = decodeBody(head.Body), head.Path, head.Certified
		dropped := map[uuid.UUID]bool{}
		for _, op := range list {
			choice := resolutions[op.ID]
			if op.UndoOf != nil && dropped[*op.UndoOf] {
				choice = ResolutionTheirs // отмена исключённой операции исключается вместе с ней
			}
			var step opPlan
			var conflict *Conflict
			before := ops.Clone(plan.body)
			if choice != ResolutionTheirs {
				if right, ok := OperationRight(op.Type); ok {
					if err := commandbus.Require(actor, right); err != nil {
						return res, err
					}
				}
			}
			if op.Type == ComponentCertify {
				if choice == ResolutionTheirs {
					step = opPlan{op: op, status: "dropped"}
				} else {
					value, err := certification(op.Payload)
					if err != nil {
						return res, err
					}
					r := certificateResult(plan.certified, value, plan.body)
					if choice != ResolutionMine && !sameJSON(r.Before, op.Before) {
						conflict = &Conflict{OperationID: op.ID, Seq: op.Seq, Type: op.Type, Code: "BEFORE_MISMATCH", Expected: decodeNullable(op.Before), Current: r.Before}
					} else {
						plan.certified = value
						step = opPlan{op: op, status: "applied", replay: &r}
					}
				}
			} else if op.Type == DocumentSetRoute {
				step, conflict = replayRoute(&plan.path, op, choice)
			} else {
				step, conflict = replay(plan.body, op, choice)
			}
			if conflict == nil && step.status == "applied" && op.Type != ComponentCertify {
				if r := policydoc.CheckEdit(before, plan.body, actor.Rights.Has(auth.DesignZonesManage)); !r.Valid {
					return res, operationError(int(op.Seq)-1, &ops.Error{Code: "POLICY_ZONES_MANAGE_REQUIRED", Message: "Rebase меняет защищённую зону", Diagnostics: r.Diagnostics})
				}
				if !sameJSON(before, mustJSON(plan.body)) && plan.body["kind"] == "component" {
					plan.certified = false
				}
			}
			if step.status == "dropped" {
				dropped[op.ID] = true
			}
			if conflict != nil {
				conflict.Target = objectID
				res.Conflicts = append(res.Conflicts, *conflict)
				continue
			}
			plan.ops = append(plan.ops, step)
		}
		plans = append(plans, plan)
	}

	if len(res.Conflicts) > 0 {
		// Отмечаем конфликты; операции, конфликтовавшие в прошлый раз, но переигранные
		// сейчас, возвращаются в applied. Рабочие версии не меняются.
		for _, p := range plans {
			for _, step := range p.ops {
				if step.op.Status == "conflict" {
					if err := q.SetOperationStatus(ctx, store.SetOperationStatusParams{ID: step.op.ID, Status: "applied"}); err != nil {
						return res, err
					}
				}
			}
		}
		for _, c := range res.Conflicts {
			if err := q.SetOperationStatus(ctx, store.SetOperationStatusParams{ID: c.OperationID, Status: "conflict"}); err != nil {
				return res, err
			}
		}
		return res, q.SetChangesetConflicts(ctx, store.SetChangesetConflictsParams{ID: cs.ID, HasConflicts: true})
	}

	for _, p := range plans {
		kept := false
		for _, step := range p.ops {
			if step.status == "dropped" {
				err = q.SetOperationStatus(ctx, store.SetOperationStatusParams{ID: step.op.ID, Status: "dropped"})
			} else {
				err = q.ReplayOperation(ctx, store.ReplayOperationParams{
					ID: step.op.ID, Before: nullableJSON(step.replay.Before), After: nullableJSON(step.replay.After),
					Inverse: mustJSON(step.replay.Inverse),
				})
				kept = true
			}
			if err != nil {
				return res, err
			}
		}
		if !kept {
			// Ни одной операции над объектом не осталось — объект выходит из Change Set.
			if err := q.RemoveChangesetObject(ctx, store.RemoveChangesetObjectParams{ChangesetID: cs.ID, ObjectID: p.id}); err != nil {
				return res, err
			}
			if err := q.DeleteWorkingVersion(ctx, p.working); err != nil {
				return res, err
			}
			res.Removed = append(res.Removed, p.id)
			continue
		}
		raw, hash := encodeBody(p.body)
		if err := q.RebaseWorkingVersion(ctx, store.RebaseWorkingVersionParams{ID: p.working, ParentVersionID: p.head, Path: p.path, Body: raw, BodyHash: hash, Certified: p.certified}); err != nil {
			return res, err
		}
		if err := q.SetChangesetObjectBase(ctx, store.SetChangesetObjectBaseParams{ChangesetID: cs.ID, ObjectID: p.id, BaseVersionID: p.head}); err != nil {
			return res, err
		}
		res.Rebased = append(res.Rebased, p.id)
	}
	versions, err := q.ChangesetWorkingVersions(ctx, cs.ID)
	if err != nil {
		return res, err
	}
	docs := make([]validation.Document, 0, len(versions))
	for _, v := range versions {
		docs = append(docs, validation.Document{ObjectID: v.ObjectID, VersionID: v.VersionID, Body: v.Body, Path: v.Path, Certified: v.Certified})
	}
	if _, err := validation.DraftWarnings(ctx, q, cs.ProjectID, cs.ID, cs.Targets, docs); err != nil {
		return res, err
	}
	return res, q.SetChangesetConflicts(ctx, store.SetChangesetConflictsParams{ID: cs.ID, HasConflicts: false})
}

// replay применяет операцию к body (на месте) по правилам §4.1 и решению по конфликту.
func replay(body map[string]any, op store.Operation, choice string) (opPlan, *Conflict) {
	conflict := func(code, message string, expected, current any) *Conflict {
		return &Conflict{OperationID: op.ID, Seq: op.Seq, Type: op.Type, Code: code, Message: message, Expected: expected, Current: current}
	}
	if choice == ResolutionTheirs {
		return opPlan{op: op, status: "dropped"}, nil
	}
	next := ops.Clone(body)
	r, err := ops.Apply(next, ops.Op{Type: op.Type, Payload: op.Payload}, nil)
	if err != nil {
		var oerr *ops.Error
		errors.As(err, &oerr)
		switch {
		case op.Type == ops.NodeRemove && oerr != nil && oerr.Code == "NODE_NOT_FOUND":
			return opPlan{op: op, status: "dropped"}, nil // узел уже удалён — операция пуста
		case oerr != nil && oerr.Code == "INDEX_OUT_OF_RANGE":
			// Вставка по индексу ограничивается длиной списка: без индекса — в конец.
			next = ops.Clone(body)
			r, err = ops.Apply(next, ops.Op{Type: op.Type, Payload: withoutIndex(op.Payload)}, nil)
		}
	}
	if err != nil {
		return opPlan{}, conflict("OPERATION_INAPPLICABLE", err.Error(), nil, nil)
	}
	empty := string(mustJSON(next)) == string(mustJSON(body)) // на новой базе уже то же значение
	if !empty && !structural[op.Type] && choice != ResolutionMine && !sameJSON(r.Before, op.Before) {
		return opPlan{}, conflict("BEFORE_MISMATCH", "Значение изменено после начала работы над Change Set",
			decodeNullable(op.Before), r.Before)
	}
	for k := range body {
		delete(body, k)
	}
	for k, v := range next {
		body[k] = v
	}
	return opPlan{op: op, status: "applied", replay: &r}, nil
}

func withoutIndex(payload json.RawMessage) json.RawMessage {
	var m map[string]any
	_ = json.Unmarshal(payload, &m)
	delete(m, "index")
	return mustJSON(m)
}

func decodeNullable(raw []byte) any {
	if raw == nil {
		return nil
	}
	return decodeAny(raw)
}

// sameJSON сравнивает значение с сохранённым JSON без учёта форматирования jsonb.
func sameJSON(v any, stored []byte) bool {
	return reflect.DeepEqual(decodeNullable(nullableJSON(v)), decodeNullable(stored))
}

// replayRoute переигрывает document.setRoute: конфликт, если маршрут на новой базе отличается
// от «до» операции (кроме случая, когда он уже равен новому значению).
func replayRoute(path **string, op store.Operation, choice string) (opPlan, *Conflict) {
	if choice == ResolutionTheirs {
		return opPlan{op: op, status: "dropped"}, nil
	}
	p, _ := decodeRoute(op.Payload) // payload проверен при применении
	current := map[string]any{"path": *path}
	empty := sameString(*path, p.Path)
	if !empty && choice != ResolutionMine && !sameJSON(current, op.Before) {
		return opPlan{}, &Conflict{OperationID: op.ID, Seq: op.Seq, Type: op.Type, Code: "BEFORE_MISMATCH",
			Message: "Маршрут изменён после начала работы над Change Set", Expected: decodeNullable(op.Before),
			Current: decodeNullable(nullableJSON(current))}
	}
	r := ops.Result{Before: current, After: map[string]any{"path": p.Path},
		Inverse: ops.Op{Type: DocumentSetRoute, Payload: mustJSON(routePayload{Path: *path})}}
	*path = p.Path
	return opPlan{op: op, status: "applied", replay: &r}, nil
}

func sameString(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
