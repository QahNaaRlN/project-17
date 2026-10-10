// Package manifestdoc checks document properties against a validated application manifest
// (L3, 02 §11.1). It does not resolve data bindings, publish content or activate manifests.
package manifestdoc

import (
	"fmt"
	"slices"
	"sort"
	"unicode/utf16"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
)

type object = map[string]any

// ResolveComponent supplies the inputs/slotDefs of the exact component version selected
// by the caller's L2 environment resolution. Missing references fail closed.
type ResolveComponent func(ref map[string]any) (map[string]any, bool)

// Validate performs L1 before L3 and requires a valid manifest. A nil manifest means
// the environment is not ready (MF-003). The caller supplies environment-aware L2 resolution
// for Composed instances; L4-L7 and transitive component recursion remain separate stages.
func Validate(document, app any, resolve ResolveComponent) ir.Result {
	if app == nil {
		return ir.Result{Diagnostics: []ir.Diagnostic{{Code: "MANIFEST_NOT_READY", Severity: ir.SeverityError, Pointer: "", Message: "окружение не имеет активного manifest"}}}
	}
	if m := manifest.Validate(app); !m.Valid {
		ds := make([]ir.Diagnostic, len(m.Diagnostics))
		for i, d := range m.Diagnostics {
			ds[i] = ir.Diagnostic{Code: ir.Code(d.Code), Severity: ir.Severity(d.Severity), Pointer: d.Pointer, Message: d.Message, Params: d.Params}
		}
		return ir.Result{Diagnostics: ds}
	}
	if result := ir.ValidateDocument(document); !result.Valid {
		return result
	}
	c := checker{doc: asObject(document), app: asObject(app), resolve: resolve, out: []ir.Diagnostic{}}
	nodes := asObject(c.doc["nodes"])
	for _, id := range keys(nodes) {
		c.node(id, asObject(nodes[id]))
	}
	sort.SliceStable(c.out, func(i, j int) bool {
		if c.out[i].Pointer == c.out[j].Pointer {
			return c.out[i].Code < c.out[j].Code
		}
		return compareUTF16(c.out[i].Pointer, c.out[j].Pointer) < 0
	})
	valid := true
	for _, d := range c.out {
		if d.Severity == ir.SeverityError {
			valid = false
		}
	}
	return ir.Result{Valid: valid, Diagnostics: c.out}
}

type checker struct {
	doc, app object
	resolve  ResolveComponent
	out      []ir.Diagnostic
}

func (c *checker) add(code string, ptr, id string, params object) {
	c.out = append(c.out, ir.Diagnostic{Code: ir.Code(code), Severity: ir.SeverityError, Pointer: ptr, NodeID: id, Message: fmt.Sprintf("нарушение контракта manifest: %s", code), Params: params})
}

