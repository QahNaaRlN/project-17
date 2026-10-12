package ops

import (
	"reflect"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"pgregory.net/rapid"
)

func TestLocalContentRoundTripProperty(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		key := rapid.StringMatching(`^t_[a-z0-9_]{2,40}$`).Draw(rt, "key")
		text := rapid.String().Draw(rt, "text")
		original := parse(t, base)
		document := Clone(original)
		result, err := Apply(document, Op{Type: LocalContentSet, Payload: mustRaw(map[string]any{"key": key, "type": "text", "locale": "ru", "value": text})}, nil)
		if err != nil {
			rt.Fatal(err)
		}
		if _, err := Apply(document, result.Inverse, nil); err != nil {
			rt.Fatal(err)
		}
		if !reflect.DeepEqual(document, original) {
			rt.Fatal("operation and inverse changed document")
		}
	})
}

func TestLocalContent(t *testing.T) {
	// STU-003, IR-041, CHG-010: locale text has an exact inverse.
	after, _ := roundTrip(t, base, op(LocalContentSet, `{"key":"t_title","type":"text","locale":"ru","value":"Заголовок"}`), nil)
	source := toJSON(t, after)
	roundTrip(t, source, op(LocalContentSet, `{"key":"t_title","type":"text","locale":"ru","value":""}`), nil)
	translated, _ := roundTrip(t, source, op(LocalContentSet, `{"key":"t_title","type":"text","locale":"en","value":"Title"}`), nil)
	if len(translated["localContent"].(map[string]any)["t_title"].(map[string]any)["value"].(map[string]any)) != 2 {
		t.Fatal("lost translation")
	}
	roundTrip(t, toJSON(t, translated), op(LocalContentRemove, `{"key":"t_title"}`), nil)
	roundTrip(t, base, op(LocalContentSet, `{"key":"t_title","type":"text","value":{"ru":"Текст","en":"Text"}}`), nil)
	for _, typ := range []string{LocalContentSet, LocalContentRemove} {
		if right, ok := Right(typ); !ok || right != auth.ContentWrite {
			t.Fatalf("wrong right %s", typ)
		}
	}
}
func TestInvalidLocalContent(t *testing.T) {
	if _, err := Apply(parse(t, base), op(LocalContentSet, `{"key":"t_title","type":"text","value":{"ru":null}}`), nil); err == nil {
		t.Fatal("null translation coerced to empty string")
	}
	for _, raw := range []string{`{`, `{"key":"bad.path","type":"text","locale":"ru","value":"x"}`, `{"key":"t_title","type":"richText","value":{}}`, `{"key":"t_title","type":"text","locale":"ru","value":null}`, `{"key":"t_title","type":"text","locale":"ru","value":1}`, `{"key":"t_title","type":"text","value":{}}`, `{"key":"t_title","type":"text","value":{"bad_locale":"x"}}`, `{"key":"t_title","type":"text","locale":"bad_locale","value":"x"}`} {
		if _, err := Apply(parse(t, base), op(LocalContentSet, raw), nil); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, raw := range []string{`{`, `{"key":"bad.path"}`, `{"key":"t_missing"}`, `{"key":"t_title","value":"x"}`} {
		if _, err := Apply(parse(t, base), op(LocalContentRemove, raw), nil); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, src := range []string{`{"type":"asset","value":{"ru":{}}}`, `{"type":"text","value":"old"}`} {
		doc := parse(t, base)
		doc["localContent"] = map[string]any{"t_title": parse(t, src)}
		for _, operation := range []Op{op(LocalContentSet, `{"key":"t_title","type":"text","locale":"ru","value":"x"}`), op(LocalContentRemove, `{"key":"t_title"}`)} {
			if _, err := Apply(doc, operation, nil); err == nil {
				t.Fatal(src)
			}
		}
	}
}
