package validation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/policydoc"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// pendingApp returns the manifest view for a document with component
// placeholders (STU-357): each placeholder of a live component request is
// checked against the request's proposed contract, as if it were a manifest
// component. Every placeholder blocks publication (STU-358).
// The returned document replaces token placeholders by aliases of the overlay
// (STU-378), so the overlay stays a valid manifest.
func (c *Context) pendingApp(ctx context.Context, doc map[string]any) (any, map[string]any, []ir.Diagnostic, error) {
	app, diagnostics, err := c.componentPlaceholders(ctx, doc)
	if err != nil {
		return nil, nil, nil, err
	}
	return c.dataAndTokenPlaceholders(ctx, app, doc, diagnostics)
}

func (c *Context) componentPlaceholders(ctx context.Context, doc map[string]any) (any, []ir.Diagnostic, error) {
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

// request reads a live request of the kind for a placeholder marker.
func (c *Context) request(ctx context.Context, marker, kind string) (store.PendingComponentRequestRow, bool, error) {
	row := store.PendingComponentRequestRow{}
	err := pgx.ErrNoRows
	if id, parse := uuid.Parse(marker); parse == nil {
		row, err = c.q.PendingComponentRequest(ctx, store.PendingComponentRequestParams{ProjectID: c.ProjectID, ID: id})
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return row, false, err
	}
	live := err == nil && row.Kind == kind && row.Status != "draft" && row.Status != "rejected" && row.Status != "cancelled"
	return row, live, nil
}

// dataAndTokenPlaceholders — STU-377/378/379: a data source placeholder is
// checked by its proposed params and result; a token placeholder takes the
// proposed value of its category. Both block publication.
func (c *Context) dataAndTokenPlaceholders(ctx context.Context, app any, doc map[string]any, diagnostics []ir.Diagnostic) (any, map[string]any, []ir.Diagnostic, error) {
	overlay := map[string]any{}
	for k, v := range object(app) {
		overlay[k] = v
	}
	changed := false
	sources := object(doc["dataSources"])
	for _, key := range sortedKeys(sources) {
		use := object(sources[key])
		marker, ok := use["pending"].(string)
		if !ok {
			continue
		}
		pointer := ir.Pointer("dataSources", key, "pending")
		row, live, err := c.request(ctx, marker, "dataSource")
		if err != nil {
			return nil, nil, nil, err
		}
		if !live || row.Key != use["source"] || doc["kind"] != "page" {
			diagnostics = append(diagnostics, problem("CAPABILITY_PENDING_INVALID", pointer, "", nil).Diagnostics...)
			continue
		}
		diagnostics = append(diagnostics, problem("DATA_SOURCE_PENDING_CAPABILITY", pointer, "", map[string]any{"requestId": marker}).Diagnostics...)
		defs := map[string]any{}
		for k, v := range object(overlay["dataSources"]) {
			defs[k] = v
		}
		contract := mustDecode(row.ProposedContract)
		definition := map[string]any{}
		for _, field := range []string{"params", "result", "paginated"} {
			if v, ok := contract[field]; ok {
				definition[field] = v
			}
		}
		defs[row.Key] = definition
		overlay["dataSources"] = defs
		changed = true
	}
	var copied map[string]any
	nodes := object(doc["nodes"])
	for _, id := range sortedKeys(nodes) {
		for _, leaf := range designLeaves(object(object(nodes[id])["design"]), ir.Pointer("nodes", id, "design")) {
			marker, ok := strings.CutPrefix(leaf.value, "pending:")
			if !ok {
				continue
			}
			row, live, err := c.request(ctx, marker, "token")
			if err != nil {
				return nil, nil, nil, err
			}
			contract := mustDecode(row.ProposedContract)
			category := policydoc.TokenCategory(leaf.property)
			if !live || category == "" || contract["category"] != category {
				diagnostics = append(diagnostics, problem("CAPABILITY_PENDING_INVALID", leaf.pointer, id, nil).Diagnostics...)
				continue
			}
			diagnostics = append(diagnostics, problem("TOKEN_PENDING_CAPABILITY", leaf.pointer, id, map[string]any{"requestId": marker}).Diagnostics...)
			if copied == nil {
				copied = cloneJSON(doc)
			}
			alias := "capPending" + strings.ReplaceAll(marker, "-", "")
			setLeaf(copied, leaf.pointer, alias)
			tokens := map[string]any{}
			for k, v := range object(overlay["tokens"]) {
				tokens[k] = v
			}
			values := map[string]any{}
			for k, v := range object(tokens[category]) {
				values[k] = v
			}
			values[alias] = contract["value"]
			tokens[category] = values
			overlay["tokens"] = tokens
			changed = true
		}
	}
	if copied != nil {
		doc = copied
	}
	if !changed {
		return app, doc, diagnostics, nil
	}
	return overlay, doc, diagnostics, nil
}

type designLeaf struct {
	property, pointer, value string
}

// designLeaves lists string design values with their property and pointer.
func designLeaves(design map[string]any, base string) []designLeaf {
	out := []designLeaf{}
	add := func(property string, v any, pointer string) {
		if s, ok := v.(string); ok {
			out = append(out, designLeaf{property, pointer, s})
			return
		}
		if m := object(v); m != nil && m["raw"] == nil {
			for _, bp := range sortedKeys(m) {
				if s, ok := m[bp].(string); ok {
					out = append(out, designLeaf{property, pointer + "/" + bp, s})
				}
			}
		}
	}
	for _, key := range sortedKeys(design) {
		if key == "states" {
			for _, state := range sortedKeys(object(design[key])) {
				for _, prop := range sortedKeys(object(object(design[key])[state])) {
					add(prop, object(object(design[key])[state])[prop], base+ir.Pointer("states", state, prop))
				}
			}
			continue
		}
		add(key, design[key], base+ir.Pointer(key))
	}
	return out
}

// setLeaf replaces the string at a JSON pointer of a cloned document.
func setLeaf(doc map[string]any, pointer, value string) {
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	m := doc
	for _, p := range parts[:len(parts)-1] {
		m = object(m[p])
	}
	if m != nil {
		m[parts[len(parts)-1]] = value
	}
}

func cloneJSON(doc map[string]any) map[string]any {
	b, _ := json.Marshal(doc)
	return mustDecode(b)
}