func (c *checker) node(id string, n object) {
	ptr := ir.Pointer("nodes", id)
	name, _ := n["type"].(string)
	definition := manifest.BuiltinContract(name)
	if definition == nil {
		definition = asObject(asObject(c.app["primitives"])[name])
	}
	if definition == nil {
		definition = asObject(asObject(c.app["components"])[name])
	}
	if definition == nil {
		c.add("TYPE_UNKNOWN", ptr+"/type", id, object{"type": name})
		return
	}
	if name == "Composed" {
		var component object
		var ok bool
		if c.resolve != nil {
			component, ok = c.resolve(asObject(n["ref"]))
		}
		if !ok || component["kind"] != "component" {
			c.add("COMPONENT_NOT_FOUND", ptr+"/ref", id, nil)
			return
		}
		definition = object{"props": component["inputs"], "slots": component["slotDefs"]}
	}
	props := asObject(definition["props"])
	bindings := asObject(n["bindings"])
	static := asObject(n["props"])
	for _, field := range keys(static) {
		t, known := props[field]
		p := ptr + ir.Pointer("props", field)
		if !known {
			c.add("PROP_UNKNOWN", p, id, object{"property": field})
			continue
		}
		schema := asObject(t)
		if schema["content"] == true {
			c.add("PROP_CONTENT_LITERAL", p, id, object{"property": field})
			continue
		}
		c.property(schema, static[field], p, id)
	}
	for _, field := range keys(bindings) {
		if _, ok := props[field]; !ok {
			c.add("PROP_UNKNOWN", ptr+ir.Pointer("bindings", field), id, object{"property": field})
		}
	}
	for _, field := range keys(props) {
		t := asObject(props[field])
		_, hasDefault := t["default"]
		_, bound := bindings[field]
		if t["required"] == true && !hasDefault && !bound && static[field] == nil {
			c.add("REQUIRED_INPUT_MISSING", ptr+ir.Pointer("props", field), id, object{"property": field})
		}
	}
	if definition["container"] != true && len(array(n["children"])) > 0 {
		c.add("CHILDREN_NOT_ALLOWED", ptr+"/children", id, nil)
	}
	slots := asObject(n["slots"])
	defs := asObject(definition["slots"])
	for _, slot := range keys(slots) {
		if _, ok := defs[slot]; !ok {
			c.add("SLOT_UNKNOWN", ptr+ir.Pointer("slots", slot), id, object{"slot": slot})
		}
	}
	nodes := asObject(c.doc["nodes"])
	for _, slot := range keys(defs) {
		s := asObject(defs[slot])
		children := array(slots[slot])
		p := ptr + ir.Pointer("slots", slot)
		if !rangeFits(float64(len(children)), s["min"], s["max"]) {
			c.add("SLOT_COUNT_OUT_OF_RANGE", p, id, object{"slot": slot, "count": len(children), "min": s["min"], "max": s["max"]})
		}
		if allowed, ok := s["allowedTypes"]; ok {
			for i, child := range children {
				childID, _ := child.(string)
				childType := asObject(nodes[childID])["type"]
				if !slices.Contains(array(allowed), childType) {
					c.add("SLOT_TYPE_DENIED", p+ir.Pointer(i), id, object{"slot": slot, "type": childType})
				}
			}
		}
	}
	events := asObject(definition["events"])
	for _, event := range keys(asObject(n["on"])) {
		if _, ok := events[event]; !ok {
			c.add("EVENT_UNKNOWN", ptr+ir.Pointer("on", event), id, object{"event": event})
		}
	}
	if deprecated := asObject(definition["deprecated"]); deprecated != nil {
		c.out = append(c.out, ir.Diagnostic{Code: "TYPE_DEPRECATED", Severity: ir.SeverityWarning, Pointer: ptr + "/type", NodeID: id, Message: "тип помечен устаревшим", Params: object{"type": name}})
	}
	if name == "Slot" {
		slot, _ := static["name"].(string)
		_, bound := bindings["name"]
		if c.doc["kind"] != "component" || (!bound && asObject(c.doc["slotDefs"])[slot] == nil) {
			c.add("SLOT_UNKNOWN", ptr+"/props/name", id, object{"slot": slot})
		}
	}
}

func (c *checker) property(t object, value any, ptr, id string) {
	// IR-031/032: every object for a responsive property is a breakpoint wrapper.
	// Structured values are supplied inside base; non-responsive structured props are literal data.
	kind := t["type"]
	structured := kind == "object" || kind == "link" || kind == "asset" || kind == "reference" || kind == "richText"
	if responsive := asObject(value); responsive != nil && (t["responsive"] == true || !structured) {
		if t["responsive"] != true {
			c.add("PROP_TYPE_MISMATCH", ptr, id, object{"reason": "responsive_not_allowed"})
			return
		}
		if _, ok := responsive["base"]; !ok {
			c.add("PROP_TYPE_MISMATCH", ptr, id, object{"reason": "responsive_base_missing"})
		}
		for _, bp := range keys(responsive) {
			if bp != "base" && asObject(c.app["breakpoints"])[bp] == nil {
				c.add("BREAKPOINT_UNKNOWN", ptr+ir.Pointer(bp), id, object{"breakpoint": bp})
				continue
			}
			if !c.value(t, responsive[bp], 0) {
				c.add("PROP_TYPE_MISMATCH", ptr+ir.Pointer(bp), id, object{"type": kind})
			}
		}
		return
	}
	if !c.value(t, value, 0) {
		c.add("PROP_TYPE_MISMATCH", ptr, id, object{"type": kind})
	}
}

func asObject(v any) object { m, _ := v.(map[string]any); return m }
func array(v any) []any     { a, _ := v.([]any); return a }
func keys(m object) []string {
	k := make([]string, 0, len(m))
	for key := range m {
		k = append(k, key)
	}
	slices.SortFunc(k, compareUTF16)
	return k
}

func compareUTF16(a, b string) int {
	return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
}
