// Package policydoc checks L5–L6 without storage or mutations.
package policydoc

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
)

type Mode int

const (
	Strict Mode = iota
	System
	Free
	Code
)

func (m Mode) String() string { return []string{"STRICT", "SYSTEM", "FREE", "CODE"}[m] }
func mode(v any) (Mode, bool) {
	for i, s := range []string{"STRICT", "SYSTEM", "FREE", "CODE"} {
		if v == s {
			return Mode(i), true
		}
	}
	return 0, false
}

type Component struct {
	RequiredMode Mode
	Certified    bool
}
type Options struct {
	Project          map[string]any
	ResolveComponent func(map[string]any) (Component, bool)
}
type Result struct {
	ir.Result
	RequiredMode Mode
}
type policy struct {
	mode, max Mode
	allows    [][]string
	denies    []string
	rawColors bool
}
type checker struct {
	out     Result
	app     map[string]any
	options Options
}

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }
func keys(m map[string]any) []string {
	k := make([]string, 0, len(m))
	for s := range m {
		k = append(k, s)
	}
	slices.Sort(k)
	return k
}
func strs(v any) []string {
	var out []string
	for _, v := range array(v) {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
func array(v any) []any { a, _ := v.([]any); return a }
func (c *checker) error(code, path, id string, params map[string]any) {
	c.out.Valid = false
	c.out.Diagnostics = append(c.out.Diagnostics, ir.Diagnostic{Code: ir.Code(code), Severity: ir.SeverityError, Pointer: path, NodeID: id, Message: code, Params: params})
}
func (c *checker) level(p policy, v map[string]any, path, id string) policy {
	if v == nil {
		return p
	}
	if x, exists := v["mode"]; exists {
		m, ok := mode(x)
		if !ok {
			c.error("POLICY_MODE_INVALID", path+"/mode", id, nil)
		} else {
			p.mode = m
			p.max = m
		}
	}
	if x, exists := v["maxMode"]; exists {
		m, ok := mode(x)
		if !ok {
			c.error("POLICY_MODE_INVALID", path+"/maxMode", id, nil)
		} else {
			p.max = m
		}
	}
	if p.mode > p.max {
		c.error("POLICY_MODE_EXCEEDS_MAX", path, id, nil)
	}
	if a, exists := v["allowedTypes"]; exists {
		p.allows = append(slices.Clone(p.allows), strs(a))
	}
	p.denies = append(slices.Clone(p.denies), strs(v["deniedTypes"])...)
	if b, ok := v["allowRawColors"].(bool); ok {
		p.rawColors = b
	}
	return p
}
func (c *checker) child(p policy, v map[string]any, path, id string) policy {
	n := c.level(p, v, path, id)
	if n.mode > p.max || n.max > p.max {
		c.error("POLICY_MODE_EXCEEDS_PARENT", path, id, map[string]any{"maxMode": p.max.String()})
	}
	return n
}
func Validate(doc map[string]any, app any, o Options) Result {
	c := checker{app: obj(app), options: o, out: Result{Result: ir.Result{Valid: true}}}
	if v, exists := o.Project["limits"]; exists && obj(v) == nil {
		c.error("POLICY_LIMIT_INVALID", "/project/settings/limits", "", nil)
	}
	if v, exists := o.Project["allowRawColors"]; exists {
		if _, ok := v.(bool); !ok {
			c.error("POLICY_SETTINGS_INVALID", "/project/settings/allowRawColors", "", nil)
		}
	}
	for _, key := range keys(obj(o.Project["limits"])) {
		limit := obj(obj(o.Project["limits"])[key])
		r, ok := catalogue[key]
		if !ok || r.raw != "length" || limit == nil {
			c.error("POLICY_LIMIT_INVALID", "/project/settings/limits/"+key, "", nil)
			continue
		}
		low, high := r.low, r.high
		for field, v := range limit {
			n, ok := number(v)
			if !ok || (field != "min" && field != "max") {
				c.error("POLICY_LIMIT_INVALID", "/project/settings/limits/"+key, "", nil)
				continue
			}
			if field == "min" {
				low = n
			} else {
				high = n
			}
		}
		if low < r.low || high > r.high || low > high {
			c.error("POLICY_LIMIT_INVALID", "/project/settings/limits/"+key, "", nil)
		}
	}
	project := map[string]any{}
	for k, v := range o.Project {
		project[k] = v
	}
	if m, ok := project["defaultMode"]; ok {
		project["mode"] = m
	}
	p := c.level(policy{mode: System, max: System}, project, "/project/settings", "")
	p = c.child(p, obj(doc["policy"]), "/policy", "")
	nodes := obj(doc["nodes"])
	var walk func(string, string, policy)
	walk = func(id, parent string, p policy) {
		n := obj(nodes[id])
		if n == nil {
			return
		}
		p = c.child(p, obj(n["zone"]), ir.Pointer("nodes", id, "zone"), id)
		c.out.RequiredMode = max(c.out.RequiredMode, p.mode)
		typ, _ := n["type"].(string)
		identity := typ
		if typ == "Composed" {
			identity = fmt.Sprint(obj(n["ref"])["component"])
		}
		allowed := true
		explicit := false
		for _, list := range p.allows {
			match := slices.Contains(list, typ) || slices.Contains(list, identity)
			allowed = allowed && match
			explicit = explicit || slices.Contains(list, identity)
		}
		if !allowed || slices.Contains(p.denies, typ) || slices.Contains(p.denies, identity) {
			c.error("POLICY_TYPE_FORBIDDEN", ir.Pointer("nodes", id, "type"), id, map[string]any{"type": identity})
		}
		if typ == "Composed" {
			if o.ResolveComponent != nil {
				if comp, ok := o.ResolveComponent(obj(n["ref"])); ok {
					req := comp.RequiredMode
					if comp.Certified {
						req = Strict
					}
					c.out.RequiredMode = max(c.out.RequiredMode, req)
					if !explicit && (p.mode < req || (p.mode == Strict && !comp.Certified)) {
						c.error("POLICY_COMPONENT_MODE_REQUIRED", ir.Pointer("nodes", id, "ref"), id, map[string]any{"requiredMode": req.String(), "certified": comp.Certified})
					}
				}
			}
		} else if p.mode == Strict && c.app != nil {
			if _, ok := obj(c.app["components"])[typ]; !ok {
				c.error("POLICY_TYPE_MODE_REQUIRED", ir.Pointer("nodes", id, "type"), id, map[string]any{"mode": p.mode.String()})
			}
		}
		if p.mode < Code && pending(n["on"]) {
			c.error("POLICY_PENDING_REQUIRES_CODE", ir.Pointer("nodes", id, "on"), id, nil)
		}
		if c.app != nil {
			c.design(n, typ, parent, p, id)
		}
		for _, v := range array(n["children"]) {
			walk(fmt.Sprint(v), typ, p)
		}
		for _, slot := range keys(obj(n["slots"])) {
			for _, v := range array(obj(n["slots"])[slot]) {
				walk(fmt.Sprint(v), typ, p)
			}
		}
	}
	walk(fmt.Sprint(doc["root"]), "", p)
	return c.out
}
func pending(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			if k == "action" && strings.HasPrefix(fmt.Sprint(v), "pending:") {
				return true
			}
			if pending(v) {
				return true
			}
		}
	case []any:
		for _, v := range x {
			if pending(v) {
				return true
			}
		}
	}
	return false
}

