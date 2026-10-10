package validation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// CNT-010/013: required is a draft warning at every depth; bad types/locales remain errors.
func TestContentFieldContracts(t *testing.T) {
	c := Context{project: map[string]any{"locales": []any{"ru", "en"}}}
	schema := map[string]any{"fields": map[string]any{
		"localized": map[string]any{"type": "text", "localized": true, "required": true},
		"nested":    map[string]any{"type": "object", "fields": map[string]any{"name": map[string]any{"type": "string", "required": true}}},
		"list":      map[string]any{"type": "list", "min": json.Number("1"), "max": json.Number("2"), "of": map[string]any{"type": "number"}},
		"defaulted": map[string]any{"type": "string", "required": true, "default": "x"},
	}}
	for _, draft := range []bool{false, true} {
		r := c.Fields(schema, map[string]any{"nested": map[string]any{}, "localized": map[string]any{"ru": nil}}, draft)
		if r.Valid != draft || len(r.Diagnostics) != 3 {
			t.Fatal(r, draft)
		}
	}
	for _, data := range []map[string]any{
		{"localized": true}, {"localized": map[string]any{"xx": "x"}}, {"localized": map[string]any{"ru": 1}},
		{"nested": []any{}}, {"nested": map[string]any{"unknown": true}},
		{"list": true}, {"list": []any{}}, {"list": []any{1, 2, 3}}, {"list": []any{"bad"}},
	} {
		if r := c.Fields(schema, data, true); r.Valid {
			t.Fatal(data, r)
		}
	}
	deep := map[string]any{"type": "string"}
	var value any = "x"
	for i := 0; i < 35; i++ {
		deep = map[string]any{"type": "list", "of": deep}
		value = []any{value}
	}
	if c.Fields(map[string]any{"fields": map[string]any{"deep": deep}}, map[string]any{"deep": value}, true).Valid {
		t.Fatal("unbounded nesting")
	}
	for _, raw := range []string{`{"schemas":{"Article":{"version":1.0}}}`, `{"schemas":{"Article":{"version":0}}}`, `{"schemas":{"Article":{"version":1.2}}}`, `{"schemas":{"Article":{"version":2147483648}}}`} {
		c.app = map[string]any{}
		json.Unmarshal([]byte(raw), &c.app)
		_, _, ok := c.Schema("Article")
		if ok != strings.Contains(raw, "1.0") {
			t.Fatal(raw)
		}
	}
	if _, _, ok := c.Schema("Missing"); ok {
		t.Fatal("unknown schema")
	}
}

// L2 must reject malformed and excessive reference structures before any database reads.
func TestContentReferenceLimits(t *testing.T) {
	c := Context{}
	r, err := c.References(context.Background(), map[string]any{"entityId": "bad"})
	if err != nil || r.Valid {
		t.Fatal(r, err)
	}
	var deep any = "end"
	for i := 0; i < 130; i++ {
		deep = []any{deep}
	}
	r, err = c.References(context.Background(), deep)
	if err != nil || r.Valid {
		t.Fatal(r, err)
	}
	r, err = c.TypedReferences(context.Background(), map[string]any{"type": "reference"}, map[string]any{"entityId": "bad"}, "/ref")
	if err != nil || r.Valid {
		t.Fatal(r, err)
	}
	r, err = c.TypedReferences(context.Background(), map[string]any{"type": "string"}, nil, "/ref")
	if err != nil || !r.Valid {
		t.Fatal(r, err)
	}
}
