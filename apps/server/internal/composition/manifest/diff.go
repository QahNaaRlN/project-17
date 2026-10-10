package manifest

import (
	"encoding/json"
	"reflect"
	"slices"
)

// ChangeClass describes compatibility, independently of whether a document uses the change.
type ChangeClass string

const (
	Compatible ChangeClass = "compatible"
	Breaking   ChangeClass = "breaking"
	Visual     ChangeClass = "visual"
)

// Change is a deterministic, leaf-level manifest diff (MF-020). Schema marks any
// change that requires the standard-environment candidate workflow (MF-025).
// Before and After are present even for JSON null; Added/Removed distinguish absence.
type Change struct {
	Pointer string      `json:"pointer"`
	Class   ChangeClass `json:"class"`
	Schema  bool        `json:"schema"`
	Added   bool        `json:"added,omitempty"`
	Removed bool        `json:"removed,omitempty"`
	Before  any         `json:"before"`
	After   any         `json:"after"`
}

// Diff compares validated manifests. It does not mutate either input. Optional
// absent sections and empty sections are equivalent, as in the manifest schema.
// Document impact and activation are separate decisions: a breaking diff alone
// is not a reason to reject registration (MF-022, MF-023).
func Diff(before, after any) []Change {
	a, b := object(before), object(after)
	out := []Change{}
	keys := union(a, b)
	for _, key := range keys {
		av, aok := a[key]
		bv, bok := b[key]
		if slices.Contains([]string{"primitives", "components", "actions", "dataSources", "formatters", "schemas", "capabilities", "breakpoints", "tokens"}, key) {
			if !aok {
				av = obj{}
				aok = true
			}
			if !bok {
				bv = obj{}
				bok = true
			}
		}
		if key == "icons" {
			if !aok {
				av = []any{}
				aok = true
			}
			if !bok {
				bv = []any{}
				bok = true
			}
		}
		diffValue([]string{key}, av, bv, aok, bok, b, &out)
	}
	slices.SortFunc(out, func(a, b Change) int { return compareUTF16(a.Pointer, b.Pointer) })
	return out
}

func union(a, b obj) []string {
	keys := map[string]any{}
	for k := range a {
		keys[k] = nil
	}
	for k := range b {
		keys[k] = nil
	}
	return sortedKeys(keys)
}

func diffValue(path []string, a, b any, aok, bok bool, parentB obj, out *[]Change) {
	if slices.Contains([]string{"fields", "slots", "events"}, role(path)) {
		if !aok {
			a = obj{}
			aok = true
		}
		if !bok {
			b = obj{}
			bok = true
		}
	}
	if aok == bok && equalJSON(a, b) {
		return
	}
	// An entire added/removed registry entry is one change, rather than a list
	// of misleading required-property changes within a newly added component.
	if aok && bok {
		am, ais := a.(map[string]any)
		bm, bis := b.(map[string]any)
		if ais && bis && !atomicPath(path) {
			for _, k := range union(am, bm) {
				av, ae := am[k]
				bv, be := bm[k]
				diffValue(append(slices.Clone(path), k), av, bv, ae, be, bm, out)
			}
			return
		}
	}
	c := classify(path, a, b, aok, bok, parentB)
	segments := make([]any, len(path))
	for i, segment := range path {
		segments[i] = segment
	}
	*out = append(*out, Change{Pointer: Pointer(segments...), Class: c, Schema: path[0] == "schemas",
		Added: !aok, Removed: !bok, Before: a, After: b})
}

// Typed defaults, source references, migrations and app metadata are indivisible:
// their internal keys do not have the semantics of a registry or a type range.
func atomicPath(path []string) bool {
	return role(path) == "metadata" || role(path) == "literal"
}

func equalJSON(a, b any) bool {
	// JSON numbers can arrive as json.Number or float64. Comparing canonical
	// bytes also ignores object key order and numeric lexical representations.
	ac, ae := CanonicalJSON(a)
	bc, be := CanonicalJSON(b)
	return ae == nil && be == nil && string(ac) == string(bc)
}

func classify(path []string, a, b any, aok, bok bool, parentB obj) ChangeClass {
	section, field := path[0], path[len(path)-1]
	if role(path) == "literal" && !bok {
		return Breaking
	}
	if atomicPath(path) || section == "manifestVersion" {
		return Compatible
	}
	if section == "tokens" {
		if !bok {
			return Breaking
		}
		if !aok {
			return Compatible
		}
		return Visual
	}
	if section == "breakpoints" {
		if !bok {
			return Breaking
		}
		if aok {
			return Visual
		}
		return Compatible
	}
	if section == "icons" || section == "irVersions" {
		return setClass(a, b)
	}
	if field == "input" && role(path[:len(path)-1]) == "formatter" {
		return setClass(a, b)
	}
	if len(path) == 2 {
		if !bok {
			return Breaking
		}
		return Compatible
	}
	if !aok {
		if requiredAddition(path, b, parentB) {
			return Breaking
		}
		return Compatible
	}
	if !bok {
		if (field == "container" && role(path[:len(path)-1]) == "component") || (field == "responsive" && constraintPath(path)) {
			if a == false {
				return Compatible
			}
			return Breaking
		}
		// Removing a constraint widens the accepted value set. Removing a
		// property/slot/event/result field removes part of the contract instead.
		if constraintPath(path) && slices.Contains([]string{"min", "max", "minLength", "maxLength", "pattern", "integer", "required", "unique", "mimeTypes", "marks", "blocks", "schemes", "allowedTypes", "nodeType", "assetKind"}, field) {
			return Compatible
		}
		return Breaking
	}
	if constraintPath(path) {
		switch field {
		case "min", "minLength":
			return rangeClass(a, b, true)
		case "max", "maxLength":
			return rangeClass(a, b, false)
		case "required", "integer", "unique":
			if b == true {
				return Breaking
			}
			return Compatible
		case "responsive":
			if b == false {
				return Breaking
			}
			return Compatible
		case "values", "allowedTypes", "mimeTypes", "marks", "blocks", "schemes":
			return setClass(a, b)
		}
	}
	if field == "container" && role(path[:len(path)-1]) == "component" {
		if b == false {
			return Breaking
		}
		return Compatible
	}
	// Types, reference schemas, patterns and other unclassified executable
	// contracts are conservative: changing them requires an impact check.
	return Breaking
}

