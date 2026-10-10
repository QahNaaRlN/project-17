package manifestdoc

import (
	"encoding/json"
	"math"
	"testing"
)

// 04 §4: values, modifiers, nested lists/objects and references.
func TestValueContract(t *testing.T) {
	doc, app := fixture(t)
	asObject(app["tokens"])["container"] = object{"prose": "65ch"}
	c := checker{doc: doc, app: app}
	id := "0192f1c4-7a1e-7c2b-9d10-3b5f2a9e4c11"
	cases := []struct {
		schema         object
		valid, invalid []any
	}{
		{object{"type": "string", "minLength": float64(1), "maxLength": float64(2)}, []any{"😀", "яя"}, []any{"", "abc", 42, "\x00", string([]byte{255})}},
		{object{"type": "string", "pattern": "^a+$"}, []any{"aaa"}, []any{"b"}},
		{object{"type": "string", "pattern": "(?=a)"}, nil, []any{"a"}},
		{object{"type": "number", "integer": true, "min": float64(1), "max": float64(3)}, []any{float64(1), json.Number("2")}, []any{float64(0), float64(4), float64(1.5), json.Number("x"), math.NaN(), math.Inf(1), "2"}},
		{object{"type": "boolean"}, []any{true, false}, []any{"true"}},
		{object{"type": "enum", "values": []any{"x", "y"}}, []any{"x"}, []any{"z", false}},
		{object{"type": "enum", "enumSource": "container"}, []any{"prose"}, []any{"unknown", true}},
		{object{"type": "date"}, []any{"2024-02-29"}, []any{"2025-02-29", "2024-2-1", true}},
		{object{"type": "datetime"}, []any{"2026-10-10T12:30:00+05:00"}, []any{"2026-10-10", true}},
		{object{"type": "url", "schemes": []any{"https"}}, []any{"https://example.com"}, []any{"http://example.com", "relative", true, "https://["}},
		{object{"type": "color"}, []any{"primary"}, []any{"unknown", false}},
		{object{"type": "nodeRef", "nodeType": "Text"}, []any{"n_text"}, []any{"n_root", "n_unknown", true}},
		{object{"type": "asset"}, []any{object{"assetId": id}}, []any{object{"assetId": "bad"}, id}},
		{object{"type": "reference"}, []any{object{"entityId": id}}, []any{object{"entityId": "bad"}}},
		{object{"type": "link"}, []any{object{"kind": "page", "page": id}, object{"kind": "anchor", "node": "n_text"}, object{"kind": "url", "url": "https://example.com"}}, []any{object{"kind": "page", "page": "bad"}, object{"kind": "anchor", "node": "missing"}, object{"kind": "url", "url": "relative"}, object{"kind": "other"}, true}},
		{object{"type": "list", "min": float64(1), "max": float64(2), "of": object{"type": "boolean"}}, []any{[]any{true, false}}, []any{[]any{}, []any{true, false, true}, []any{"true"}, false}},
		{object{"type": "object", "fields": object{"title": object{"type": "string", "required": true}, "flag": object{"type": "boolean", "required": true, "default": false}}}, []any{object{"title": "ok"}}, []any{object{}, object{"title": "ok", "unknown": true}, object{"title": true}, true}},
		{object{"type": "unknown"}, nil, []any{true}},
	}
	for _, test := range cases {
		for _, v := range test.valid {
			if !c.value(test.schema, v, 0) {
				t.Errorf("%v rejects %v", test.schema, v)
			}
		}
		for _, v := range test.invalid {
			if c.value(test.schema, v, 0) {
				t.Errorf("%v accepts %v", test.schema, v)
			}
		}
	}
	if !c.value(object{"type": "string"}, nil, 0) || c.value(object{"type": "string", "required": true}, nil, 0) || c.value(object{"type": "string"}, "x", 33) {
		t.Fatal("null/depth")
	}
}

// CNT RichText v1: nested blocks, inline content, marks and optional subsets.
func TestRichTextContract(t *testing.T) {
	d, m := fixture(t)
	c := checker{doc: d, app: m}
	schema := object{"type": "richText"}
	text := object{"type": "text", "text": "привет😀", "marks": []any{object{"type": "bold"}, object{"type": "link", "attrs": object{"to": object{"kind": "anchor", "node": "n_text"}}}}}
	p := object{"type": "paragraph", "content": []any{text, object{"type": "hardBreak"}}}
	valid := object{"type": "doc", "content": []any{p, object{"type": "heading", "attrs": object{"level": float64(2)}, "content": []any{text}}, object{"type": "bulletList", "content": []any{object{"type": "listItem", "content": []any{p}}}}, object{"type": "blockquote", "content": []any{p}}, object{"type": "image", "attrs": object{"assetId": "0192f1c4-7a1e-7c2b-9d10-3b5f2a9e4c11", "alt": "alt"}}, object{"type": "divider"}}}
	if !c.value(schema, valid, 0) {
		t.Fatal("valid RichText")
	}
	invalid := []object{
		{"type": "doc"}, {"type": "doc", "content": true}, {"type": "doc", "content": []any{object{"type": "unknown"}}},
		{"type": "doc", "content": []any{text}},
		{"type": "heading", "attrs": object{"level": float64(1)}, "content": []any{}},
		{"type": "heading", "attrs": object{"level": "2"}, "content": []any{}},
		{"type": "image", "attrs": object{"assetId": "bad"}},
		{"type": "image", "attrs": object{"assetId": "0192f1c4-7a1e-7c2b-9d10-3b5f2a9e4c11", "alt": true}},
		{"type": "text", "text": false}, {"type": "text", "text": "x", "marks": true},
		{"type": "text", "text": "x", "marks": []any{object{"type": "unknown"}}},
		{"type": "text", "text": "x", "marks": []any{object{"type": "link", "attrs": object{"to": false}}}},
		{"type": "paragraph", "content": []any{p}},
		{"type": "bulletList", "content": []any{p}},
		{"type": "divider", "content": []any{p}},
	}
	for _, n := range invalid {
		if c.richText(n, schema, 0) {
			t.Errorf("accepted %v", n)
		}
	}
	if c.value(schema, object{"type": "paragraph", "content": []any{}}, 0) || c.richText(valid, schema, 33) {
		t.Fatal("root/depth")
	}
	if c.value(object{"type": "richText", "blocks": []any{"heading"}}, valid, 0) || c.value(object{"type": "richText", "marks": []any{"italic"}}, valid, 0) {
		t.Fatal("subset")
	}
	if !c.richText(object{"type": "hardBreak", "content": []any{}}, schema, 0) {
		t.Fatal("empty leaf")
	}
}
