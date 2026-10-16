package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// ContentContext is kept separate from entity field names and participates in ETags.
type ContentContext struct {
	Locale   string               `json:"locale"`
	Versions map[string]uuid.UUID `json:"versions"`
}

// BindingValueMissing is a valid schema path with no value in the selected locale.
const BindingValueMissing = "BINDING_VALUE_MISSING"

var contentPath = regexp.MustCompile(`^\$content(?:\.[A-Za-z][A-Za-z0-9_]*){1,8}$`)

func primaryObject(v any) map[string]any { m, _ := v.(map[string]any); return m }
func primaryProblem(code, pointer, node string) ir.Diagnostic {
	return ir.Diagnostic{Code: ir.Code(code), Severity: ir.SeverityWarning, Pointer: pointer, NodeID: node, Message: code}
}

// Follow the schema rather than interpreting an arbitrary object.assetId as a file.
func primaryAssetRefs(fields, content map[string]any) map[uuid.UUID]bool {
	refs := map[uuid.UUID]bool{}
	for key, value := range content {
		field := primaryObject(fields[key])
		switch field["type"] {
		case "asset":
			if raw, ok := primaryObject(value)["assetId"].(string); ok {
				if id, err := uuid.Parse(raw); err == nil {
					refs[id] = true
				}
			}
		case "object":
			for id := range primaryAssetRefs(primaryObject(field["fields"]), primaryObject(value)) {
				refs[id] = true
			}
		}
	}
	return refs
}

// projectPrimary never serializes unused fields, reference targets, or other locales.
func projectPrimary(doc, schema, data map[string]any, locale string) (map[string]any, []ir.Diagnostic, error) {
	out := map[string]any{}
	diagnostics := []ir.Diagnostic{}
	paths := map[string]ir.Diagnostic{}
	var scan func(any, string, string)
	scan = func(v any, pointer, node string) {
		switch x := v.(type) {
		case string:
			if !strings.HasPrefix(x, "$content") {
				return
			}
			if !contentPath.MatchString(x) {
				diagnostics = append(diagnostics, primaryProblem("BINDING_PATH_UNRESOLVED", pointer, node))
				return
			}
			paths[x] = primaryProblem("", pointer, node)
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if k != "lit" && k != "default" && k != "format" && k != "template" {
					scan(x[k], pointer+ir.Pointer(k), node)
				}
			}
		case []any:
			for i, v := range x {
				scan(v, fmt.Sprintf("%s/%d", pointer, i), node)
			}
		}
	}
	nodes := primaryObject(doc["nodes"])
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		n := primaryObject(nodes[id])
		for _, k := range []string{"bindings", "when", "on"} {
			scan(n[k], ir.Pointer("nodes", id, k), id)
		}
	}
	scan(doc["dataSources"], "/dataSources", "")
	if len(paths) > 200 {
		return nil, nil, commandbus.NewError(413, "LIMIT_EXCEEDED", "Слишком много полей контента", "primary content supports at most 200 paths")
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	for _, path := range ordered {
		pathProblem := func(code string) ir.Diagnostic {
			d := paths[path]
			d.Code = ir.Code(code)
			d.Message = code
			d.Params = map[string]any{"path": path}
			return d
		}
		parts := strings.Split(strings.TrimPrefix(path, "$content."), ".")
		fields := primaryObject(schema["fields"])
		values := data
		var value any
		present := true
		supported := true
		for i, key := range parts {
			if key == "__proto__" || key == "prototype" || key == "constructor" {
				supported = false
				break
			}
			field := primaryObject(fields[key])
			if field == nil {
				supported = false
				break
			}
			value, present = values[key]
			if field["localized"] == true && present {
				value, present = primaryObject(value)[locale]
			}
			if i < len(parts)-1 {
				if field["type"] != "object" {
					supported = false
					break
				}
				fields = primaryObject(field["fields"])
				values = primaryObject(value)
			} else {
				switch field["type"] {
				case "string", "text", "number", "boolean", "enum", "date", "datetime", "url", "asset":
				default:
					supported = false
				}
			}
		}
		if !supported {
			diagnostics = append(diagnostics, pathProblem("BINDING_PATH_UNRESOLVED"))
			continue
		}
		if !present {
			diagnostics = append(diagnostics, pathProblem(BindingValueMissing))
			continue
		}
		target := out
		for _, key := range parts[:len(parts)-1] {
			if target[key] == nil {
				target[key] = map[string]any{}
			}
			target = primaryObject(target[key])
		}
		target[parts[len(parts)-1]] = value
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, nil, err
	}
	if len(raw) > 1024*1024 {
		return nil, nil, commandbus.NewError(413, "LIMIT_EXCEEDED", "Контент слишком большой", "primary content exceeds 1 MiB")
	}
	return out, diagnostics, nil
}