// CheckEdit examines both trees. Removing a locked ancestor or introducing a
// lock and editing it in the same operation cannot evade the structural guard.
func CheckEdit(before, after map[string]any, manage bool) ir.Result {
	r := ir.Result{Valid: true}
	if manage {
		return r
	}
	fail := func(id, path string) {
		r.Valid = false
		r.Diagnostics = append(r.Diagnostics, ir.Diagnostic{Code: "POLICY_ZONES_MANAGE_REQUIRED", Severity: ir.SeverityError, Pointer: path, NodeID: id, Message: "Изменение защищённой зоны требует design.zones.manage"})
	}
	if !reflect.DeepEqual(before["policy"], after["policy"]) {
		fail("", "/policy")
	}
	a, b := obj(before["nodes"]), obj(after["nodes"])
	protected := func(doc map[string]any) map[string]bool {
		out := map[string]bool{}
		var walk func(string, bool)
		walk = func(id string, lock bool) {
			n := obj(obj(doc["nodes"])[id])
			if n == nil {
				return
			}
			lock = lock || n["locked"] == true || obj(n["zone"])["locked"] == true
			out[id] = lock
			for _, v := range array(n["children"]) {
				walk(fmt.Sprint(v), lock)
			}
			for _, v := range obj(n["slots"]) {
				for _, v := range array(v) {
					walk(fmt.Sprint(v), lock)
				}
			}
		}
		walk(fmt.Sprint(doc["root"]), false)
		return out
	}
	pa, pb := protected(before), protected(after)
	locations := func(doc map[string]any) map[string]string {
		out := map[string]string{}
		var walk func(string, string)
		walk = func(id, path string) {
			out[id] = path
			n := obj(obj(doc["nodes"])[id])
			for i, v := range array(n["children"]) {
				walk(fmt.Sprint(v), fmt.Sprint(path, "/children/", i))
			}
			for slot, v := range obj(n["slots"]) {
				for i, v := range array(v) {
					walk(fmt.Sprint(v), fmt.Sprint(path, "/slots/", slot, "/", i))
				}
			}
		}
		walk(fmt.Sprint(doc["root"]), fmt.Sprint(doc["root"]))
		return out
	}
	la, lb := locations(before), locations(after)
	ids := map[string]bool{}
	for id := range a {
		ids[id] = true
	}
	for id := range b {
		ids[id] = true
	}
	for _, id := range keysBool(ids) {
		x, y := obj(a[id]), obj(b[id])
		if !reflect.DeepEqual(x["zone"], y["zone"]) || !reflect.DeepEqual(x["locked"], y["locked"]) {
			fail(id, ir.Pointer("nodes", id, "zone"))
			continue
		}
		if pa[id] || pb[id] {
			if la[id] != lb[id] {
				fail(id, ir.Pointer("nodes", id))
				continue
			}
			for _, field := range []string{"type", "children", "slots"} {
				if !reflect.DeepEqual(x[field], y[field]) {
					fail(id, ir.Pointer("nodes", id, field))
					break
				}
			}
		}
	}
	return r
}
func keysBool(m map[string]bool) []string {
	k := make([]string, 0, len(m))
	for s := range m {
		k = append(k, s)
	}
	slices.Sort(k)
	return k
}
func nativeContainer(app map[string]any, typ string) bool {
	m := manifest.BuiltinContract(typ)
	if m == nil {
		m = obj(obj(app["components"])[typ])
		if m == nil {
			m = obj(obj(app["primitives"])[typ])
		}
	}
	return m["container"] == true
}
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case json.Number:
		n, e := x.Float64()
		return n, e == nil
	case int:
		return float64(x), true
	}
	return 0, false
}
