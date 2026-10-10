// Package validation supplies the project/environment/Change Set context for L1–L6.
// All reads use the caller's transaction. It never writes content or activates manifests.
package validation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/a11ydoc"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/bindingdoc"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifestdoc"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/policydoc"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

type Document struct {
	ObjectID  uuid.UUID
	VersionID uuid.UUID
	Certified bool
	Body      []byte
	Path      *string
}

type Context struct {
	q            *store.Queries
	ProjectID    uuid.UUID
	Environment  store.Environment
	ChangesetID  *uuid.UUID
	ManifestID   *uuid.UUID
	ManifestHash string
	Candidate    *store.ValidationCandidateRow
	app          any
	project      map[string]any
	// Overrides is the prospective publication, including promotion's exact versions.
	Overrides    map[uuid.UUID]Document
	PreserveHead bool // promotion moves published pointers only
	Draft        bool
	UseHead      bool
}

// LockProjectEnvironments keeps the environment -> CS -> object lock order for workflow
// commands that check several target environments. ListEnvironments is name ordered.
func LockProjectEnvironments(ctx context.Context, q *store.Queries, project uuid.UUID) error {
	envs, err := q.ListEnvironments(ctx, project)
	if err != nil {
		return err
	}
	for _, e := range envs {
		if _, err := q.LockManifestEnvironment(ctx, store.LockManifestEnvironmentParams{ProjectID: project, Name: e.Name}); err != nil {
			return err
		}
	}
	return nil
}

// Load locks the environment first: manifest and published pointers cannot change during
// validation/publication. A candidate is accepted only for its linked schema CS and base.
func Load(ctx context.Context, q *store.Queries, project uuid.UUID, environment string, cs *uuid.UUID) (*Context, error) {
	env, err := q.LockManifestEnvironment(ctx, store.LockManifestEnvironmentParams{ProjectID: project, Name: environment})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Окружение не найдено", environment)
	}
	if err != nil {
		return nil, err
	}
	c := &Context{q: q, ProjectID: project, Environment: env, ChangesetID: cs, Draft: cs != nil, ManifestID: env.ActiveManifestID, Overrides: map[uuid.UUID]Document{}}
	settings, err := q.GetProjectSettings(ctx, project)
	if err != nil {
		return nil, err
	}
	c.project, err = decode(settings)
	if err != nil {
		return nil, err
	}
	var raw []byte
	if cs != nil {
		change, err := q.GetChangeset(ctx, store.GetChangesetParams{ID: *cs, ProjectID: project})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Change Set не найден", "Change Set не принадлежит проекту")
		}
		if err != nil {
			return nil, err
		}
		row, err := q.ValidationCandidate(ctx, store.ValidationCandidateParams{ProjectID: project, ChangesetID: *cs})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if errors.Is(err, pgx.ErrNoRows) && change.Kind == "schema" {
			return nil, commandbus.NewError(http.StatusConflict, "MANIFEST_CANDIDATE_REQUIRED", "Нет связанного кандидата", "Схемный Change Set проверяется только по связанному manifest")
		}
		if err == nil {
			if row.Kind != "schema" || row.EnvironmentID != env.ID || !sameID(row.BaseManifestID, env.ActiveManifestID) {
				return nil, commandbus.NewError(http.StatusConflict, "MANIFEST_CANDIDATE_MISMATCH", "Кандидат не соответствует окружению", "Проверьте схемный Change Set, окружение и исходный manifest")
			}
			c.Candidate = &row
			c.ManifestID = &row.ManifestID
			c.ManifestHash = row.Hash
			raw = row.Body
		}
	}
	if raw == nil && c.ManifestID != nil {
		m, err := q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: project, ID: *c.ManifestID})
		if err != nil {
			return nil, err
		}
		raw = m.Body
		c.ManifestHash = m.Hash
	}
	if raw != nil {
		c.app, err = manifest.ParseJSON(raw)
		if err != nil {
			return nil, err
		}
	}
	return c, nil
}

func sameID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func decode(raw []byte) (map[string]any, error) {
	var d map[string]any
	err := json.Unmarshal(raw, &d)
	return d, err
}

