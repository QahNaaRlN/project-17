package changes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

const (
	EntityCreate     = "entity.create"
	EntitySetFields  = "entity.setFields"
	EntityDelete     = "entity.delete"
	EntityRestore    = "entity.restore"
	AssetUpdateMeta  = "asset.updateMeta"
	AssetCreate      = "asset.create"
	AssetReplaceFile = "asset.replaceFile"
)

func isContentOperation(t string) bool {
	return t == EntitySetFields || t == EntityDelete || t == EntityRestore || t == AssetUpdateMeta || t == AssetReplaceFile
}
func payloadError(message string) error { return &ops.Error{Code: "PAYLOAD_INVALID", Message: message} }
func parsePayload(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return payloadError(err.Error())
	}
	return nil
}
func (s *session) createEntity(in OperationInput) (recordInput, error) {
	var p struct {
		Schema      string         `json:"schema"`
		Environment string         `json:"environment"`
		Data        map[string]any `json:"data"`
	}
	if err := parsePayload(in.Payload, &p); err != nil {
		return recordInput{}, err
	}
	if p.Environment == "" || p.Data == nil {
		return recordInput{}, payloadError("нужны environment, schema и data")
	}
	if len(s.cs.Targets) == 0 {
		s.cs.Targets = []string{p.Environment}
		if err := s.q.SetContentTargets(s.ctx, store.SetContentTargetsParams{ID: s.cs.ID, Targets: s.cs.Targets}); err != nil {
			return recordInput{}, err
		}
	} else {
		found := false
		for _, name := range s.cs.Targets {
			found = found || name == p.Environment
		}
		if !found {
			return recordInput{}, payloadError("environment не входит в targets Change Set")
		}
	}
	c, err := validation.Load(s.ctx, s.q, s.actor.ProjectID, p.Environment, &s.cs.ID)
	if err != nil {
		return recordInput{}, err
	}
	schema, version, ok := c.Schema(p.Schema)
	if !ok {
		return recordInput{}, payloadError("схема не найдена в окружении")
	}
	if r := c.Fields(schema, p.Data, true); !r.Valid {
		return recordInput{}, &ops.Error{Code: "OPERATION_INVALID", Message: "тело не соответствует схеме", Diagnostics: r.Diagnostics}
	}
	id := uuid.Must(uuid.NewV7())
	obj, err := s.q.CreateEntityObject(s.ctx, store.CreateEntityObjectParams{ID: id, ProjectID: s.actor.ProjectID, SchemaName: &p.Schema})
	if err != nil {
		return recordInput{}, err
	}
	d, err := s.createWorking(obj.ID, nil, nil, p.Data)
	if err != nil {
		return recordInput{}, err
	}
	d.kind = "entity"
	d.schemaVersion = &version
	return recordInput{target: id, opType: in.Type, payload: in.Payload, after: map[string]any{"entityId": id, "schema": p.Schema, "schemaVersion": version, "data": p.Data}}, nil
}

