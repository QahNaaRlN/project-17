// Package bindingdoc implements static binding and action validation (IR-042/052).
// The caller supplies the exact environment's manifest and component/page resolution.
package bindingdoc

import (
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifestdoc"
)

type object = map[string]any
type scopes map[string]object
type Options struct {
	Path             *string
	ResolveComponent manifestdoc.ResolveComponent
	// ResolvePage returns the prospective route of a page in this environment.
	ResolvePage func(string) (*string, bool)
}
type checker struct {
	doc, app object
	options  Options
	out      []ir.Diagnostic
}

func obj(v any) object { m, _ := v.(map[string]any); return m }
func arr(v any) []any  { a, _ := v.([]any); return a }
func keys(m object) []string {
	a := make([]string, 0, len(m))
	for k := range m {
		a = append(a, k)
	}
	sort.Strings(a)
	return a
}
func typ(t string) object    { return object{"type": t, "required": true} }
func fields(f object) object { return object{"type": "object", "fields": f, "required": true} }
func copyMap(m object) object {
	n := object{}
	for k, v := range m {
		n[k] = v
	}
	return n
}
func clone(s scopes) scopes {
	n := scopes{}
	for k, v := range s {
		n[k] = v
	}
	return n
}
func (c *checker) add(code, p, id string, params object) {
	c.out = append(c.out, ir.Diagnostic{Code: ir.Code(code), Severity: ir.SeverityError, Pointer: p, NodeID: id, Message: code, Params: params})
}

var expression = regexp.MustCompile(`^\$(content|local|props|item|index|each|data|context)(\.(?:[A-Za-z_][A-Za-z0-9_]*|[0-9]+))*$`)

func routeFields(path *string) object {
	f := object{}
	if path != nil {
		for _, s := range strings.Split(*path, "/") {
			if strings.HasPrefix(s, ":") {
				f[strings.TrimPrefix(s, ":")] = typ("string")
			}
		}
	}
	return f
}
func (c *checker) path(expr, p, id string, s scopes) object {
	if !expression.MatchString(expr) {
		c.add("BINDING_SYNTAX", p, id, object{"path": expr})
		return nil
	}
	parts := strings.Split(expr[1:], ".")
	t := s[parts[0]]
	if t == nil {
		c.add("BINDING_SCOPE_UNAVAILABLE", p, id, object{"path": expr})
		return nil
	}
	missing := false
	refs := 0
	for _, seg := range parts[1:] {
		missing = missing || (t["required"] != true && t["default"] == nil)
		if t["type"] == "reference" {
			refs++
			if refs > 3 {
				c.add("BINDING_DEPTH_EXCEEDED", p, id, object{"path": expr})
				return nil
			}
			t = fields(obj(obj(obj(c.app["schemas"])[stringValue(t["schema"])])["fields"]))
		}
		switch t["type"] {
		case "object":
			t = obj(obj(t["fields"])[seg])
		case "list":
			if numericSegment(seg) {
				t = obj(t["of"])
				missing = true
			} else {
				t = nil
			}
		default:
			t = nil
		}
		if t == nil {
			c.add("BINDING_PATH_UNRESOLVED", p, id, object{"path": expr})
			return nil
		}
	}
	t = copyMap(t)
	if missing {
		delete(t, "required")
		delete(t, "default")
	}
	return t
}
func numericSegment(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
func stringValue(v any) string { s, _ := v.(string); return s }

func compatible(from, to object, depth int) bool {
	if from == nil || to == nil || depth > 32 {
		return false
	}
	if to["required"] == true && to["default"] == nil && from["required"] != true && from["default"] == nil {
		return false
	}
	f, t := stringValue(from["type"]), stringValue(to["type"])
	if t == "text" || t == "string" {
		return f == "text" || f == "string" || f == "number" || f == "date" || f == "datetime"
	}
	if f == "url" && t == "link" {
		return true
	}
	if f != t {
		return false
	}
	switch t {
	case "reference":
		return from["schema"] == to["schema"]
	case "asset":
		return to["assetKind"] == nil || from["assetKind"] == to["assetKind"]
	case "nodeRef":
		return to["nodeType"] == nil || from["nodeType"] == to["nodeType"]
	case "number":
		if to["integer"] == true && from["integer"] != true {
			return false
		}
		for _, bound := range []string{"min", "max"} {
			limit, constrained := number(to[bound])
			if !constrained {
				continue
			}
			value, known := number(from[bound])
			if !known || (bound == "min" && value < limit) || (bound == "max" && value > limit) {
				return false
			}
		}
	case "enum":
		if from["enumSource"] != nil && from["enumSource"] == to["enumSource"] {
			return true
		}
		for _, v := range arr(from["values"]) {
			found := false
			for _, w := range arr(to["values"]) {
				if v == w {
					found = true
				}
			}
			if !found {
				return false
			}
		}
		return len(arr(from["values"])) > 0
	case "list":
		return to["of"] == nil || compatible(obj(from["of"]), obj(to["of"]), depth+1)
	case "object":
		for k, v := range obj(to["fields"]) {
			source := obj(obj(from["fields"])[k])
			target := obj(v)
			if source == nil && (target["required"] != true || target["default"] != nil) {
				continue
			}
			if !compatible(source, target, depth+1) {
				return false
			}
		}
	}
	return true
}
func (c *checker) value(v any, target object, p, id string, s scopes, code string) object {
	if expr, ok := v.(string); ok {
		source := c.path(expr, p, id, s)
		if source != nil && !compatible(source, target, 0) {
			c.add("BINDING_TYPE_MISMATCH", p, id, object{"expected": target, "actual": source})
		}
		return source
	}
	if target["type"] == "link" {
		link := obj(v)
		if literal, ok := link["lit"]; ok {
			link = obj(literal)
		}
		if link != nil {
			before := len(c.out)
			c.link(link, p, id, s)
			if len(c.out) > before {
				return nil
			}
			return typ("link")
		}
	}
	lit, ok := obj(v)["lit"]
	if !ok || !manifestdoc.ValidateValue(c.doc, c.app, target, lit) {
		c.add(code, p, id, object{"expected": target})
		return nil
	}
	return infer(lit)
}
func infer(v any) object {
	switch x := v.(type) {
	case string:
		return typ("string")
	case float64:
		t := typ("number")
		t["min"] = x
		t["max"] = x
		t["integer"] = x == math.Trunc(x)
		return t
	case json.Number:
		n, ok := number(x)
		if !ok {
			return object{}
		}
		return infer(n)
	case bool:
		return typ("boolean")
	case []any:
		t := typ("list")
		if len(x) > 0 {
			t["of"] = infer(x[0])
		}
		return t
	case map[string]any:
		f := object{}
		for k, v := range x {
			f[k] = infer(v)
		}
		return fields(f)
	default:
		return object{}
	}
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, !math.IsNaN(x) && !math.IsInf(x, 0)
	case json.Number:
		n, err := x.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}