func (c *Context) resolve(ctx context.Context, ref map[string]any) (Document, bool, error) {
	id, err := uuid.Parse(fmt.Sprint(ref["component"]))
	if err != nil {
		return Document{}, false, nil
	}
	var number int32
	if v, ok := ref["version"].(float64); ok {
		if v < 1 || v > 2147483647 || v != float64(int32(v)) {
			return Document{}, false, nil
		}
		number = int32(v)
	}
	if number == 0 {
		if d, ok := c.Overrides[id]; ok {
			return d, true, nil
		}
	}
	row, err := c.q.ValidationComponent(ctx, store.ValidationComponentParams{ProjectID: c.ProjectID, ObjectID: id, Number: number, ChangesetID: c.ChangesetID, UseHead: c.UseHead, EnvironmentID: c.Environment.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, false, nil
	}
	if err != nil {
		return Document{}, false, err
	}
	return Document{ObjectID: id, VersionID: row.ID, Body: row.Body, Path: row.Path, Certified: row.Certified}, true, nil
}

// Validate checks every exact transitive component version, detects cycles by object
// identity (IR-062), and includes the originating component/version in nested diagnostics.
func (c *Context) Validate(ctx context.Context, d Document) (ir.Result, error) {
	o, err := c.q.ContentIdentity(ctx, store.ContentIdentityParams{ProjectID: c.ProjectID, ID: d.ObjectID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ir.Result{}, err
	}
	if err == nil && o.Kind != "document" {
		return c.validateContent(ctx, d, o)
	}

	stack := map[uuid.UUID]bool{}
	count := 0
	requirements := map[uuid.UUID]policydoc.Mode{}
	var visit func(Document, int) (ir.Result, error)
	visit = func(d Document, depth int) (ir.Result, error) {
		count++
		if depth > 128 || count > 10000 {
			return problem("LIMIT_EXCEEDED", "", "", map[string]any{"limit": "componentExpansion"}), nil
		}
		doc, err := decode(d.Body)
		if err != nil {
			return problem("IR_SCHEMA_VIOLATION", "", "", nil), nil
		}
		stack[d.ObjectID] = true
		defer delete(stack, d.ObjectID)
		resolved := map[string]map[string]any{}
		components := map[string]policydoc.Component{}
		extra := []ir.Diagnostic{}
		// L1 first keeps malformed documents out of reference traversal.
		if c.app == nil {
			r := manifestdoc.Validate(doc, nil, nil)
			if ir.ValidateDocument(doc).Valid {
				p := policydoc.Validate(doc, nil, policydoc.Options{Project: c.project})
				r.Diagnostics = append(r.Diagnostics, p.Diagnostics...)
			}
			return r, nil
		}
		if r := ir.ValidateDocument(doc); !r.Valid {
			return r, nil
		}
		nodes := object(doc["nodes"])
		keys := sortedKeys(nodes)
		for _, id := range keys {
			node := object(nodes[id])
			if node["type"] != "Composed" {
				continue
			}
			ref := object(node["ref"])
			key := ir.Pointer("nodes", id, "ref")
			child, ok, err := c.resolve(ctx, ref)
			if err != nil {
				return ir.Result{}, err
			}
			if !ok {
				continue
			}
			body, err := decode(child.Body)
			if err != nil {
				return ir.Result{}, err
			}
			if body["kind"] != "component" {
				continue
			}
			resolved[fmt.Sprint(ref)] = body
			if stack[child.ObjectID] {
				extra = append(extra, problem("COMPONENT_CYCLE", key, id, map[string]any{"componentId": child.ObjectID}).Diagnostics...)
				continue
			}
			r, err := visit(child, depth+1)
			if err != nil {
				return ir.Result{}, err
			}
			components[fmt.Sprint(ref)] = policydoc.Component{RequiredMode: requirements[child.VersionID], Certified: child.Certified}
			for _, diag := range r.Diagnostics {
				extra = append(extra, ir.Diagnostic{Code: diag.Code, Severity: diag.Severity, Pointer: key, NodeID: id, Message: diag.Message, Params: map[string]any{"componentId": child.ObjectID, "versionId": child.VersionID, "diagnostic": diag}})
			}
		}
		r := manifestdoc.Validate(doc, c.app, func(ref map[string]any) (map[string]any, bool) { m, ok := resolved[fmt.Sprint(ref)]; return m, ok })
		var pageError error
		bindings := bindingdoc.Validate(doc, c.app, bindingdoc.Options{Path: d.Path,
			ResolveComponent: func(ref map[string]any) (map[string]any, bool) { m, ok := resolved[fmt.Sprint(ref)]; return m, ok },
			ResolvePage: func(raw string) (*string, bool) {
				id, err := uuid.Parse(raw)
				if err != nil {
					return nil, false
				}
				if page, ok := c.Overrides[id]; ok {
					body, err := decode(page.Body)
					return page.Path, err == nil && body["kind"] == "page"
				}
				row, err := c.q.ValidationPage(ctx, store.ValidationPageParams{ProjectID: c.ProjectID, ObjectID: id, ChangesetID: c.ChangesetID, UseHead: c.UseHead, EnvironmentID: c.Environment.ID})
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					pageError = err
				}
				return row, err == nil
			}})
		if pageError != nil {
			return ir.Result{}, pageError
		}
		policy := policydoc.Validate(doc, c.app, policydoc.Options{Project: c.project, ResolveComponent: func(ref map[string]any) (policydoc.Component, bool) {
			v, ok := components[fmt.Sprint(ref)]
			return v, ok
		}})
		requirements[d.VersionID] = policy.RequiredMode
		r.Diagnostics = append(r.Diagnostics, policy.Diagnostics...)
		r.Diagnostics = append(r.Diagnostics, bindings.Diagnostics...)
		refs, err := c.References(ctx, doc)
		if err != nil {
			return ir.Result{}, err
		}
		r.Diagnostics = append(r.Diagnostics, refs.Diagnostics...)
		content := object(doc["content"])
		resolve := object(content["resolve"])
		if resolve["by"] == "entity" {
			typed, err := c.TypedReferences(ctx, map[string]any{"type": "reference", "schema": content["schema"]}, resolve, "/content/resolve")
			if err != nil {
				return ir.Result{}, err
			}
			r.Diagnostics = append(r.Diagnostics, typed.Diagnostics...)
		}

		r.Diagnostics = append(r.Diagnostics, extra...)
		sort.SliceStable(r.Diagnostics, func(i, j int) bool {
			if r.Diagnostics[i].Pointer == r.Diagnostics[j].Pointer {
				return r.Diagnostics[i].Code < r.Diagnostics[j].Code
			}
			return r.Diagnostics[i].Pointer < r.Diagnostics[j].Pointer
		})
		for _, d := range r.Diagnostics {
			if d.Severity == ir.SeverityError {
				r.Valid = false
			}
		}
		return r, nil
	}
	r, err := visit(d, 0)
	if err != nil {
		return r, err
	}
	doc, err := decode(d.Body)
	if err != nil || !ir.ValidateDocument(doc).Valid || c.app == nil {
		return r, nil
	}
	var resolveErr error
	a := a11ydoc.Validate(doc, c.app, func(ref map[string]any) (map[string]any, bool) {
		child, ok, err := c.resolve(ctx, ref)
		if err != nil {
			resolveErr = err
			return nil, false
		}
		if !ok {
			return nil, false
		}
		body, err := decode(child.Body)
		if err != nil {
			resolveErr = err
			return nil, false
		}
		return body, true
	})
	if resolveErr != nil {
		return ir.Result{}, resolveErr
	}
	r.Diagnostics = append(r.Diagnostics, a.Diagnostics...)
	sort.SliceStable(r.Diagnostics, func(i, j int) bool {
		if r.Diagnostics[i].Pointer == r.Diagnostics[j].Pointer {
			return r.Diagnostics[i].Code < r.Diagnostics[j].Code
		}
		return r.Diagnostics[i].Pointer < r.Diagnostics[j].Pointer
	})
	r.Valid = r.Valid && a.Valid
	return r, nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func problem(code, pointer, node string, params map[string]any) ir.Result {
	return ir.Result{Diagnostics: []ir.Diagnostic{{Code: ir.Code(code), Severity: ir.SeverityError, Pointer: pointer, NodeID: node, Message: code, Params: params}}}
}

// CheckPublication validates the prospective environment, including existing consumers
// of live components; replacements in the batch take precedence over current pointers.
func (c *Context) CheckPublication(ctx context.Context, docs []Document) (map[string][]ir.Diagnostic, error) {
	c.Draft = false
	c.Overrides = map[uuid.UUID]Document{}
	rows, err := c.q.ValidationPublishedVersions(ctx, store.ValidationPublishedVersionsParams{ProjectID: c.ProjectID, ID: c.Environment.ID})
	if err != nil {
		return nil, err
	}
	all := map[uuid.UUID]Document{}
	for _, r := range rows {
		all[r.ObjectID] = Document{ObjectID: r.ObjectID, VersionID: r.VersionID, Body: r.Body, Path: r.Path, Certified: r.Certified}
	}
	for _, d := range docs {
		all[d.ObjectID] = d
		c.Overrides[d.ObjectID] = d
	}
	ids := make([]uuid.UUID, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	headDocs := docs
	if c.PreserveHead {
		headDocs = nil
	}
	out, err := c.HeadConstraints(ctx, headDocs)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		r, err := c.Validate(ctx, all[id])
		if err != nil {
			return nil, err
		}
		if len(r.Diagnostics) > 0 {
			out[id.String()] = append(out[id.String()], r.Diagnostics...)
		}
	}
	return out, nil
}

func RequireValid(problems map[string][]ir.Diagnostic) error {
	if !HasErrors(problems) {
		return nil
	}
	return commandbus.NewError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "Документы не прошли проверку", "Изменения отклонены контрактом окружения").WithParams(map[string]any{"documents": problems})
}

func HasErrors(problems map[string][]ir.Diagnostic) bool {
	for _, ds := range problems {
		for _, d := range ds {
			if d.Severity == ir.SeverityError {
				return true
			}
		}
	}
	return false
}
