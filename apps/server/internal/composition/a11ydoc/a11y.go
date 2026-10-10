// Package a11ydoc checks static accessibility of the composed render tree (L7).
package a11ydoc

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
)

type Resolver func(map[string]any) (map[string]any, bool)
type view struct {
	node     map[string]any
	children []*view
	id, path string
	source   string
}
type fragment struct {
	doc        map[string]any
	ids        []any
	anchor, id string
	slots      map[string]fragment
	inputs     map[string]any
}
type builder struct {
	resolve Resolver
	count   int
	limited bool
	stack   map[string]bool
}

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }
func array(v any) []any        { a, _ := v.([]any); return a }
func keys(m map[string]any) []string {
	k := make([]string, 0, len(m))
	for s := range m {
		k = append(k, s)
	}
	slices.Sort(k)
	return k
}
func (b *builder) build(doc map[string]any, id, anchor, anchorID string, slots map[string]fragment, inputs map[string]any, depth int) []*view {
	b.count++
	if depth > 128 || b.count > 10000 {
		b.limited = true
		return nil
	}
	n := obj(obj(doc["nodes"])[id])
	if n == nil || n["design"] != nil && obj(n["design"])["hidden"] == true {
		return nil
	}
	if n["type"] == "Slot" {
		if f, ok := slots[fmt.Sprint(obj(n["props"])["name"])]; ok {
			var out []*view
			for _, v := range f.ids {
				out = append(out, b.build(f.doc, fmt.Sprint(v), f.anchor, f.id, f.slots, f.inputs, depth+1)...)
			}
			return out
		}
	}
	n = ops.Clone(n)
	for prop, binding := range obj(n["bindings"]) {
		s, ok := binding.(string)
		if m := obj(binding); m != nil {
			s, ok = m["expr"].(string)
		}
		if !ok || !strings.HasPrefix(s, "$props.") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(s, "$props."), ".")
		v, exists := inputs[parts[0]]
		for _, part := range parts[1:] {
			v, exists = obj(v)[part]
			if !exists {
				break
			}
		}
		if !exists || v == nil {
			if fallback, ok := obj(binding)["default"]; ok {
				v = fallback
				exists = true
			}
		}
		if !exists || v == nil {
			delete(obj(n["bindings"]), prop)
		} else if s, ok := v.(string); !ok || !strings.HasPrefix(s, "$") {
			delete(obj(n["bindings"]), prop)
			if obj(n["props"]) == nil {
				n["props"] = map[string]any{}
			}
			obj(n["props"])[prop] = v
		}
	}
	path := ir.Pointer("nodes", id)
	diagID := id
	source := ""
	if anchor != "" {
		source = path
		path = anchor
		diagID = anchorID
	}
	v := &view{node: n, id: diagID, path: path, source: source}
	if n["type"] == "Composed" && b.resolve != nil {
		ref := obj(n["ref"])
		key := fmt.Sprint(ref)
		if child, ok := b.resolve(ref); ok && !b.stack[key] && ir.ValidateDocument(child).Valid {
			b.stack[key] = true
			fills := map[string]fragment{}
			for name, ids := range obj(n["slots"]) {
				fills[name] = fragment{doc: doc, ids: array(ids), anchor: anchor, id: anchorID, slots: slots, inputs: inputs}
			}
			args := map[string]any{}
			for name, def := range obj(child["inputs"]) {
				if d, ok := obj(def)["default"]; ok {
					args[name] = d
				}
			}
			for name, value := range obj(n["props"]) {
				args[name] = value
			}
			for name, value := range obj(n["bindings"]) {
				args[name] = value
			}
			nextAnchor, nextID := anchor, anchorID
			if nextAnchor == "" {
				nextAnchor = path + "/ref"
				nextID = diagID
			}
			v.children = b.build(child, fmt.Sprint(child["root"]), nextAnchor, nextID, fills, args, depth+1)
			delete(b.stack, key)
		}
	} else {
		for _, child := range array(n["children"]) {
			v.children = append(v.children, b.build(doc, fmt.Sprint(child), anchor, anchorID, slots, inputs, depth+1)...)
		}
		for _, slot := range keys(obj(n["slots"])) {
			for _, child := range array(obj(n["slots"])[slot]) {
				v.children = append(v.children, b.build(doc, fmt.Sprint(child), anchor, anchorID, slots, inputs, depth+1)...)
			}
		}
	}
	return []*view{v}
}

type checker struct {
	out      ir.Result
	app      map[string]any
	h1, last int
	page     bool
}

