package workflow

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Resolution — решение автора по конфликту rebase (06 §4.2).
type Resolution struct {
	OperationID uuid.UUID `json:"operationId"`
	Choice      string    `json:"choice"` // mine | theirs
}

type rebasePayload struct {
	ChangesetID uuid.UUID    `json:"changesetId"`
	ExpectedSeq int32        `json:"expectedSeq"`
	Resolutions []Resolution `json:"resolutions"`
}

// RebaseOutcome — ответ rebase-changeset.
type RebaseOutcome struct {
	changes.RebaseResult
	Changeset changes.Changeset `json:"changeset"`
	Checks    []Check           `json:"checks,omitempty"` // проверки повторены для поданного Change Set
}

// MaxResolutions — предел решений в одной команде.
const MaxResolutions = 1000

func validateRebase(p rebasePayload) error {
	if len(p.Resolutions) > MaxResolutions {
		return commandbus.Validation(map[string]string{"resolutions": fmt.Sprintf("не больше %d", MaxResolutions)})
	}
	for i, r := range p.Resolutions {
		if r.Choice != changes.ResolutionMine && r.Choice != changes.ResolutionTheirs {
			return commandbus.Validation(map[string]string{fmt.Sprintf("resolutions[%d].choice", i): "mine или theirs"})
		}
	}
	return nil
}

// handleRebase переносит Change Set на текущий head (06 §4). Без конфликтов поданный
// Change Set остаётся на проверке: проверки повторяются, согласования переносятся на новое
// содержимое (CHG-040, кроме риска high). С конфликтами Change Set возвращается в open,
// согласования сбрасываются (CHG-041).
func handleRebase(ctx context.Context, tx pgx.Tx, actor auth.Actor, p rebasePayload) (RebaseOutcome, error) {
	q := store.New(tx)
	if err := validation.LockProjectEnvironments(ctx, q, actor.ProjectID); err != nil {
		return RebaseOutcome{}, err
	}
	cs, err := changes.Lock(ctx, q, actor.ProjectID, p.ChangesetID)
	if err != nil {
		return RebaseOutcome{}, err
	}
	if err := changes.RequireOwner(cs, actor); err != nil {
		return RebaseOutcome{}, err
	}
	if err := changes.RequireState(cs, "Rebase", "open", "failed", "changes_requested", "in_review", "approved"); err != nil {
		return RebaseOutcome{}, err
	}
	if cs.Seq != p.ExpectedSeq {
		return RebaseOutcome{}, commandbus.NewError(http.StatusConflict, "CHANGESET_SEQ_CONFLICT", "Change Set изменился",
			fmt.Sprintf("Ожидался seq %d, текущий %d", p.ExpectedSeq, cs.Seq))
	}
	resolutions := make(map[uuid.UUID]string, len(p.Resolutions))
	for _, r := range p.Resolutions {
		resolutions[r.OperationID] = r.Choice
	}
	res, err := changes.Rebase(ctx, q, cs, resolutions, actor)
	if err != nil {
		return RebaseOutcome{}, err
	}
	submitted := cs.State != "open"
	nothing := len(res.Rebased) == 0 && len(res.Removed) == 0 && len(res.Conflicts) == 0
	cs.HasConflicts = len(res.Conflicts) > 0
	out := RebaseOutcome{RebaseResult: res}

	if cs.HasConflicts {
		if submitted {
			if err := reopen(ctx, q, &cs); err != nil {
				return RebaseOutcome{}, err
			}
		}
	} else if submitted && !nothing {
		objects, err := q.ListChangesetObjects(ctx, cs.ID)
		if err != nil {
			return RebaseOutcome{}, err
		}
		if len(objects) == 0 {
			// Все операции исключены — подавать нечего, Change Set возвращается в работу.
			err = reopen(ctx, q, &cs)
		} else {
			out.Checks, err = reevaluate(ctx, q, actor.ProjectID, &cs)
		}
		if err != nil {
			return RebaseOutcome{}, err
		}
	}
	out.Changeset = changes.ToChangeset(cs)
	return out, nil
}

// reopen возвращает поданный Change Set в работу со сбросом согласований.
func reopen(ctx context.Context, q *store.Queries, cs *store.Changeset) error {
	if err := q.InvalidateApprovals(ctx, cs.ID); err != nil {
		return err
	}
	if err := q.SetChangesetState(ctx, store.SetChangesetStateParams{ID: cs.ID, State: "open"}); err != nil {
		return err
	}
	cs.State = "open"
	return nil
}

// reevaluate повторяет проверки поданного Change Set после rebase и пересчитывает состояние.
func reevaluate(ctx context.Context, q *store.Queries, projectID uuid.UUID, cs *store.Changeset) ([]Check, error) {
	ev, err := evaluate(ctx, q, projectID, cs.ID)
	if err != nil {
		return nil, err
	}
	if ev.risk == RiskHigh {
		err = q.InvalidateApprovals(ctx, cs.ID)
	} else {
		err = q.RebindApprovals(ctx, store.RebindApprovalsParams{ChangesetID: cs.ID, OldHash: cs.ContentHash, NewHash: ev.hash})
	}
	if err != nil {
		return nil, err
	}
	status, err := coverage(ctx, q, projectID, cs.ID, ev.hash, ev.roles)
	if err != nil {
		return nil, err
	}
	state := cs.State
	if failed(ev.checks) {
		state = "failed"
	} else if state != "changes_requested" {
		state = "in_review"
		if status.count >= ev.required && len(status.missing) == 0 {
			state = "approved"
		}
	}
	if err := q.SubmitChangeset(ctx, store.SubmitChangesetParams{ID: cs.ID, State: state, Risk: &ev.risk, ContentHash: ev.hash, Targets: cs.Targets}); err != nil {
		return nil, err
	}
	cs.State, cs.Risk, cs.ContentHash = state, &ev.risk, ev.hash
	return ev.checks, nil
}
