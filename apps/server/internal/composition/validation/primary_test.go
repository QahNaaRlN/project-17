package validation

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

// STU-209/216/217: hidden dependencies are expressions, never literal text.
func TestPrimaryDependencyPointersSTU209216217(t *testing.T) {
	doc := map[string]any{"a/b": map[string]any{"~name": []any{
		map[string]any{"lit": "$content.literal", "default": "$content.default", "template": "$content.template", "format": "$content.format"},
		map[string]any{"args": []any{false, map[string]any{"expr": "$content.title"}}},
	}}}
	for _, test := range []struct {
		pointer string
		want    bool
	}{
		{"/a~1b/~0name/0", false}, {"/a~1b/~0name/1", true}, {"/a~1b/~0name/1/args", true},
		{"/a~1b/~0name/no", false}, {"/a~1b/~0name/-1", false}, {"/a~1b/~0name/2", false},
		{"/missing", false}, {"/a~1b/~0name/1/args/0", false},
	} {
		if got := primaryAtPointer(doc, test.pointer); got != test.want {
			t.Fatalf("%s: %v", test.pointer, got)
		}
	}
	if err := (&Context{}).RequirePrimaryEdit(context.Background(), uuid.New(), doc, nil); err == nil {
		t.Fatal("missing manifest accepted")
	}
	c := testContext()
	valid := map[string]any{"irVersion": "1.0", "kind": "page", "root": "n_root", "nodes": map[string]any{"n_root": map[string]any{"id": "n_root", "type": "Box"}}}
	if err := c.RequirePrimaryEdit(context.Background(), uuid.New(), valid, nil); err != nil {
		t.Fatal(err)
	}
}
