package validation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Incremental publication checks (docs/incremental-validation.md, STU-562…568).

// CrossCheckEnv, set to "1", makes every incremental check also run the full one and fail
// on a difference (STU-568). Test support sets it for the whole Go test suite.
const CrossCheckEnv = "CMS_VALIDATION_CROSSCHECK"

// maxAffected — the expansion limit of Validate; a larger affected set falls back to the
// full check instead of failing the publication.
var maxAffected = 10000

var errTooMany = errors.New("validation: affected set exceeds limit")

// Basis is the hash of what a check result depends on besides content: the manifest and
// the project settings (STU-566).
func Basis(manifestHash string, settings []byte) []byte {
	h := sha256.New()
	h.Write([]byte(manifestHash))
	h.Write([]byte{0})
	h.Write(settings)
	return h.Sum(nil)
}

func (c *Context) basis() []byte { return Basis(c.ManifestHash, c.settings) }

// RecordBasis marks the environment as checked under the context's basis. Publish, promote
// and rollback call it after their check passed: the environment then is the checked state.
func (c *Context) RecordBasis(ctx context.Context) error {
	return c.q.SetEnvironmentValidationBasis(ctx, store.SetEnvironmentValidationBasisParams{ID: c.Environment.ID, ValidationBasis: c.basis()})
}

func (c *Context) incremental() bool {
	return c.Candidate == nil && c.Environment.ValidationBasis != nil && bytes.Equal(c.Environment.ValidationBasis, c.basis())
}

