package delivery

import (
	"reflect"
	"testing"
)

// STU-241/250: fields used only by on/when are projected in the exact locale.
func TestBehaviorPrimaryProjectionSTU241250(t *testing.T) {
	schema := map[string]any{"fields": map[string]any{"title": map[string]any{"type": "text", "localized": true}, "visible": map[string]any{"type": "boolean"}, "private": map[string]any{"type": "text"}}}
	doc := map[string]any{"nodes": map[string]any{"n_root": map[string]any{
		"on":   map[string]any{"click": []any{map[string]any{"action": "test.use", "args": map[string]any{"title": "$content.title", "literal": map[string]any{"lit": "$content.private"}}}}},
		"when": map[string]any{"op": "eq", "args": []any{"$content.visible", map[string]any{"lit": false}}},
	}}}
	data := map[string]any{"title": map[string]any{"ru": "Название", "en": "Title"}, "visible": false, "private": "secret"}
	for _, locale := range []string{"ru", "en"} {
		got, ds, err := projectPrimary(doc, schema, data, locale)
		if err != nil || len(ds) > 0 || !reflect.DeepEqual(got, map[string]any{"title": data["title"].(map[string]any)[locale], "visible": false}) {
			t.Fatal(locale, got, ds, err)
		}
	}
}
