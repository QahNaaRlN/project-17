package bindingdoc

import "testing"

// STU-240/251: direct action operands use the same strict contracts as runtime.
func TestDirectActionTypesSTU240(t *testing.T) {
	for _, tc := range []struct {
		source, target string
		valid          bool
	}{
		{"number", "string", false}, {"number", "text", false}, {"object", "string", false},
		{"url", "link", false}, {"text", "string", true}, {"boolean", "boolean", true},
		{"number", "number", true},
	} {
		t.Run(tc.source+"To"+tc.target, func(t *testing.T) {
			a := app(t)
			a["actions"] = object{"test.use": object{"args": object{"value": object{"type": tc.target, "required": true}}}}
			d := document(object{"type": "Button", "on": object{"click": object{"action": "test.use", "args": object{"value": "$local.value"}}}})
			obj(d["localContent"])["value"] = object{"type": tc.source, "value": object{"ru": "unused"}}
			result := Validate(d, a, Options{})
			if result.Valid != tc.valid {
				t.Fatalf("valid=%v diagnostics=%+v", tc.valid, result.Diagnostics)
			}
		})
	}
}
