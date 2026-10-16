package bindingdoc

import "testing"

func TestSimpleContentTypesSTU207(t *testing.T) {
	for _, tc := range []struct {
		from, to map[string]any
		want     bool
	}{
		{object{"type": "number"}, object{"type": "text"}, false},
		{object{"type": "richText"}, object{"type": "text"}, false},
		{object{"type": "asset", "assetKind": "image"}, object{"type": "asset", "assetKind": "image"}, true},
		{object{"type": "asset", "assetKind": "file"}, object{"type": "asset", "assetKind": "image"}, false},
		{object{"type": "asset"}, object{"type": "url"}, false},
		{object{"type": "text"}, object{"type": "text", "required": true}, false},
		{object{"type": "text", "required": true}, object{"type": "text", "required": true}, true},
		{object{"type": "string", "maxLength": 10.0}, object{"type": "string", "maxLength": 5.0}, false},
		{object{"type": "text"}, object{"type": "string", "minLength": 1.0}, false},
		{object{"type": "string", "minLength": 2.0, "maxLength": 3.0, "pattern": "a"}, object{"type": "string", "minLength": 1.0, "maxLength": 5.0, "pattern": "a"}, true},
		{object{"type": "string"}, object{"type": "string", "pattern": "a"}, false},
		{object{"type": "number", "integer": true, "min": 1.0, "max": 5.0}, object{"type": "number", "integer": true, "min": 0.0, "max": 10.0}, true},
		{object{"type": "enum", "values": []any{"a"}}, object{"type": "enum", "values": []any{"a", "b"}}, true},
	} {
		if got := SimpleContentCompatible(tc.from, tc.to); got != tc.want {
			t.Fatalf("%v -> %v = %v", tc.from, tc.to, got)
		}
	}
}