func (s *session) applyContent(target uuid.UUID, op ops.Op) (recordInput, error) {
	right, _ := OperationRight(op.Type)
	if err := commandbus.Require(s.actor, right); err != nil {
		return recordInput{}, err
	}
	d, err := s.load(target)
	if err != nil {
		return recordInput{}, err
	}
	kind := "entity"
	if op.Type == AssetUpdateMeta || op.Type == AssetReplaceFile {
		kind = "asset"
	}
	if d.kind != kind {
		return recordInput{}, payloadError("неверный тип объекта")
	}
	var r ops.Result
	if op.Type == EntityDelete || op.Type == EntityRestore {
		var p struct{}
		if err := parsePayload(op.Payload, &p); err != nil {
			return recordInput{}, err
		}
		r = deleteResult(d.deleted, op.Type)
		d.deleted = op.Type == EntityDelete
	} else {
		if d.deleted {
			return recordInput{}, payloadError("сначала восстановите сущность")
		}
		if op.Type == AssetReplaceFile {
			return s.replaceAssetFile(target, d, op)
		}
		r, err = contentFields(d.body, op)
		if err != nil {
			return recordInput{}, err
		}
	}
	if kind == "entity" && !d.deleted {
		env := "staging"
		if len(s.cs.Targets) > 0 {
			env = s.cs.Targets[0]
		}
		if len(s.cs.Targets) == 0 {
			s.cs.Targets = []string{env}
			if err := s.q.SetContentTargets(s.ctx, store.SetContentTargetsParams{ID: s.cs.ID, Targets: s.cs.Targets}); err != nil {
				return recordInput{}, err
			}
		}
		c, err := validation.Load(s.ctx, s.q, s.actor.ProjectID, env, &s.cs.ID)
		if err != nil {
			return recordInput{}, err
		}
		obj, err := s.q.ContentIdentity(s.ctx, store.ContentIdentityParams{ProjectID: s.actor.ProjectID, ID: target})
		if err != nil {
			return recordInput{}, err
		}
		schema, version, ok := c.Schema(*obj.SchemaName)
		if !ok {
			return recordInput{}, payloadError("схема не найдена")
		}
		if d.schemaVersion != nil && *d.schemaVersion > version {
			return recordInput{}, commandbus.NewError(409, "SCHEMA_VERSION_AHEAD", "Даункаст не поддерживается", "Выберите окружение с совместимой схемой")
		}
		if result := c.Fields(schema, d.body, true); !result.Valid {
			return recordInput{}, &ops.Error{Code: "OPERATION_INVALID", Message: "тело не соответствует схеме", Diagnostics: result.Diagnostics}
		}
		d.schemaVersion = &version
	}
	return recordInput{target: target, opType: op.Type, payload: op.Payload, before: r.Before, after: r.After, inverse: &r.Inverse}, nil
}
func deleteResult(deleted bool, t string) ops.Result {
	inverse := EntityRestore
	if deleted {
		inverse = EntityDelete
	}
	return ops.Result{Before: map[string]any{"deleted": deleted}, After: map[string]any{"deleted": t == EntityDelete}, Inverse: ops.Op{Type: inverse, Payload: json.RawMessage(`{}`)}}
}

// contentFields applies paths atomically; journal snapshots preserve missing vs null values.
func contentFields(body map[string]any, op ops.Op) (ops.Result, error) {
	var p struct {
		Set   map[string]any `json:"set"`
		Unset []string       `json:"unset"`
	}
	if err := parsePayload(op.Payload, &p); err != nil {
		return ops.Result{}, err
	}
	paths := []string{}
	seen := map[string]bool{}
	for path := range p.Set {
		paths = append(paths, path)
		seen[path] = true
	}
	for _, path := range p.Unset {
		if seen[path] {
			return ops.Result{}, payloadError("повтор пути")
		}
		seen[path] = true
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return ops.Result{}, payloadError("set/unset пусты")
	}
	sort.Strings(paths)
	next := ops.Clone(body)
	before := map[string]any{}
	after := map[string]any{}
	inverseSet := map[string]any{}
	inverseUnset := []string{}
	for i, path := range paths {
		parts := strings.Split(path, ".")
		if len(parts) > 32 || parts[0] == "" {
			return ops.Result{}, payloadError("неверный путь")
		}
		for _, part := range parts {
			if part == "" {
				return ops.Result{}, payloadError("неверный путь")
			}
		}
		for _, other := range paths[:i] {
			if strings.HasPrefix(path, other+".") {
				return ops.Result{}, payloadError("пересекающиеся пути")
			}
		}
		if op.Type == AssetUpdateMeta && parts[0] != "alt" && parts[0] != "title" && parts[0] != "focalPoint" && parts[0] != "tags" {
			return ops.Result{}, payloadError("можно менять только метаданные ассета")
		}
		old, exists := pathValue(body, parts)
		before[path] = map[string]any{"exists": exists, "value": old}
		if exists {
			inverseSet[path] = old
		} else {
			restore := path
			for n := 1; n < len(parts); n++ {
				ancestor := strings.Join(parts[:n], ".")
				v, ok := pathValue(body, parts[:n])
				if !ok {
					restore = ancestor
					break
				}
				if v == nil {
					inverseSet[ancestor] = nil
					restore = ""
					break
				}
			}
			if restore != "" {
				inverseUnset = append(inverseUnset, restore)
			}
		}
		value, set := p.Set[path]
		parent := next
		for _, part := range parts[:len(parts)-1] {
			child, ok := parent[part].(map[string]any)
			if !ok {
				if parent[part] != nil {
					return ops.Result{}, payloadError("путь пересекает скаляр")
				}
				if !set {
					parent = nil
					break
				}
				child = map[string]any{}
				parent[part] = child
			}
			parent = child
		}
		if parent != nil {
			key := parts[len(parts)-1]
			if set {
				parent[key] = value
			} else {
				delete(parent, key)
			}
		}
		v, ok := pathValue(next, parts)
		after[path] = map[string]any{"exists": ok, "value": v}
	}
	for k := range body {
		delete(body, k)
	}
	for k, v := range next {
		body[k] = v
	}
	sort.Strings(inverseUnset)
	filtered := []string{}
	for _, path := range inverseUnset {
		covered := false
		for _, parent := range filtered {
			covered = covered || path == parent || strings.HasPrefix(path, parent+".")
		}
		for parent := range inverseSet {
			covered = covered || strings.HasPrefix(path, parent+".")
		}
		if !covered {
			filtered = append(filtered, path)
		}
	}
	inverseUnset = filtered
	return ops.Result{Before: before, After: after, Inverse: ops.Op{Type: op.Type, Payload: mustJSON(map[string]any{"set": inverseSet, "unset": inverseUnset})}}, nil
}
func pathValue(body map[string]any, parts []string) (any, bool) {
	current := body
	for _, part := range parts[:len(parts)-1] {
		m, ok := current[part].(map[string]any)
		if !ok {
			return nil, false
		}
		current = m
	}
	v, ok := current[parts[len(parts)-1]]
	return v, ok
}

