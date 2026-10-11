package validation

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifestdoc"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Schema returns the environment's immutable contract (or its linked candidate).
func (c *Context) Schema(name string) (map[string]any, int32, bool) {
	s := object(object(object(c.app)["schemas"])[name])
	n, err := json.Number(fmt.Sprint(s["version"])).Float64()
	v := int64(n)
	if err != nil || n != float64(v) || v < 1 || v > 2147483647 {
		return nil, 0, false
	}
	return s, int32(v), true
}

// Fields checks stored entity values; only missing required fields are draft warnings.
func (c *Context) Fields(schema, data map[string]any, draft bool) ir.Result {
	r := ir.Result{Valid: true}
	add := func(code, path string, warning bool) {
		severity := ir.SeverityError
		if warning {
			severity = ir.SeverityWarning
		} else {
			r.Valid = false
		}
		r.Diagnostics = append(r.Diagnostics, ir.Diagnostic{Code: ir.Code(code), Severity: severity, Pointer: path, Message: code})
	}

	locales, _ := c.project["locales"].([]any)
	var check func(map[string]any, any, string, int)
	check = func(field map[string]any, value any, path string, depth int) {
		if depth > 32 {
			add("CONTENT_DEPTH_LIMIT", path, false)
			return
		}
		if value == nil {
			if field["required"] == true && field["default"] == nil {
				add("CONTENT_REQUIRED", path, draft)
			}
			return
		}
		if field["localized"] == true {
			values := object(value)
			if values == nil {
				add("CONTENT_FIELD_INVALID", path, false)
				return
			}
			f := map[string]any{}
			for k, v := range field {
				f[k] = v
			}
			delete(f, "localized")
			for _, locale := range sortedKeys(values) {
				allowed := len(locales) == 0
				for _, l := range locales {
					allowed = allowed || l == locale
				}
				if !allowed {
					add("CONTENT_FIELD_INVALID", path+ir.Pointer(locale), false)
				} else {
					check(f, values[locale], path+ir.Pointer(locale), depth+1)
				}
			}
			if field["required"] == true {
				for _, l := range locales {
					locale := fmt.Sprint(l)
					if _, exists := values[locale]; !exists {
						check(f, nil, path+ir.Pointer(locale), depth+1)
					}
				}
			}
			return
		}
		switch field["type"] {
		case "object":
			obj := object(value)
			if obj == nil {
				add("CONTENT_FIELD_INVALID", path, false)
				return
			}
			fields := object(field["fields"])
			for _, key := range sortedKeys(obj) {
				if fields[key] == nil {
					add("CONTENT_FIELD_UNKNOWN", path+ir.Pointer(key), false)
				}
			}
			for _, key := range sortedKeys(fields) {
				check(object(fields[key]), obj[key], path+ir.Pointer(key), depth+1)
			}
		case "list":
			values, ok := value.([]any)
			if !ok {
				add("CONTENT_FIELD_INVALID", path, false)
				return
			}
			length := float64(len(values))
			for _, bound := range []string{"min", "max"} {
				if raw, exists := field[bound]; exists {
					n, _ := json.Number(fmt.Sprint(raw)).Float64()
					if (bound == "min" && length < n) || (bound == "max" && length > n) {
						add("CONTENT_FIELD_INVALID", path, false)
					}
				}
			}
			for i, v := range values {
				check(object(field["of"]), v, fmt.Sprintf("%s/%d", path, i), depth+1)
			}
		default:
			if !manifestdoc.ValidateValue(nil, c.app, field, value) {
				add("CONTENT_FIELD_INVALID", path, false)
			}
		}
	}
	check(map[string]any{"type": "object", "fields": schema["fields"]}, data, "", 0)
	return r
}

