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
	if s.actor.System {
		return s.validateSystem(target, oldNodes, nodes)
	}
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
				// STU-335: any live anchor of an active request may hold the marker.
				a, err := s.q.AnchorAt(s.ctx, store.AnchorAtParams{ProjectID: s.actor.ProjectID, RequestID: requestID, ChangesetID: s.cs.ID, DocumentID: target, NodeID: id, Event: event})
				if errors.Is(err, pgx.ErrNoRows) {
					return pendingError()
				}
				if err != nil {
					return err
				}
				active := a.RequestStatus == "submitted" || a.RequestStatus == "accepted" || a.RequestStatus == "in_progress" || a.RequestStatus == "pr_open"
				if !active || len(calls) != 1 || len(call) != 1 {
					return pendingError()
				}
			}
		}
	}
	return nil
}

func systemForbidden() error {
	return commandbus.NewError(http.StatusForbidden, "CAPABILITY_SYSTEM_OPERATION_FORBIDDEN", "Операция системы запрещена", "cms-system только заменяет заглушку выполненного запроса")
}

// validateSystem lets cms-system change exactly one thing (STU-345): an event
// holding pending:<id> of a fulfilled request at its anchor becomes a single
// call of that request's key. Any other difference is refused.
func (s *session) validateSystem(target uuid.UUID, before, after map[string]any) error {
	if !sameJSON(withoutEvents(before), mustJSON(withoutEvents(after))) {
		return systemForbidden()
	}
	for id, raw := range after {
		on, _ := raw.(map[string]any)["on"].(map[string]any)
		oldOn, _ := before[id].(map[string]any)["on"].(map[string]any)
		if len(on) != len(oldOn) {
			return systemForbidden()
		}
		for event, chain := range on {
			if sameJSON(chain, mustJSON(oldOn[event])) {
				continue
			}
			old := chainOf(oldOn[event])
			name, _ := old[0].(map[string]any)["action"].(string)
			requestID, err := uuid.Parse(strings.TrimPrefix(name, "pending:"))
			if len(old) != 1 || !strings.HasPrefix(name, "pending:") || err != nil {
				return systemForbidden()
			}
			a, err := s.q.AnchorAt(s.ctx, store.AnchorAtParams{ProjectID: s.actor.ProjectID, RequestID: requestID, ChangesetID: s.cs.ID, DocumentID: target, NodeID: id, Event: event})
			if errors.Is(err, pgx.ErrNoRows) {
				return systemForbidden()
			}
			if err != nil {
				return err
			}
			calls := chainOf(chain)
			action, _ := calls[0].(map[string]any)["action"].(string)
			if a.RequestStatus != "fulfilled" || len(calls) != 1 || action != a.RequestKey {
				return systemForbidden()
			}
		}
	}
	return nil
}

func chainOf(v any) []any {
	if calls, ok := v.([]any); ok && len(calls) > 0 {
		return calls
	}
	if v == nil {
		return []any{map[string]any{}}
	}
	return []any{v}
}

// withoutEvents removes event handlers so the remaining nodes can be compared.
func withoutEvents(nodes map[string]any) map[string]any {
	out := make(map[string]any, len(nodes))
	for id, raw := range nodes {
		node, _ := raw.(map[string]any)
		copy := make(map[string]any, len(node))
		for k, v := range node {
			if k != "on" {
				copy[k] = v
			}
		}
		out[id] = copy
	}
	return out
}
