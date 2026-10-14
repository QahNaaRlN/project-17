package reviewdiff

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func img(t *testing.T, raw string, deleted bool) *Image {
	t.Helper()
	v, err := decode([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return &Image{Body: v, Meta: map[string]any{"deleted": deleted}, Deleted: deleted}
}
func TestExactDiffAndValues(t *testing.T) {
	a := img(t, `{"x":null,"n":1,"big":9007199254740993,"arr":[1,2],"empty":"","zero":0,"flag":false}`, false)
	b := img(t, `{"n":1.0,"big":9007199254740994,"arr":[2,1],"empty":null,"zero":null,"flag":null}`, false)
	changes, summary := Diff("entity", a, b)
	if len(changes) != 6 || summary.Effect != "modified" {
		t.Fatal(changes, summary)
	}
	raw, _ := json.Marshal(changes)
	if !strings.Contains(string(raw), `9007199254740993`) || !strings.Contains(string(raw), `rawJson`) || !strings.Contains(string(raw), `"value":null`) {
		t.Fatal(string(raw))
	}
	if canonical(a.Body["n"]) != canonical(b.Body["n"]) || hash(a.Body) == hash(b.Body) {
		t.Fatal("numeric canonicalization")
	}
	c, _ := Diff("entity", a, img(t, `{"flag":false,"zero":0,"empty":"","arr":[1,2],"big":9007199254740993,"n":1.0,"x":null}`, false))
	if len(c) != 0 {
		t.Fatal(c)
	}
	for _, v := range []any{json.Number("1e400"), json.Number("bad"), map[string]any{"n": json.Number("9007199254740993")}, []any{json.Number("9007199254740993")}} {
		if !unsafeNumber(v) {
			t.Fatal(v)
		}
	}
	if unsafeNumber(map[string]any{"arr": []any{json.Number("0.1"), true, nil}}) {
		t.Fatal("safe number")
	}
}
func TestStructureLocalesAndAssetPrivacy(t *testing.T) {
	a := img(t, `{"root":"r","nodes":{"r":{"id":"r","children":["a","b"]},"a":{"id":"a","type":"Text","props":{"text":"a"}},"b":{"id":"b","type":"Box","slots":{"footer":[]}}},"localContent":{"t_x":{"value":{"ru":"old","en":"same"}}}}`, false)
	b := img(t, `{"root":"r","nodes":{"r":{"id":"r","children":["b"]},"a":{"id":"a","type":"Text","props":{"text":"changed"},"design":{"gap":2},"bindings":{"text":"$local.x"},"when":true,"behavior":{"click":[]}},"b":{"id":"b","type":"Box","slots":{"footer":["a"]}},"new":{"id":"new","type":"Box"}},"localContent":{"t_x":{"value":{"ru":"new","en":"same"}}}}`, false)
	c, _ := Diff("document", a, b)
	seen := map[string]bool{}
	for _, i := range c {
		seen[i.Category] = true
		if i.Kind == "move" && i.NodeID == "a" {
			seen["move"] = true
		}
		if i.Locale == "ru" {
			seen["locale"] = true
		}
	}
	for _, key := range []string{"structure", "property", "design", "binding", "condition", "behavior", "localContent", "move", "locale"} {
		if !seen[key] {
			t.Fatal(key, c)
		}
	}
	reverse, _ := Diff("document", b, a)
	if len(reverse) != len(c) {
		t.Fatal(reverse)
	}
	x := img(t, `{"fileHash":"a","storageKey":"hidden-a","nested":{"credential":"hidden-b"},"array":[{"signedUrl":"hidden-c"}]}`, false)
	y := img(t, `{"fileHash":"b","storageKey":"private-a","nested":{"credential":"private-b"},"array":[{"signedUrl":"private-c"}]}`, false)
	for _, pair := range [][2]*Image{{x, y}, {nil, x}, {x, nil}} {
		c, _ = Diff("asset", pair[0], pair[1])
		raw, _ := json.Marshal(c)
		if strings.Contains(string(raw), "hidden-") || strings.Contains(string(raw), "private-") {
			t.Fatal(string(raw))
		}
	}
}
func TestLifecycleAndSignedCursors(t *testing.T) {
	live := img(t, `{"title":"a"}`, false)
	deleted := img(t, `{"title":"a"}`, true)
	for _, row := range []struct {
		a, b *Image
		want string
	}{{nil, live, "added"}, {live, nil, "deleted"}, {live, deleted, "deleted"}, {deleted, live, "restored"}, {nil, deleted, "metadata-only"}, {deleted, deleted, "unchanged"}, {nil, nil, "unchanged"}} {
		_, s := Diff("entity", row.a, row.b)
		if s.Effect != row.want {
			t.Fatal(s, row.want)
		}
	}
	token := cursor("context", 7, []byte("key"))
	if n, e := offset(token, "context", []byte("key")); n != 7 || e != nil {
		t.Fatal(n, e)
	}
	for _, row := range []struct{ raw, context string }{{"bad", "context"}, {token, "other"}, {token + "x", "context"}, {cursor("context", -1, []byte("key")), "context"}, {cursor("context", 8, []byte("other")), "context"}} {
		if _, e := offset(row.raw, row.context, []byte("key")); e == nil {
			t.Fatal(row)
		}
	}
	if n, e := offset("", "context", nil); n != 0 || e != nil {
		t.Fatal(n, e)
	}
	if _, e := decode([]byte(`[]`)); e == nil {
		t.Fatal("non object")
	}
}

func TestMetadataOnlyAndNestedServiceProjection(t *testing.T) {
	a := img(t, `{"title":"same","storage":{"key":"private-old"}}`, false)
	b := img(t, `{"title":"same","storage":{"key":"private-new"}}`, false)
	changes, _ := Diff("asset", a, b)
	raw, _ := json.Marshal(changes)
	if strings.Contains(string(raw), "private-") || !strings.Contains(string(raw), "redacted") {
		t.Fatal(string(raw))
	}
	b.Body = a.Body
	b.Meta = map[string]any{"deleted": false, "path": "/new"}
	_, summary := Diff("entity", a, b)
	if summary.Effect != "metadata-only" {
		t.Fatal(summary)
	}
}

func TestDeterministicTitlesAndCursorOverflow(t *testing.T) {
	path := "/fallback"
	for _, row := range []struct {
		body map[string]any
		want string
	}{{map[string]any{"title": map[string]any{"ru": "Russian", "en": "English"}}, "Russian"}, {map[string]any{"title": map[string]any{"ru": "", "en": "English"}}, "English"}, {map[string]any{"title": map[string]any{"fr": "French"}}, "French"}, {map[string]any{"title": map[string]any{"en": 3}, "name": "Name"}, "Name"}} {
		if got := title("entity", &Image{Body: row.body}, nil, uuid.New()); got != row.want {
			t.Fatal(got)
		}
	}
	if title("document", nil, &path, uuid.New()) != path {
		t.Fatal("fallback")
	}
	context := "context"
	key := []byte("key")
	raw := base64.RawURLEncoding.EncodeToString([]byte(context + ":9999999999999999999999999999"))
	m := hmac.New(sha256.New, key)
	m.Write([]byte(raw))
	raw += "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
	if _, err := offset(raw, context, key); err == nil {
		t.Fatal("overflow cursor accepted")
	}
}