func (c *Context) contentVersion(ctx context.Context, id uuid.UUID) (store.ContentVersionRow, error) {
	if d, ok := c.Overrides[id]; ok {
		o, err := c.q.ContentIdentity(ctx, store.ContentIdentityParams{ProjectID: c.ProjectID, ID: id})
		if err != nil {
			return store.ContentVersionRow{}, err
		}
		v, err := c.q.GetVersion(ctx, d.VersionID)
		return store.ContentVersionRow{ID: v.ID, Body: d.Body, Deleted: v.Deleted, SchemaVersion: v.SchemaVersion, Kind: o.Kind, SchemaName: o.SchemaName}, err
	}
	var cs *uuid.UUID
	if c.Draft || c.UseHead {
		cs = c.ChangesetID
	}
	return c.q.ContentVersion(ctx, store.ContentVersionParams{ProjectID: c.ProjectID, ObjectID: id, EnvironmentID: c.Environment.ID, ChangesetID: cs, UseHead: c.UseHead})
}

// References traverses structured JSON, including rich text, IR local content and bindings.
func (c *Context) References(ctx context.Context, body any) (ir.Result, error) {
	r := ir.Result{Valid: true}
	var visit func(any, string, int) error
	visit = func(value any, path string, depth int) error {
		if depth > 128 {
			r.Valid = false
			r.Diagnostics = append(r.Diagnostics, problem("CONTENT_DEPTH_LIMIT", path, "", nil).Diagnostics...)
			return nil
		}
		switch v := value.(type) {
		case map[string]any:
			for _, key := range []string{"entityId", "assetId", "page"} {
				raw, exists := v[key]
				if key == "page" && v["kind"] != "page" {
					continue
				}
				if !exists {
					continue
				}
				id, err := uuid.Parse(fmt.Sprint(raw))
				target := store.ContentVersionRow{}
				if err == nil {
					target, err = c.contentVersion(ctx, id)
				}
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					if _, e := uuid.Parse(fmt.Sprint(raw)); e == nil {
						return err
					}
				}
				kind := "entity"
				if key == "assetId" {
					kind = "asset"
				}
				if key == "page" {
					kind = "document"
				}
				targetBody, _ := decode(target.Body)
				if err != nil || target.Kind != kind || target.Deleted || (key == "page" && targetBody["kind"] != "page") {
					r.Valid = false
					r.Diagnostics = append(r.Diagnostics, problem("CONTENT_REFERENCE_UNPUBLISHED", path+"/"+key, "", map[string]any{"target": raw, "kind": kind}).Diagnostics...)
				} else if schema, ok := v["schema"].(string); ok && kind == "entity" && (target.SchemaName == nil || *target.SchemaName != schema) {
					r.Valid = false
					r.Diagnostics = append(r.Diagnostics, problem("CONTENT_REFERENCE_SCHEMA", path+"/"+key, "", map[string]any{"schema": schema}).Diagnostics...)
				}
			}
			for _, k := range sortedKeys(v) {
				if err := visit(v[k], path+ir.Pointer(k), depth+1); err != nil {
					return err
				}
			}
		case []any:
			for i, x := range v {
				if err := visit(x, fmt.Sprintf("%s/%d", path, i), depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return r, visit(body, "", 0)
}

func (c *Context) validateContent(ctx context.Context, d Document, o store.Object) (ir.Result, error) {
	v, err := c.q.GetVersion(ctx, d.VersionID)
	if err != nil {
		return ir.Result{}, err
	}
	if v.Deleted {
		return ir.Result{Valid: true}, nil
	}
	data, err := decode(d.Body)
	if err != nil {
		return ir.Result{}, err
	}
	if o.Kind == "asset" {
		if data["managedFile"] == true {
			hash, err := hex.DecodeString(fmt.Sprint(data["fileHash"]))
			if err != nil || len(hash) != 32 {
				return problem("CONTENT_ASSET_NOT_READY", "/fileHash", "", nil), nil
			}
			f, err := c.q.ReadyAssetFile(ctx, store.ReadyAssetFileParams{ProjectID: c.ProjectID, Sha256: hash})
			if errors.Is(err, pgx.ErrNoRows) {
				return problem("CONTENT_ASSET_NOT_READY", "/fileHash", "", nil), nil
			}
			if err != nil {
				return ir.Result{}, err
			}
			if data["mimeType"] != f.MimeType || fmt.Sprint(data["size"]) != fmt.Sprint(f.SizeBytes) {
				return problem("CONTENT_ASSET_FILE_MISMATCH", "/fileHash", "", nil), nil
			}
		}
		metadata := map[string]any{}
		for _, key := range []string{"alt", "title", "focalPoint", "tags"} {
			if value, ok := data[key]; ok {
				metadata[key] = value
			}
		}
		result := c.Fields(map[string]any{"fields": map[string]any{
			"alt": map[string]any{"type": "text", "localized": true}, "title": map[string]any{"type": "text"},
			"tags":       map[string]any{"type": "list", "of": map[string]any{"type": "string"}},
			"focalPoint": map[string]any{"type": "object", "fields": map[string]any{"x": map[string]any{"type": "number", "min": 0.0, "max": 1.0, "required": true}, "y": map[string]any{"type": "number", "min": 0.0, "max": 1.0, "required": true}}},
		}}, metadata, false)
		return result, nil
	}
	s, version, ok := c.Schema(*o.SchemaName)
	if !ok {
		return problem("CONTENT_SCHEMA_UNKNOWN", "", "", nil), nil
	}
	if v.SchemaVersion == nil || *v.SchemaVersion != version {
		code := "SCHEMA_UPCAST_REQUIRED"
		if v.SchemaVersion != nil && *v.SchemaVersion > version {
			code = "SCHEMA_VERSION_AHEAD"
		}
		return problem(code, "", "", map[string]any{"expected": version, "actual": v.SchemaVersion}), nil
	}
	r := c.Fields(s, data, c.Draft)
	typed, err := c.TypedReferences(ctx, map[string]any{"type": "object", "fields": s["fields"]}, data, "")
	if err != nil {
		return ir.Result{}, err
	}
	r.Diagnostics = append(r.Diagnostics, typed.Diagnostics...)
	r.Valid = r.Valid && typed.Valid
	return r, nil
}

// HeadConstraints validates the prospective project-wide head, including deletions/replacements.
func (c *Context) HeadConstraints(ctx context.Context, docs []Document) (map[string][]ir.Diagnostic, error) {
	heads, err := c.q.ContentHeads(ctx, c.ProjectID)
	if err != nil {
		return nil, err
	}
	type item struct {
		id     uuid.UUID
		schema string
		data   map[string]any
	}
	all := map[uuid.UUID]item{}
	for _, h := range heads {
		if !h.Deleted {
			data, _ := decode(h.Body)
			all[h.ID] = item{h.ID, *h.SchemaName, data}
		}
	}
	for _, d := range docs {
		o, err := c.q.ContentIdentity(ctx, store.ContentIdentityParams{ProjectID: c.ProjectID, ID: d.ObjectID})
		if err != nil {
			return nil, err
		}
		if o.Kind != "entity" {
			continue
		}
		v, err := c.q.GetVersion(ctx, d.VersionID)
		if err != nil {
			return nil, err
		}
		delete(all, o.ID)
		if !v.Deleted {
			data, _ := decode(d.Body)
			all[o.ID] = item{o.ID, *o.SchemaName, data}
		}
	}
	out := map[string][]ir.Diagnostic{}
	ids := map[string][]uuid.UUID{}
	for _, id := range sortedUUIDs(all) {
		x := all[id]
		s, _, ok := c.Schema(x.schema)
		if !ok {
			continue
		}
		if s["singleton"] == true && len(ids[x.schema]) > 0 {
			out[id.String()] = append(out[id.String()], problem("CONTENT_SINGLETON", "", "", map[string]any{"objects": ids[x.schema]}).Diagnostics...)
		}
		ids[x.schema] = append(ids[x.schema], id)
		for _, key := range sortedKeys(object(s["fields"])) {
			f := object(object(s["fields"])[key])
			if f["unique"] != true || x.data[key] == nil {
				continue
			}
			for _, otherID := range ids[x.schema][:len(ids[x.schema])-1] {
				other := all[otherID]
				collision := reflect.DeepEqual(x.data[key], other.data[key])
				if f["localized"] == true {
					collision = false
					for l, value := range object(x.data[key]) {
						collision = collision || (value != nil && reflect.DeepEqual(value, object(other.data[key])[l]))
					}
				}
				if collision {
					out[id.String()] = append(out[id.String()], problem("CONTENT_UNIQUE", ir.Pointer(key), "", map[string]any{"object": otherID}).Diagnostics...)
				}
			}
		}
	}
	return out, nil
}

func sortedUUIDs[T any](m map[uuid.UUID]T) []uuid.UUID {
	keys := map[string]uuid.UUID{}
	for id := range m {
		keys[id.String()] = id
	}
	text := map[string]any{}
	for k := range keys {
		text[k] = nil
	}
	result := []uuid.UUID{}
	for _, k := range sortedKeys(text) {
		result = append(result, keys[k])
	}
	return result
}

func (c *Context) TypedReferences(ctx context.Context, field map[string]any, value any, path string) (ir.Result, error) {
	r := ir.Result{Valid: true}
	if value == nil {
		return r, nil
	}
	if field["localized"] == true {
		f := map[string]any{}
		for k, v := range field {
			f[k] = v
		}
		delete(f, "localized")
		for _, locale := range sortedKeys(object(value)) {
			child, err := c.TypedReferences(ctx, f, object(value)[locale], path+ir.Pointer(locale))
			if err != nil {
				return r, err
			}
			r.Diagnostics = append(r.Diagnostics, child.Diagnostics...)
			r.Valid = r.Valid && child.Valid
		}
		return r, nil
	}
	appendResult := func(child ir.Result, err error) error {
		r.Diagnostics = append(r.Diagnostics, child.Diagnostics...)
		r.Valid = r.Valid && child.Valid
		return err
	}
	switch field["type"] {
	case "reference", "asset":
		kind := "entity"
		key := "entityId"
		if field["type"] == "asset" {
			kind = "asset"
			key = "assetId"
		}
		id, err := uuid.Parse(fmt.Sprint(object(value)[key]))
		if err != nil {
			return problem("CONTENT_REFERENCE_UNPUBLISHED", path, "", nil), nil
		}
		target, err := c.contentVersion(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return problem("CONTENT_REFERENCE_UNPUBLISHED", path, "", map[string]any{"target": id}), nil
		}
		if err != nil {
			return r, err
		}
		if target.Kind != kind || target.Deleted {
			return problem("CONTENT_REFERENCE_UNPUBLISHED", path, "", map[string]any{"target": id}), nil
		}
		if kind == "entity" && field["schema"] != nil && (target.SchemaName == nil || *target.SchemaName != field["schema"]) {
			return problem("CONTENT_REFERENCE_SCHEMA", path, "", map[string]any{"schema": field["schema"]}), nil
		}
		if kind == "asset" {
			body, _ := decode(target.Body)
			if allowed, ok := field["mimeTypes"].([]any); ok {
				matches := false
				for _, mime := range allowed {
					matches = matches || mime == body["mimeType"]
				}
				if !matches {
					return problem("CONTENT_ASSET_MIME", path, "", nil), nil
				}
			}
			if field["assetKind"] != nil && field["assetKind"] != body["assetKind"] {
				return problem("CONTENT_ASSET_KIND", path, "", nil), nil
			}
		}
	case "richText", "link":
		return c.References(ctx, value)
	case "object":
		for _, k := range sortedKeys(object(value)) {
			if err := appendResult(c.TypedReferences(ctx, object(object(field["fields"])[k]), object(value)[k], path+ir.Pointer(k))); err != nil {
				return r, err
			}
		}
	case "list":
		values, _ := value.([]any)
		for i, v := range values {
			if err := appendResult(c.TypedReferences(ctx, object(field["of"]), v, fmt.Sprintf("%s/%d", path, i))); err != nil {
				return r, err
			}
		}
	}
	return r, nil
}
