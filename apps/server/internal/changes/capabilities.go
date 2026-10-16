package changes

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// ApplyInTx uses the ordinary operation pipeline, including rights, inverses,
// version checks and history. The caller owns the transaction and lock order.
func ApplyInTx(ctx context.Context, tx pgx.Tx, actor auth.Actor, cs uuid.UUID, seq int32, operations []OperationInput) (ApplyResult, error) {
	p := applyPayload{ChangesetID: cs, ExpectedSeq: seq, Operations: operations, Group: true}
	if err := authorizeApply(actor, p); err != nil {
		return ApplyResult{}, err
	}
	if err := validateApply(p); err != nil {
		return ApplyResult{}, err
	}
	return handleApply(ctx, tx, actor, p)
}

func pendingError() error {
	return commandbus.NewError(http.StatusConflict, "CAPABILITY_PENDING_INVALID", "Заглушка недоступна", "Нужен активный запрос с этой привязкой")
}

// validatePending compares event values, allowing untouched legacy placeholders.
// Only historyAction's protected journal may restore terminal placeholders.
func (s *session) validatePending(target uuid.UUID, before, after map[string]any) error {
	if s.historical {
		return nil
	}
	nodes, _ := after["nodes"].(map[string]any)
	oldNodes, _ := before["nodes"].(map[string]any)
	for id, raw := range nodes {
		node, _ := raw.(map[string]any)
		on, _ := node["on"].(map[string]any)
		old, _ := oldNodes[id].(map[string]any)
		oldOn, _ := old["on"].(map[string]any)
		for event, chain := range on {
			if sameJSON(chain, mustJSON(oldOn[event])) {
				continue
			}
			calls, _ := chain.([]any)
			if calls == nil {
				calls = []any{chain}
			}
			for _, value := range calls {
				call, _ := value.(map[string]any)
				action, _ := call["action"].(string)
				if !strings.HasPrefix(action, "pending:") {
					continue
				}
				requestID, err := uuid.Parse(strings.TrimPrefix(action, "pending:"))
				if err != nil {
					return pendingError()
				}
				r, err := s.q.GetCapabilityRequest(s.ctx, store.GetCapabilityRequestParams{ProjectID: s.actor.ProjectID, ID: requestID})
				if errors.Is(err, pgx.ErrNoRows) {
					return pendingError()
				}
				if err != nil {
					return err
				}
				active := r.Status == "submitted" || r.Status == "accepted" || r.Status == "in_progress" || r.Status == "pr_open"
				if !active || r.ChangesetID != s.cs.ID || r.DocumentID != target || r.NodeID != id || r.Event != event || len(calls) != 1 || len(call) != 1 {
					return pendingError()
				}
			}
		}
	}
	return nil
}
