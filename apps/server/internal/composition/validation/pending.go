package validation

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// pendingApp returns the manifest view for a document with component
// placeholders (STU-357): each placeholder of a live component request is
// checked against the request's proposed contract, as if it were a manifest
// component. Every placeholder blocks publication (STU-358).
func (c *Context) pendingApp(ctx context.Context, doc map[string]any) (any, []ir.Diagnostic, error) {
	nodes := object(doc["nodes"])
	parents := map[string]string{}
	for _, id := range sortedKeys(nodes) {
		n := object(nodes[id])
		for _, child := range childIDs(n) {
			parents[child] = id
		}
	}
	var components map[string]any
	diagnostics := []ir.Diagnostic{}
	for _, id := range sortedKeys(nodes) {
		n := object(nodes[id])
		raw, ok := n["pending"].(string)
		if !ok {
			continue
		}
		pointer := ir.Pointer("nodes", id, "pending")
		row := store.PendingComponentRequestRow{}
		err := pgx.ErrNoRows
		if request, parse := uuid.Parse(raw); parse == nil {
			row, err = c.q.PendingComponentRequest(ctx, store.PendingComponentRequestParams{ProjectID: c.ProjectID, ID: request})
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, err
		}
		live := err == nil && row.Kind == "component" && row.Key == fmt.Sprint(n["type"]) && row.Status != "draft" && row.Status != "rejected" && row.Status != "cancelled"
		if live && nestedPending(nodes, parents, id) {
			live = false
		}
		if !live {
			diagnostics = append(diagnostics, problem("CAPABILITY_PENDING_INVALID", pointer, id, nil).Diagnostics...)
			continue
		}
		diagnostics = append(diagnostics, problem("COMPONENT_PENDING_CAPABILITY", pointer, id, map[string]any{"requestId": raw}).Diagnostics...)
		contract := mustDecode(row.ProposedContract)
		if components == nil {
			components = map[string]any{}
			for k, v := range object(object(c.app)["components"]) {
				components[k] = v
			}
		}
		definition := map[string]any{}
		for _, field := range []string{"props", "container", "slots"} {
			if v, ok := contract[field]; ok {
				definition[field] = v
			}
		}
		components[row.Key] = definition
	}
	if components == nil {
		return c.app, diagnostics, nil
	}
	app := map[string]any{}
	for k, v := range object(c.app) {
		app[k] = v
	}
	app["components"] = components
	return app, diagnostics, nil
}

// nestedPending: a placeholder inside another placeholder is refused.
func nestedPending(nodes map[string]any, parents map[string]string, id string) bool {
	seen := map[string]bool{id: true}
	for p := parents[id]; p != "" && !seen[p]; p = parents[p] {
		seen[p] = true
		if _, ok := object(nodes[p])["pending"].(string); ok {
			return true
		}
	}
	return false
}

func childIDs(n map[string]any) []string {
	out := []string{}
	for _, v := range array(n["children"]) {
		out = append(out, fmt.Sprint(v))
	}
	for _, slot := range sortedKeys(object(n["slots"])) {
		for _, v := range array(object(n["slots"])[slot]) {
			out = append(out, fmt.Sprint(v))
		}
	}
	return out
}

func mustDecode(b []byte) map[string]any {
	v, _ := decode(b)
	return v
}

func array(v any) []any { a, _ := v.([]any); return a }