// Content is the entity/asset read model; deleted versions are retained in CS/history only.
type Content struct {
	ID            uuid.UUID       `json:"id"`
	Kind          string          `json:"kind"`
	Schema        *string         `json:"schema,omitempty"`
	SchemaVersion *int32          `json:"schemaVersion,omitempty"`
	VersionID     uuid.UUID       `json:"versionId"`
	Deleted       bool            `json:"deleted"`
	Data          json.RawMessage `json:"data"`
}

func GetContent(ctx context.Context, q *store.Queries, project, id uuid.UUID, env string, cs *uuid.UUID, headOnly ...bool) (Content, error) {
	useHead := env == ""
	if len(headOnly) > 0 {
		useHead = headOnly[0]
	}
	if env == "" {
		env = "staging"
	}
	e, err := q.GetManifestEnvironment(ctx, store.GetManifestEnvironmentParams{ProjectID: project, Name: env})
	if errors.Is(err, pgx.ErrNoRows) {
		return Content{}, notFound("Окружение", id)
	}
	if err != nil {
		return Content{}, err
	}
	historical := false
	if cs != nil {
		change, err := q.GetChangeset(ctx, store.GetChangesetParams{ProjectID: project, ID: *cs})
		if errors.Is(err, pgx.ErrNoRows) {
			return Content{}, notFound("Change Set", *cs)
		}
		if err != nil {
			return Content{}, err
		}
		historical = change.State == "merged" || change.State == "abandoned"
	}
	v, err := q.ContentVersion(ctx, store.ContentVersionParams{ProjectID: project, ObjectID: id, EnvironmentID: e.ID, ChangesetID: cs, UseHead: useHead})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Content{}, err
	}
	if err != nil || v.Kind == "document" || (v.Deleted && cs == nil) {
		return Content{}, notFound("Контент", id)
	}
	if v.Kind == "entity" && !v.Deleted && !historical {
		c, err := validation.Load(ctx, q, project, env, cs)
		if err != nil {
			return Content{}, err
		}
		_, version, ok := c.Schema(*v.SchemaName)
		if !ok {
			return Content{}, commandbus.NewError(409, "CONTENT_SCHEMA_UNKNOWN", "Схема не найдена", *v.SchemaName)
		}
		if v.SchemaVersion == nil || *v.SchemaVersion != version {
			code := "SCHEMA_UPCAST_REQUIRED"
			if v.SchemaVersion != nil && *v.SchemaVersion > version {
				code = "SCHEMA_VERSION_AHEAD"
			}
			return Content{}, commandbus.NewError(409, code, "Версия сущности несовместима", code)
		}
	}
	return Content{ID: id, Kind: v.Kind, Schema: v.SchemaName, SchemaVersion: v.SchemaVersion, VersionID: v.ID, Deleted: v.Deleted, Data: v.Body}, nil
}
