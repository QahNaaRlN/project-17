package ops

import (
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"pgregory.net/rapid"
	"reflect"
	"strings"
	"testing"
)

func TestPrimaryInversePropertySTU204217(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		original := parse(t, base)
		if rapid.Bool().Draw(rt, "existing") {
			original["content"] = map[string]any{"schema": "Article", "resolve": map[string]any{"by": "routeParam", "param": "slug", "field": "slug"}}
		}
		next := Clone(original)
		var value any
		if rapid.Bool().Draw(rt, "entity") {
			value = map[string]any{"schema": rapid.StringMatching(`^[A-Z][a-z]{1,15}$`).Draw(rt, "schema"), "resolve": map[string]any{"by": "entity", "entityId": "00000000-0000-4000-8000-000000000001"}}
		}
		result, err := Apply(next, Op{Type: DocumentSetContent, Payload: mustRaw(map[string]any{"content": value})}, nil)
		if err != nil {
			rt.Fatal(err)
		}
		if _, err := Apply(next, result.Inverse, nil); err != nil {
			rt.Fatal(err)
		}
		if !reflect.DeepEqual(original, next) {
			rt.Fatal("inverse lost optional source or route declaration")
		}
	})
}

func TestPrimaryContentSTU204217(t *testing.T) {
	entity := `{"schema":"Article","resolve":{"by":"entity","entityId":"00000000-0000-4000-8000-000000000001"}}`
	after, _ := roundTrip(t, base, op(DocumentSetContent, `{"content":`+entity+`}`), nil)
	removed, _ := roundTrip(t, toJSON(t, after), op(DocumentSetContent, `{"content":null}`), nil)
	if _, exists := removed["content"]; exists {
		t.Fatal("null must remove the optional field")
	}
	route := `{"schema":"Article","resolve":{"by":"routeParam","param":"slug","field":"slug"}}`
	// Use an existing valid routeParam declaration for exact inverse preservation.
	original := parse(t, base)
	original["content"] = fromJSON([]byte(route))
	roundTrip(t, toJSON(t, original), op(DocumentSetContent, `{"content":`+entity+`}`), nil)
	if right, ok := Right(DocumentSetContent); !ok || right != auth.DesignCompose {
		t.Fatal("wrong capability")
	}
}

func TestPrimaryContentInvalidSTU204(t *testing.T) {
	for _, raw := range []string{`{`, `{}`, `{"content":null,"extra":1}`, `{"content":{}}`, `{"content":null} {}`} {
		if _, err := Apply(parse(t, base), op(DocumentSetContent, raw), nil); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	component := strings.Replace(base, `"kind":"page"`, `"kind":"component"`, 1)
	if _, err := Apply(parse(t, component), op(DocumentSetContent, `{"content":null}`), nil); err == nil {
		t.Fatal("component accepted")
	}
}