// checkAffected validates the batch and the published objects that depend on it (STU-562…565).
func (c *Context) checkAffected(ctx context.Context, docs []Document) (map[string][]ir.Diagnostic, map[string]bool, error) {
	c.Draft = false
	c.Overrides = map[uuid.UUID]Document{}
	deleted, err := c.q.ValidationPublishedDeleted(ctx, c.Environment.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, r := range deleted {
		c.Overrides[r.ObjectID] = Document{ObjectID: r.ObjectID, VersionID: r.VersionID, Deleted: true}
	}
	affected := map[string]bool{}
	var batch []uuid.UUID
	add := func(id uuid.UUID) {
		if !affected[id.String()] {
			affected[id.String()] = true
			batch = append(batch, id)
		}
	}
	inDocs := map[uuid.UUID]bool{}
	for _, d := range docs {
		c.Overrides[d.ObjectID] = d
		inDocs[d.ObjectID] = true
		add(d.ObjectID)
	}
	for _, id := range c.Changed {
		add(id)
	}
	dependents, err := c.q.ValidationDependents(ctx, store.ValidationDependentsParams{EnvironmentID: c.Environment.ID, Objects: batch, ProjectID: c.ProjectID})
	if err != nil {
		return nil, nil, err
	}
	if len(dependents) > maxAffected {
		return nil, nil, errTooMany
	}
	var fetch []uuid.UUID
	for _, id := range append(slices.Clone(c.Changed), dependents...) {
		if !inDocs[id] {
			fetch = append(fetch, id)
		}
		affected[id.String()] = true
	}
	rows, err := c.q.ValidationPublishedVersionsByID(ctx, store.ValidationPublishedVersionsByIDParams{ProjectID: c.ProjectID, EnvironmentID: c.Environment.ID, Objects: fetch})
	if err != nil {
		return nil, nil, err
	}
	all := map[uuid.UUID]Document{}
	for _, r := range rows {
		if !r.Deleted {
			all[r.ObjectID] = Document{ObjectID: r.ObjectID, VersionID: r.VersionID, Body: r.Body, Path: r.Path, Certified: r.Certified}
		}
	}
	for _, d := range docs {
		all[d.ObjectID] = d
		if d.Deleted {
			delete(all, d.ObjectID)
		}
	}

	var out map[string][]ir.Diagnostic
	if c.PublishedOnly {
		out, err = c.constraints(ctx, batch, docs, true)
	} else {
		var heads []uuid.UUID
		var headDocs []Document
		if !c.PreserveHead {
			headDocs = docs
			heads = batch
		}
		out, err = c.constraints(ctx, heads, headDocs, false)
	}
	if err != nil {
		return nil, nil, err
	}
	for _, id := range sortedUUIDs(all) {
		r, err := c.Validate(ctx, all[id])
		if err != nil {
			return nil, nil, err
		}
		if len(r.Diagnostics) > 0 {
			out[id.String()] = append(out[id.String()], r.Diagnostics...)
		}
	}
	return out, affected, nil
}

// constraints checks singleton and unique for the batch only (STU-565): each batch entity
// of a constrained schema is compared with every entity of that schema — the project heads,
// or the environment's published versions for rollback (published).
func (c *Context) constraints(ctx context.Context, batch []uuid.UUID, docs []Document, published bool) (map[string][]ir.Diagnostic, error) {
	out := map[string][]ir.Diagnostic{}
	schemas := map[string]bool{}
	for _, id := range batch {
		o, err := c.q.ContentIdentity(ctx, store.ContentIdentityParams{ProjectID: c.ProjectID, ID: id})
		if err != nil {
			return nil, err
		}
		if o.Kind != "entity" || o.SchemaName == nil {
			continue
		}
		if s, _, ok := c.Schema(*o.SchemaName); ok && constrained(s) {
			schemas[*o.SchemaName] = true
		}
	}
	if len(schemas) == 0 {
		return out, nil
	}
	names := make([]string, 0, len(schemas))
	for name := range schemas {
		names = append(names, name)
	}
	slices.Sort(names)
	type item struct {
		schema string
		data   map[string]any
	}
	items := map[uuid.UUID]item{}
	if published {
		rows, err := c.q.ValidationPublishedEntities(ctx, store.ValidationPublishedEntitiesParams{EnvironmentID: c.Environment.ID, ProjectID: c.ProjectID, Schemas: names})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if !r.Deleted {
				data, _ := decode(r.Body)
				items[r.ObjectID] = item{*r.SchemaName, data}
			}
		}
	} else {
		rows, err := c.q.ContentHeadsBySchema(ctx, store.ContentHeadsBySchemaParams{ProjectID: c.ProjectID, Schemas: names})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if !r.Deleted {
				data, _ := decode(r.Body)
				items[r.ID] = item{*r.SchemaName, data}
			}
		}
	}
	for _, d := range docs {
		o, err := c.q.ContentIdentity(ctx, store.ContentIdentityParams{ProjectID: c.ProjectID, ID: d.ObjectID})
		if err != nil {
			return nil, err
		}
		if o.Kind != "entity" || o.SchemaName == nil || !schemas[*o.SchemaName] {
			continue
		}
		removed := d.Deleted
		if !published {
			v, err := c.q.GetVersion(ctx, d.VersionID)
			if err != nil {
				return nil, err
			}
			removed = v.Deleted
		}
		delete(items, d.ObjectID)
		if !removed {
			data, _ := decode(d.Body)
			items[d.ObjectID] = item{*o.SchemaName, data}
		}
	}
	bySchema := map[string][]uuid.UUID{}
	for _, id := range sortedUUIDs(items) {
		bySchema[items[id].schema] = append(bySchema[items[id].schema], id)
	}
	for _, id := range sortedUUIDs(setOf(batch)) {
		x, ok := items[id]
		if !ok {
			continue
		}
		s, _, _ := c.Schema(x.schema)
		others := slices.DeleteFunc(slices.Clone(bySchema[x.schema]), func(o uuid.UUID) bool { return o == id })
		if s["singleton"] == true && len(others) > 0 {
			out[id.String()] = append(out[id.String()], problem("CONTENT_SINGLETON", "", "", map[string]any{"objects": others}).Diagnostics...)
		}
		for _, key := range sortedKeys(object(s["fields"])) {
			f := object(object(s["fields"])[key])
			if f["unique"] != true || x.data[key] == nil {
				continue
			}
			for _, other := range others {
				if collides(f, x.data[key], items[other].data[key]) {
					out[id.String()] = append(out[id.String()], problem("CONTENT_UNIQUE", ir.Pointer(key), "", map[string]any{"object": other}).Diagnostics...)
				}
			}
		}
	}
	return out, nil
}

