package manifestdoc

import (
	"encoding/json"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
)

var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ValidateValue checks a literal against a manifest field contract in its document.
func ValidateValue(document, app, schema, value any) bool {
	c := checker{doc: asObject(document), app: asObject(app)}
	return c.value(asObject(schema), value, 0)
}

func numeric(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		n, err := x.Float64()
		return n, err == nil
	case float64:
		return x, !math.IsNaN(x) && !math.IsInf(x, 0)
	default:
		return 0, false
	}
}
func rangeFits(v float64, min, max any) bool {
	lo, l := numeric(min)
	hi, h := numeric(max)
	return (!l || v >= lo) && (!h || v <= hi)
}

func (c *checker) value(t object, v any, depth int) bool {
	if depth > 32 {
		return false
	}
	if v == nil {
		return t["required"] != true
	}
	s, isString := v.(string)
	if isString && (!utf8.ValidString(s) || strings.ContainsRune(s, 0)) {
		return false
	}
	switch t["type"] {
	case "string", "text":
		if !isString || !rangeFits(float64(utf8.RuneCountInString(s)), t["minLength"], t["maxLength"]) {
			return false
		}
		if pattern, ok := t["pattern"].(string); ok {
			re, err := manifest.CompilePattern(pattern)
			return err == nil && re.MatchString(s)
		}
		return true
	case "number":
		n, ok := numeric(v)
		return ok && rangeFits(n, t["min"], t["max"]) && (t["integer"] != true || n == math.Trunc(n))
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "enum":
		values := array(t["values"])
		if source, ok := t["enumSource"].(string); ok {
			if source == "icons" {
				return isString && slices.Contains(array(c.app["icons"]), v)
			}
			registry := asObject(asObject(c.app["tokens"])[source])
			return isString && registry[s] != nil
		}
		return isString && slices.Contains(values, v)
	case "date":
		if !isString {
			return false
		}
		date, err := time.Parse("2006-01-02", s)
		return err == nil && date.Format("2006-01-02") == s
	case "datetime":
		if !isString {
			return false
		}
		_, err := time.Parse(time.RFC3339, s)
		return err == nil
	case "url":
		if !isString {
			return false
		}
		u, err := url.Parse(s)
		return err == nil && u.IsAbs() && (t["schemes"] == nil || slices.Contains(array(t["schemes"]), any(u.Scheme)))
	case "color":
		return isString && asObject(asObject(c.app["tokens"])["colors"])[s] != nil
	case "nodeRef":
		node := asObject(asObject(c.doc["nodes"])[s])
		return isString && node != nil && (t["nodeType"] == nil || t["nodeType"] == node["type"])
	case "asset", "reference":
		key := "assetId"
		if t["type"] == "reference" {
			key = "entityId"
		}
		obj := asObject(v)
		id, _ := obj[key].(string)
		return uuid.MatchString(id)
	case "link":
		return c.link(v)
	case "list":
		items, ok := v.([]any)
		if !ok || !rangeFits(float64(len(items)), t["min"], t["max"]) {
			return false
		}
		for _, item := range items {
			if !c.value(asObject(t["of"]), item, depth+1) {
				return false
			}
		}
		return true
	case "object":
		obj := asObject(v)
		if obj == nil {
			return false
		}
		fields := asObject(t["fields"])
		for _, key := range keys(obj) {
			if fields[key] == nil || !c.value(asObject(fields[key]), obj[key], depth+1) {
				return false
			}
		}
		for _, key := range keys(fields) {
			field := asObject(fields[key])
			if field["required"] == true && field["default"] == nil && obj[key] == nil {
				return false
			}
		}
		return true
	case "richText":
		obj := asObject(v)
		return obj["type"] == "doc" && c.richText(obj, t, depth)
	default:
		return false
	}
}

func (c *checker) link(v any) bool {
	obj := asObject(v)
	switch obj["kind"] {
	case "url":
		s, ok := obj["url"].(string)
		if !ok {
			return false
		}
		u, err := url.Parse(s)
		return err == nil && ((strings.HasPrefix(s, "/") && u.Scheme == "") || (u.IsAbs() && slices.Contains([]string{"http", "https", "mailto", "tel"}, u.Scheme)))
	case "page":
		id, _ := obj["page"].(string)
		return uuid.MatchString(id)
	case "anchor":
		id, _ := obj["node"].(string)
		return asObject(c.doc["nodes"])[id] != nil
	default:
		return false
	}
}