// role follows the schema rather than guessing from the last segment: e.g.
// props.description is a property, not component metadata; fields.min is a
// field, not a minimum constraint.
func role(path []string) string {
	current := "root"
	for _, key := range path {
		switch current {
		case "root":
			switch key {
			case "components", "primitives":
				current = "components"
			case "actions":
				current = "actions"
			case "dataSources":
				current = "sources"
			case "formatters":
				current = "formatters"
			case "schemas":
				current = "schemas"
			case "capabilities":
				current = "capabilities"
			case "app", "codeIndex":
				current = "metadata"
			default:
				current = "other"
			}
		case "components":
			current = "component"
		case "actions":
			current = "action"
		case "sources":
			current = "source"
		case "formatters":
			current = "formatter"
		case "schemas":
			current = "schema"
		case "capabilities":
			current = "capability"
		case "capability":
			current = "metadata"
		case "component", "action", "source", "schema", "formatter":
			switch key {
			case "props", "args", "params", "fields", "provides":
				current = "fields"
			case "slots":
				current = "slots"
			case "result":
				current = "type"
			case "events":
				current = "events"
			case "description", "category", "group", "since", "origin", "version", "cache", "paginated", "indexes", "sourceRef", "deprecated", "migrateFrom", "display":
				current = "metadata"
			default:
				current = "other"
			}
		case "fields":
			current = "type"
		case "slots":
			current = "slot"
		case "type":
			switch key {
			case "fields":
				current = "fields"
			case "of":
				current = "type"
			case "description", "deprecated":
				current = "metadata"
			case "default":
				current = "literal"
			default:
				current = "other"
			}
		case "slot":
			if key == "description" {
				current = "metadata"
			} else {
				current = "other"
			}
		case "events":
			current = "event"
		case "event":
			if key == "payload" {
				current = "fields"
			} else if key == "description" {
				current = "metadata"
			} else {
				current = "other"
			}
		default: // metadata and literal descendants retain their role
		}
	}
	return current
}

func constraintPath(path []string) bool {
	parent := role(path[:len(path)-1])
	return parent == "type" || parent == "slot"
}

func requiredAddition(path []string, b any, parent obj) bool {
	field := path[len(path)-1]
	if field == "required" && constraintPath(path) {
		return b == true
	}
	if constraintPath(path) {
		switch field {
		case "integer", "unique":
			return b == true
		case "min":
			if role(path[:len(path)-1]) == "slot" || parent["type"] == "list" {
				n, ok := jsonNumber(b)
				return !ok || n > 0
			}
			return true
		case "minLength":
			n, ok := jsonNumber(b)
			return !ok || n > 0
		case "max", "maxLength", "pattern", "mimeTypes", "marks", "blocks", "schemes", "allowedTypes", "nodeType", "assetKind":
			return true
		}
	}
	def := object(b)
	if role(path) == "slot" {
		n, ok := jsonNumber(def["min"])
		return ok && n > 0
	}
	// Only input fields must be supplied by the caller. Adding a required
	// output field to a data source does not invalidate existing bindings.
	output := len(path) >= 3 && ((path[0] == "dataSources" && path[2] == "result") || (slices.Contains([]string{"components", "primitives"}, path[0]) && path[2] == "provides"))
	output = output || (len(path) >= 5 && slices.Contains([]string{"components", "primitives"}, path[0]) && path[2] == "events" && path[4] == "payload")
	if role(path) == "type" && role(path[:len(path)-1]) == "fields" && !output {
		return def["required"] == true
	}
	return false
}

func jsonNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case float64:
		return n, true
	}
	return 0, false
}

func rangeClass(a, b any, minimum bool) ChangeClass {
	av, ao := jsonNumber(a)
	bv, bo := jsonNumber(b)
	if !ao || !bo || (minimum && bv > av) || (!minimum && bv < av) {
		return Breaking
	}
	return Compatible
}

func setClass(a, b any) ChangeClass {
	if a == nil {
		a = []any{}
	}
	old, ao := a.([]any)
	next, bo := b.([]any)
	if !ao || !bo {
		return Breaking
	}
	for _, value := range old {
		if !slices.ContainsFunc(next, func(v any) bool { return reflect.DeepEqual(value, v) }) {
			return Breaking
		}
	}
	return Compatible
}

// SchemasChanged is independent of compatibility: even an optional added
// schema field needs agreement in standard environments (MF-021, MF-025).
func SchemasChanged(diff []Change) bool {
	return slices.ContainsFunc(diff, func(c Change) bool { return c.Schema })
}

// BreakingChanges extracts changes requiring document impact analysis.
func BreakingChanges(diff []Change) []Change {
	out := []Change{}
	for _, c := range diff {
		if c.Class == Breaking {
			out = append(out, c)
		}
	}
	return out
}