func constrained(schema map[string]any) bool {
	if schema["singleton"] == true {
		return true
	}
	for _, f := range object(schema["fields"]) {
		if object(f)["unique"] == true {
			return true
		}
	}
	return false
}

// collides compares a unique field; a localized field collides on any shared locale value.
func collides(field map[string]any, a, b any) bool {
	if field["localized"] != true {
		return reflect.DeepEqual(a, b)
	}
	for locale, v := range object(a) {
		if v != nil && reflect.DeepEqual(v, object(b)[locale]) {
			return true
		}
	}
	return false
}

func setOf(ids []uuid.UUID) map[uuid.UUID]bool {
	m := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// crossCheck runs the full check and compares (STU-568): both agree on whether the
// publication is blocked, diagnostics of affected objects match, and the full check finds no
// error outside them. Singleton and unique diagnostics are placed differently (on the batch
// object instead of the later one) and are compared only through the blocking result.
func (c *Context) crossCheck(ctx context.Context, docs []Document, out map[string][]ir.Diagnostic, affected map[string]bool) error {
	full := *c
	all, err := full.checkAll(ctx, docs)
	if err != nil {
		return err
	}
	if HasErrors(out) != HasErrors(all) {
		return fmt.Errorf("validation cross-check: incremental blocked=%v, full blocked=%v: %v / %v", HasErrors(out), HasErrors(all), out, all)
	}
	keys := map[string]bool{}
	for k := range out {
		keys[k] = true
	}
	for k := range all {
		keys[k] = true
	}
	for k := range keys {
		a, f := placed(out[k]), placed(all[k])
		if affected[k] {
			if !reflect.DeepEqual(a, f) {
				return fmt.Errorf("validation cross-check: %s: incremental %v, full %v", k, a, f)
			}
		} else if HasErrors(map[string][]ir.Diagnostic{k: f}) {
			return fmt.Errorf("validation cross-check: %s is outside the affected set but fails the full check: %v", k, f)
		}
	}
	return nil
}

func placed(ds []ir.Diagnostic) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, d := range ds {
		if d.Code != "CONTENT_UNIQUE" && d.Code != "CONTENT_SINGLETON" {
			out = append(out, d)
		}
	}
	return out
}

// PublishedDependents returns the published versions of objects and of the published
// consumers of components, transitively through components (STU-567). all, or a consumer
// set over the limit, reads the whole environment.
func PublishedDependents(ctx context.Context, q *store.Queries, project, env uuid.UUID, objects, components []uuid.UUID, all bool) ([]store.ValidationPublishedVersionsRow, error) {
	if all {
		return q.ValidationPublishedVersions(ctx, store.ValidationPublishedVersionsParams{ProjectID: project, ID: env})
	}
	ids := slices.Clone(objects)
	if len(components) > 0 {
		deps, err := q.ValidationDependents(ctx, store.ValidationDependentsParams{EnvironmentID: env, Objects: components, ProjectID: project})
		if err != nil {
			return nil, err
		}
		if len(deps) > maxAffected {
			return PublishedDependents(ctx, q, project, env, nil, nil, true)
		}
		ids = append(ids, deps...)
	}
	rows, err := q.ValidationPublishedVersionsByID(ctx, store.ValidationPublishedVersionsByIDParams{ProjectID: project, EnvironmentID: env, Objects: ids})
	out := make([]store.ValidationPublishedVersionsRow, len(rows))
	for i, r := range rows {
		out[i] = store.ValidationPublishedVersionsRow(r)
	}
	return out, err
}

// CrossChecked runs f on the dependents (all=false) and, under CrossCheckEnv, also on the
// whole environment, failing when the results differ (STU-568).
func CrossChecked[T any](f func(all bool) (T, error)) (T, error) {
	r, err := f(false)
	if err != nil || os.Getenv(CrossCheckEnv) != "1" {
		return r, err
	}
	full, err := f(true)
	if err != nil {
		return r, err
	}
	if !reflect.DeepEqual(r, full) {
		return r, fmt.Errorf("validation cross-check: dependents %v, whole environment %v", r, full)
	}
	return r, nil
}
