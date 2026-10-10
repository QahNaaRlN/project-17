// Package zonepolicy derives approval requirements from changed render-tree nodes.
package zonepolicy

import (
	"encoding/json"
	"fmt"
	"slices"
)

type Result struct {
	Roles      []string
	Strict     bool
	References []map[string]any
}

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }
func array(v any) []any        { a, _ := v.([]any); return a }
func equal(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// Affected includes both old and new ancestors. usesChanged detects changes
// inside a live Composed dependency without treating pinned versions as live.
func Affected(before, after map[string]any, defaultMode string, usesChanged func(map[string]any) bool, global bool) Result {
	roles := map[string]bool{}
	out := Result{}
	a, b := obj(before["nodes"]), obj(after["nodes"])
	changed := map[string]bool{}
	locations := func(doc map[string]any) map[string]string {
		out := map[string]string{}
		var walk func(string, string)
		walk = func(id, path string) {
			if _, ok := out[id]; ok {
				return
			}
			out[id] = path
			n := obj(obj(doc["nodes"])[id])
			for i, v := range array(n["children"]) {
				walk(fmt.Sprint(v), fmt.Sprintf("%s/%s/children/%d", path, id, i))
			}
			for _, slot := range sortedKeys(obj(n["slots"])) {
				for i, v := range array(obj(n["slots"])[slot]) {
					walk(fmt.Sprint(v), fmt.Sprintf("%s/%s/slots/%s/%d", path, id, slot, i))
				}
			}
		}
		walk(fmt.Sprint(doc["root"]), "root")
		return out
	}
	oldLocations, newLocations := locations(before), locations(after)
	for id, path := range oldLocations {
		if newLocations[id] != path {
			changed[id] = true
		}
	}
	for id, path := range newLocations {
		if oldLocations[id] != path {
			changed[id] = true
		}
	}
	renderChanged := func(id string) bool {
		for _, key := range []string{"type", "props", "design", "zone", "when", "ref", "bindings", "on"} {
			if !equal(obj(a[id])[key], obj(b[id])[key]) {
				return true
			}
		}
		return false
	}
	for id, n := range a {
		if !equal(n, b[id]) {
			changed[id] = true
		}
	}
	for id, n := range b {
		if !equal(n, a[id]) {
			changed[id] = true
		}
	}
	localChanged := map[string]bool{}
	for id, v := range changed {
		localChanged[id] = v
	}
	for _, doc := range []map[string]any{before, after} {
		for id, n := range obj(doc["nodes"]) {
			if obj(n)["type"] == "Composed" && usesChanged != nil && usesChanged(obj(obj(n)["ref"])) {
				changed[id] = true
			}
		}
	}
	// Document-level changes affect every zone, even if the nodes themselves match.
	for _, key := range []string{"policy", "meta", "content", "dataSources", "root"} {
		if !equal(before[key], after[key]) {
			global = true
		}
	}
	for _, doc := range []map[string]any{before, after} {
		mode := defaultMode
		if m, ok := obj(doc["policy"])["mode"].(string); ok {
			mode = m
		}
		stack := map[string]bool{}
		var walk func(string, string, []string, bool) bool
		walk = func(id, mode string, inherited []string, ancestorChanged bool) bool {
			if stack[id] || len(stack) > 128 {
				return false
			}
			stack[id] = true
			defer delete(stack, id)
			n := obj(obj(doc["nodes"])[id])
			if n == nil {
				return false
			}
			zone := obj(n["zone"])
			if m, ok := zone["mode"].(string); ok {
				mode = m
			}
			if role, ok := zone["requiredApprovalRole"].(string); ok && role != "" {
				inherited = append(slices.Clone(inherited), role)
			}
			affected := global || ancestorChanged || changed[id]
			descendantChanged := ancestorChanged || renderChanged(id)
			for _, v := range array(n["children"]) {
				if walk(fmt.Sprint(v), mode, inherited, descendantChanged) {
					affected = true
				}
			}
			for _, v := range obj(n["slots"]) {
				for _, v := range array(v) {
					if walk(fmt.Sprint(v), mode, inherited, descendantChanged) {
						affected = true
					}
				}
			}
			if n["type"] == "Composed" && (global || ancestorChanged || localChanged[id] || affected && len(obj(n["slots"])) > 0) {
				out.References = append(out.References, obj(n["ref"]))
			}
			if affected {
				for _, r := range inherited {
					roles[r] = true
				}
				if mode == "STRICT" {
					out.Strict = true
				}
			}
			return affected
		}
		walk(fmt.Sprint(doc["root"]), mode, nil, false)
	}
	for role := range roles {
		out.Roles = append(out.Roles, role)
	}
	slices.Sort(out.Roles)
	return out
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
