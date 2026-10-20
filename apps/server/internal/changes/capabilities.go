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
		normalized, err := s.systemDataAndTokens(target, before, after)
		if err != nil {
			return err
		}
		return s.validateSystem(target, normalized, nodes)
	}
	if err := s.validatePlaceholders(target, oldNodes, nodes); err != nil {
		return err
	}
	if err := s.validateDataAndTokens(target, before, after); err != nil {
		return err
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

// validatePlaceholders: a component placeholder (STU-357) appears only at a
// live node anchor of an active request of that key; its marker is removed
// only once the request is fulfilled (STU-370). Removing the node is ordinary
// editing (STU-364).
func (s *session) validatePlaceholders(target uuid.UUID, before, after map[string]any) error {
	for id, raw := range after {
		node, _ := raw.(map[string]any)
		marker, _ := node["pending"].(string)
		old, _ := before[id].(map[string]any)
		previous, _ := old["pending"].(string)
		if marker == previous {
			continue
		}
		check, active := marker, true
		if marker == "" {
			check, active = previous, false
		}
		a, err := s.anchorAt(target, check, id, "")
		if err != nil {
			return err
		}
		live := a.RequestStatus == "submitted" || a.RequestStatus == "accepted" || a.RequestStatus == "in_progress" || a.RequestStatus == "pr_open"
		if a.RequestKey != node["type"] || active != live || !active && a.RequestStatus != "fulfilled" {
			return pendingError()
		}
	}
	return nil
}

// anchorAt finds the live anchor of the marker at this place; a missing one
// is a refused marker.
func (s *session) anchorAt(target uuid.UUID, marker, node, event string) (store.AnchorAtRow, error) {
	requestID, err := uuid.Parse(marker)
	if err != nil {
		return store.AnchorAtRow{}, pendingError()
	}
	a, err := s.q.AnchorAt(s.ctx, store.AnchorAtParams{ProjectID: s.actor.ProjectID, RequestID: requestID, ChangesetID: s.cs.ID, DocumentID: target, NodeID: node, Event: event})
	if errors.Is(err, pgx.ErrNoRows) {
		return a, pendingError()
	}
	return a, err
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
		node, _ := raw.(map[string]any)
		old, _ := before[id].(map[string]any)
		if marker, previous := node["pending"], old["pending"]; marker != previous {
			request, _ := previous.(string)
			a, err := s.anchorAt(target, request, id, "")
			if marker != nil || err != nil && errors.As(err, new(*commandbus.Error)) {
				return systemForbidden()
			}
			if err != nil {
				return err
			}
			if a.RequestStatus != "fulfilled" || a.RequestKey != node["type"] {
				return systemForbidden()
			}
		}
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
			name := actionOf(old[0])
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
			if a.RequestStatus != "fulfilled" || len(calls) != 1 || actionOf(calls[0]) != a.RequestKey {
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
	return []any{v}
}

// actionOf — the action name of one handler call, "" when it is not a call.
func actionOf(v any) string {
	call, _ := v.(map[string]any)
	name, _ := call["action"].(string)
	return name
}

// withoutEvents removes event handlers so the remaining nodes can be compared.
func withoutEvents(nodes map[string]any) map[string]any {
	out := make(map[string]any, len(nodes))
	for id, raw := range nodes {
		node, _ := raw.(map[string]any)
		copy := make(map[string]any, len(node))
		for k, v := range node {
			if k != "on" && k != "pending" {
				copy[k] = v
			}
		}
		out[id] = copy
	}
	return out
}
