package ops

import (
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"testing"
)

func TestDataSourceSTU220221(t *testing.T) {
	use := `{"source":"commerce.products.list","params":{"limit":{"lit":4}}}`
	after, _ := roundTrip(t, base, op(DocumentSetDataSource, `{"key":"featured","use":`+use+`}`), nil)
	roundTrip(t, toJSON(t, after), op(DocumentSetDataSource, `{"key":"featured","use":null}`), nil)
	roundTrip(t, toJSON(t, after), op(DocumentSetDataSource, `{"key":"featured","use":{"source":"commerce.other","params":{}}}`), nil)
	if right, ok := Right(DocumentSetDataSource); !ok || right != auth.DesignCompose {
		t.Fatal(right, ok)
	}
	for _, raw := range []string{`{}`, `{"key":"featured"}`, `{"key":"content","use":null}`, `{"key":"constructor","use":null}`, `{"key":"prototype","use":null}`, `{"key":"__proto__","use":null}`, `{"key":"bad-key","use":null}`, `{"key":"featured","use":{},"extra":true}`, `{"key":"featured","use":{}}`, `{"key":"featured","use":1}`} {
		if _, err := Apply(parse(t, base), op(DocumentSetDataSource, raw), nil); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	component := parse(t, base)
	component["kind"] = "component"
	if _, err := Apply(component, op(DocumentSetDataSource, `{"key":"featured","use":null}`), nil); err == nil {
		t.Fatal("component accepted")
	}
	doc := parse(t, base)
	uses := map[string]any{}
	for i := 0; i < 64; i++ {
		uses["source"+string(rune('A'+i))] = map[string]any{"source": "commerce.products.list"}
	}
	doc["dataSources"] = uses
	if _, err := Apply(doc, op(DocumentSetDataSource, `{"key":"extra","use":`+use+`}`), nil); err == nil {
		t.Fatal("source limit ignored")
	}
}
