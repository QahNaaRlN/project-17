package validation

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/zonepolicy"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// ZoneRequirements includes edited documents and affected published live consumers.
func ZoneRequirements(ctx context.Context, q *store.Queries, project, cs uuid.UUID, targets []string) (zonepolicy.Result, error) {
	versions, err := q.ChangesetWorkingVersions(ctx, cs)
	if err != nil {
		return zonepolicy.Result{}, err
	}
	settings, err := q.GetProjectSettings(ctx, project)
	if err != nil {
		return zonepolicy.Result{}, err
	}
	policy, err := decode(settings)
	if err != nil {
		return zonepolicy.Result{}, err
	}
	mode := "SYSTEM"
	if m, ok := policy["defaultMode"].(string); ok {
		mode = m
	}
	changed := map[uuid.UUID]bool{}
	docs := map[uuid.UUID]Document{}
	before := map[uuid.UUID]Document{}
	for _, v := range versions {
		docs[v.ObjectID] = Document{ObjectID: v.ObjectID, VersionID: v.VersionID, Body: v.Body, Path: v.Path, Certified: v.Certified}
		body, err := decode(v.Body)
		if err != nil {
			return zonepolicy.Result{}, err
		}
		if body["kind"] == "component" {
			changed[v.ObjectID] = true
		}
		if v.BaseVersionID != nil {
			base, err := q.GetVersion(ctx, *v.BaseVersionID)
			if err != nil {
				return zonepolicy.Result{}, err
			}
			before[v.ObjectID] = Document{Body: base.Body, Path: base.Path}
		}
	}
	merged := zonepolicy.Result{}
	roles := map[string]bool{}
	add := func(r zonepolicy.Result) {
		merged.Strict = merged.Strict || r.Strict
		for _, s := range r.Roles {
			roles[s] = true
		}
	}
	for id, d := range docs {
		a, err := decode(before[id].Body)
		if len(before[id].Body) == 0 {
			a = map[string]any{}
			err = nil
		}
		if err != nil {
			return merged, err
		}
		b, err := decode(d.Body)
		if err != nil {
			return merged, err
		}
		add(zonepolicy.Affected(a, b, mode, nil, !sameString(before[id].Path, d.Path)))
	}
	for _, target := range targets {
		c, err := Load(ctx, q, project, target, &cs)
		if err != nil {
			return merged, err
		}
		c.Overrides = docs
		rows, err := q.ValidationPublishedVersions(ctx, store.ValidationPublishedVersionsParams{ProjectID: project, ID: c.Environment.ID})
		if err != nil {
			return merged, err
		}
		cache := map[string]bool{}
		stack := map[string]bool{}
		count := 0
		var resolveErr error
		var consume func(zonepolicy.Result, int)
		var uses func(map[string]any) bool
		uses = func(ref map[string]any) bool {
			key := fmt.Sprint(ref)
			if v, ok := cache[key]; ok {
				return v
			}
			count++
			if count > 10000 || len(stack) > 128 {
				resolveErr = fmt.Errorf("zone approval dependency expansion exceeds limit")
				return false
			}
			id, err := uuid.Parse(fmt.Sprint(ref["component"]))
			if err != nil {
				return false
			}
			if ref["version"] == "live" && changed[id] {
				cache[key] = true
				return true
			}
			if stack[key] {
				return false
			}
			stack[key] = true
			defer delete(stack, key)
			child, ok, err := c.resolve(ctx, ref)
			if err != nil {
				resolveErr = err
				return false
			}
			if !ok {
				return false
			}
			body, err := decode(child.Body)
			if err != nil {
				resolveErr = err
				return false
			}
			affected := false
			for _, n := range object(body["nodes"]) {
				if object(n)["type"] == "Composed" && uses(object(object(n)["ref"])) {
					affected = true
				}
			}
			cache[key] = affected
			// Exact pinned wrappers can themselves contain zones around changed live children.
			// Gather their internal requirements even when that historical wrapper is not published.
			consume(zonepolicy.Affected(body, body, mode, uses, false), len(stack))
			return affected
		}
		expanded := map[uuid.UUID]bool{}
		var expandErr error
		consume = func(r zonepolicy.Result, depth int) {
			if depth > 128 {
				expandErr = fmt.Errorf("zone composition depth exceeds limit")
				return
			}
			add(r)
			for _, ref := range r.References {
				child, ok, err := c.resolve(ctx, ref)
				if err != nil {
					expandErr = err
					return
				}
				if !ok || expanded[child.VersionID] {
					continue
				}
				expanded[child.VersionID] = true
				if len(expanded) > 10000 {
					expandErr = fmt.Errorf("zone composition expansion exceeds limit")
					return
				}
				body, err := decode(child.Body)
				if err != nil {
					expandErr = err
					return
				}
				consume(zonepolicy.Affected(body, body, mode, nil, true), depth+1)
			}
		}
		for _, row := range rows {
			a, err := decode(row.Body)
			if err != nil {
				return merged, err
			}
			b := a
			global := false
			if d, ok := docs[row.ObjectID]; ok {
				global = !sameString(row.Path, d.Path)
				b, err = decode(d.Body)
				if err != nil {
					return merged, err
				}
			}
			consume(zonepolicy.Affected(a, b, mode, uses, global), 0)
		}
		for id, d := range docs {
			a := map[string]any{}
			if len(before[id].Body) > 0 {
				a, err = decode(before[id].Body)
				if err != nil {
					return merged, err
				}
			}
			b, err := decode(d.Body)
			if err != nil {
				return merged, err
			}
			consume(zonepolicy.Affected(a, b, mode, uses, !sameString(before[id].Path, d.Path)), 0)
		}
		if expandErr != nil {
			return merged, expandErr
		}
		if resolveErr != nil {
			return merged, resolveErr
		}
	}
	for role := range roles {
		merged.Roles = append(merged.Roles, role)
	}
	slices.Sort(merged.Roles)
	return merged, nil
}
func sameString(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