func resolvePrimary(ctx context.Context, q *store.Queries, a Access, p *Page, requested string) error {
	settingsRaw, err := q.GetProjectSettings(ctx, a.ProjectID)
	if err != nil {
		return err
	}
	var settings map[string]any
	if err = json.Unmarshal(settingsRaw, &settings); err != nil {
		return err
	}
	locale := requested
	if locale == "" {
		locale, _ = settings["defaultLocale"].(string)
	}
	if locale == "" {
		locale = "ru"
	}
	allowed := false
	for _, l := range toPrimaryArray(settings["locales"]) {
		allowed = allowed || l == locale
	}
	// Legacy projects without locale settings retain their existing default.
	if len(toPrimaryArray(settings["locales"])) == 0 {
		allowed = locale == "ru"
	}
	if !allowed {
		return commandbus.NewError(400, "PARAM_INVALID", "Неизвестная локаль проекта", "locale must be declared by the project")
	}
	var doc map[string]any
	if err = json.Unmarshal(p.Document, &doc); err != nil {
		return err
	}
	declaration := primaryObject(doc["content"])
	if declaration == nil {
		return nil
	}
	p.ContentContext = &ContentContext{Locale: locale, Versions: map[string]uuid.UUID{}}
	p.Data["content"] = map[string]any{}
	fail := func(code string) error {
		p.Diagnostics = append(p.Diagnostics, primaryProblem(code, "/content", ""))
		return nil
	}
	resolve := primaryObject(declaration["resolve"])
	if resolve["by"] != "entity" {
		return fail("BINDING_PATH_UNRESOLVED")
	}
	id, err := uuid.Parse(fmt.Sprint(resolve["entityId"]))
	if err != nil {
		return fail("CONTENT_REFERENCE_UNPUBLISHED")
	}
	var scopedCS *uuid.UUID
	if a.Draft {
		scopedCS = a.ChangesetID
	}
	v, err := q.ContentVersion(ctx, store.ContentVersionParams{ProjectID: a.ProjectID, ObjectID: id, EnvironmentID: a.EnvironmentID, ChangesetID: scopedCS, UseHead: a.Draft})
	if errors.Is(err, pgx.ErrNoRows) {
		return fail("CONTENT_REFERENCE_UNPUBLISHED")
	}
	if err != nil {
		return err
	}
	if v.Kind != "entity" || v.Deleted || v.SchemaName == nil || *v.SchemaName != declaration["schema"] {
		return fail("CONTENT_REFERENCE_SCHEMA")
	}
	c, err := validation.LoadSnapshot(ctx, q, a.ProjectID, a.Environment, scopedCS)
	if err != nil {
		return err
	}
	schema, version, ok := c.Schema(*v.SchemaName)
	if !ok || v.SchemaVersion == nil || *v.SchemaVersion != version {
		return fail("SCHEMA_UPCAST_REQUIRED")
	}
	var values map[string]any
	if err = json.Unmarshal(v.Body, &values); err != nil {
		return err
	}
	content, diagnostics, err := projectPrimary(doc, schema, values, locale)
	if err != nil {
		return err
	}
	p.Data["content"] = content
	p.Diagnostics = append(p.Diagnostics, diagnostics...)
	p.ContentContext.Versions[id.String()] = v.ID
	for assetID := range primaryAssetRefs(primaryObject(schema["fields"]), content) {
		asset, err := q.ContentVersion(ctx, store.ContentVersionParams{ProjectID: a.ProjectID, ObjectID: assetID, EnvironmentID: a.EnvironmentID, ChangesetID: scopedCS, UseHead: a.Draft})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && (asset.Deleted || asset.Kind != "asset")) {
			return fail("CONTENT_REFERENCE_UNPUBLISHED")
		}
		if err != nil {
			return err
		}
		p.ContentContext.Versions[assetID.String()] = asset.ID
	}
	return nil
}
func toPrimaryArray(v any) []any { a, _ := v.([]any); return a }