func (c *checker) add(v *view, code, field string, severity ir.Severity, params map[string]any) {
	path := v.path
	if field != "" {
		path += "/" + field
	}
	if v.source != "" {
		if params == nil {
			params = map[string]any{}
		}
		params["sourcePointer"] = v.source + "/" + field
		path = v.path
	}
	c.out.Diagnostics = append(c.out.Diagnostics, ir.Diagnostic{Code: ir.Code(code), Severity: severity, Pointer: path, NodeID: v.id, Message: code, Params: params})
	if severity == ir.SeverityError {
		c.out.Valid = false
	}
}
func present(n map[string]any, key string) bool {
	if v, ok := obj(n["bindings"])[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s) != ""
		}
		return v != nil
	}
	if v, ok := obj(n["props"])[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s) != ""
		}
		return v != nil
	}
	return false
}
func click(n map[string]any) bool {
	v := obj(n["on"])["click"]
	if a, ok := v.([]any); ok {
		return len(a) > 0
	}
	return obj(v) != nil && obj(v)["action"] != nil
}
func interactive(n map[string]any) bool { return n["type"] == "Button" || n["type"] == "Link" }
func icons(v *view) bool {
	if v.node["type"] == "Icon" {
		return true
	}
	if len(v.children) == 0 {
		return false
	}
	for _, v := range v.children {
		if !icons(v) {
			return false
		}
	}
	return true
}
func integer(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		v, _ := n.Int64()
		return int(v)
	}
	return 0
}
func Validate(doc map[string]any, app any, resolve Resolver) ir.Result {
	c := checker{out: ir.Result{Valid: true}, app: obj(app), page: doc["kind"] == "page"}
	b := builder{resolve: resolve, stack: map[string]bool{}}
	roots := b.build(doc, fmt.Sprint(doc["root"]), "", "", nil, nil, 0)
	bp := "base"
	var walk func(*view, bool)
	walk = func(v *view, nested bool) {
		n := v.node
		if responsive(obj(n["design"])["hidden"], bp) == true {
			return
		}
		props := obj(n["props"])
		switch n["type"] {
		case "Image":
			if props["decorative"] != true && !present(n, "alt") {
				c.add(v, "A11Y_IMAGE_ALT_MISSING", "bindings/alt", ir.SeverityError, nil)
			}
		case "Heading":
			if c.page {
				level := integer(props["level"])
				if level == 0 {
					level = 2
				}
				if level == 1 {
					c.h1++
					if c.h1 > 1 {
						c.add(v, "A11Y_HEADING_MULTIPLE_H1", "props/level", ir.SeverityError, nil)
					}
				}
				if c.last > 0 && level > c.last+1 {
					c.add(v, "A11Y_HEADING_ORDER", "props/level", ir.SeverityWarning, map[string]any{"previous": c.last, "level": level})
				}
				c.last = level
			}
		case "Button":
			if !present(n, "label") {
				c.add(v, "A11Y_BUTTON_LABEL", "bindings/label", ir.SeverityError, nil)
			}
		case "Link":
			if !present(n, "to") && !click(n) {
				c.add(v, "A11Y_LINK_TARGET", "props/to", ir.SeverityError, nil)
			}
		case "Video":
			if props["autoplay"] == true && props["muted"] != true {
				c.add(v, "A11Y_VIDEO_AUTOPLAY", "props/muted", ir.SeverityError, nil)
			}
		case "Modal":
			if !present(n, "title") {
				c.add(v, "A11Y_MODAL_TITLE", "bindings/title", ir.SeverityError, nil)
			}
		}
		if interactive(n) {
			if nested {
				c.add(v, "A11Y_NESTED_INTERACTIVE", "type", ir.SeverityError, nil)
			}
			if len(v.children) > 0 && icons(v) && !present(n, "label") {
				c.add(v, "A11Y_ICON_ONLY_LABEL", "bindings/label", ir.SeverityError, nil)
			}
		}
		for _, child := range v.children {
			walk(child, nested || interactive(n))
		}
	}
	for _, name := range append([]string{"base"}, keys(obj(c.app["breakpoints"]))...) {
		bp = name
		c.h1 = 0
		c.last = 0
		for _, root := range roots {
			walk(root, false)
		}
	}
	// The same static violation visible in several breakpoints is reported once.
	seen := map[string]bool{}
	unique := c.out.Diagnostics[:0]
	for _, d := range c.out.Diagnostics {
		raw, _ := json.Marshal(d)
		key := string(raw)
		if !seen[key] {
			unique = append(unique, d)
			seen[key] = true
		}
	}
	c.out.Diagnostics = unique
	for _, root := range roots {
		c.contrasts(root)
	}
	if b.limited {
		c.out.Valid = false
		c.out.Diagnostics = append(c.out.Diagnostics, ir.Diagnostic{Code: "A11Y_EXPANSION_LIMIT", Severity: ir.SeverityError, Pointer: "", Message: "Превышен предел композиции страницы"})
	}
	slices.SortStableFunc(c.out.Diagnostics, func(a, b ir.Diagnostic) int {
		if a.Pointer != b.Pointer {
			return strings.Compare(a.Pointer, b.Pointer)
		}
		return strings.Compare(string(a.Code), string(b.Code))
	})
	return c.out
}
