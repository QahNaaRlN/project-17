package validation

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/bindingdoc"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"strconv"
	"strings"
)

// RequirePrimaryEdit rejects invalid source dependencies while preserving draft
// warning semantics for unrelated incomplete properties.
func (c *Context) RequirePrimaryEdit(ctx context.Context, id uuid.UUID, doc map[string]any, path *string) error {
	bad := map[string][]ir.Diagnostic{}
	if c.app == nil {
		bad[id.String()] = problem("MANIFEST_NOT_READY", "/content", "", nil).Diagnostics
		return RequireValid(bad)
	}
	bindings := bindingdoc.Validate(doc, c.app, bindingdoc.Options{Path: path})
	for _, d := range bindings.Diagnostics {
		if strings.HasPrefix(d.Pointer, "/content") || primaryAtPointer(doc, d.Pointer) {
			bad[id.String()] = append(bad[id.String()], d)
		}
	}
	declaration := object(doc["content"])
	resolve := object(declaration["resolve"])
	if resolve["by"] == "entity" {
		typed, err := c.TypedReferences(ctx, map[string]any{"type": "reference", "schema": declaration["schema"]}, resolve, "/content/resolve")
		if err != nil {
			return err
		}
		bad[id.String()] = append(bad[id.String()], typed.Diagnostics...)
		sourceID, err := uuid.Parse(fmt.Sprint(resolve["entityId"]))
		if err == nil {
			source, err := c.contentVersion(ctx, sourceID)
			if err == nil {
				_, version, ok := c.Schema(fmt.Sprint(declaration["schema"]))
				if !ok || source.SchemaVersion == nil || *source.SchemaVersion != version {
					bad[id.String()] = append(bad[id.String()], problem("SCHEMA_UPCAST_REQUIRED", "/content", "", nil).Diagnostics...)
				}
			}
		}
	}
	if len(bad[id.String()]) == 0 {
		delete(bad, id.String())
	}
	return RequireValid(bad)
}
func primaryAtPointer(doc map[string]any, pointer string) bool {
	var value any = doc
	for _, part := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		if list, ok := value.([]any); ok {
			n, err := strconv.Atoi(part)
			if err != nil || n < 0 || n >= len(list) {
				return false
			}
			value = list[n]
		} else {
			value = object(value)[part]
		}
	}
	var contains func(any) bool
	contains = func(v any) bool {
		switch x := v.(type) {
		case string:
			return strings.HasPrefix(x, "$content")
		case map[string]any:
			for key, v := range x {
				if key == "lit" || key == "default" || key == "format" || key == "template" {
					continue
				}
				if contains(v) {
					return true
				}
			}
		case []any:
			for _, v := range x {
				if contains(v) {
					return true
				}
			}
		}
		return false
	}
	return contains(value)
}
