package schemadiag_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/schemadiag"
)

const testSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "name": { "type": "string", "minLength": 2, "maxLength": 3, "pattern": "^[a-z]+$" },
    "tags": { "type": "array", "minItems": 1, "maxItems": 2, "uniqueItems": true },
    "kind": { "enum": ["a", "b"] },
    "fixed": { "const": 1 },
    "props": { "type": "object", "minProperties": 1, "maxProperties": 1, "propertyNames": { "pattern": "^[a-z]+$" } },
    "never": false,
    "not": { "not": { "type": "string" } },
    "count": { "type": "integer", "minimum": 0 },
    "shape": {
      "type": "object",
      "oneOf": [
        { "type": "object", "required": ["type", "r"], "properties": { "type": { "const": "circle" }, "r": { "type": "number" } } },
        { "type": "object", "required": ["type", "w"], "properties": { "type": { "const": "rect" }, "w": { "type": "number" } } }
      ]
    },
    "req": { "type": "object", "required": ["x"] }
  }
}`

func collect(t *testing.T, instance string) map[string]schemadiag.Error {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(testSchema))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("test.json", doc); err != nil {
		t.Fatal(err)
	}
	schema := c.MustCompile("test.json")
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(instance))
	if err != nil {
		t.Fatal(err)
	}
	var verr *jsonschema.ValidationError
	if !errors.As(schema.Validate(v), &verr) {
		t.Fatal("ожидалась ошибка валидации")
	}
	out := map[string]schemadiag.Error{}
	for _, e := range schemadiag.Collect(v, verr) {
		out[e.Pointer] = e
	}
	return out
}

func TestMessagesByKind(t *testing.T) {
	for instance, want := range map[string]struct{ ptr, msg, keyword string }{
		`{"name":"a"}`:                      {"/name", "не менее, чем 2 символов", "minLength"},
		`{"name":"abcd"}`:                   {"/name", "не более, чем 3 символов", "maxLength"},
		`{"name":"A1"}`:                     {"/name", "образцу", "pattern"},
		`{"name":1}`:                        {"/name", "должно быть string", "type"},
		`{"tags":[]}`:                       {"/tags", "не менее, чем 1 элементов", "minItems"},
		`{"tags":[1,2,3]}`:                  {"/tags", "не более, чем 2 элементов", "maxItems"},
		`{"tags":[1,1]}`:                    {"/tags", "повторяющихся", "uniqueItems"},
		`{"kind":"c"}`:                      {"/kind", "разрешенных значений", "enum"},
		`{"fixed":2}`:                       {"/fixed", "заданному значению", "const"},
		`{"props":{}}`:                      {"/props", "не менее, чем 1 полей", "minProperties"},
		`{"props":{"a":1,"b":2}}`:           {"/props", "не более, чем 1 полей", "maxProperties"},
		`{"props":{"Bad":1}}`:               {"/props/Bad", "недопустимое имя свойства Bad: должно соответствовать образцу", "propertyNames"},
		`{"never":1}`:                       {"/never", "недопустимо в этом контексте", "false schema"},
		`{"not":"x"}`:                       {"/not", `не соответствовать схеме в "not"`, ""},
		`{"extra/1":1}`:                     {"/extra~11", "дополнительных полей", "additionalProperties"},
		`{"req":{}}`:                        {"/req", "обязательное поле x", "required"},
		`{"count":-1}`:                      {"/count", "", "minimum"},
		`{"shape":{"type":"rect","w":"x"}}`: {"/shape/w", "должно быть number", "type"},
	} {
		t.Run(instance, func(t *testing.T) {
			got, ok := collect(t, instance)[want.ptr]
			if !ok {
				t.Fatalf("нет ошибки в %s: %v", want.ptr, collect(t, instance))
			}
			if !strings.Contains(got.Message, want.msg) || got.Keyword != want.keyword {
				t.Errorf("получено %+v, ожидалось %+v", got, want)
			}
		})
	}
}

// Ветка oneOf с несовпавшим тегом не даёт ошибок; без совпавших веток — ошибки тега.
func TestOneOfTagMismatch(t *testing.T) {
	got := collect(t, `{"shape":{"type":"circle","r":"big"}}`)
	if len(got) != 1 || got["/shape/r"].Keyword != "type" {
		t.Errorf("совпавшая ветка: %v", got)
	}
	got = collect(t, `{"shape":{"type":"oval"}}`)
	if got["/shape/type"].Keyword != "const" {
		t.Errorf("неизвестный тег: %v", got)
	}
}

// Расплывчатая ошибка места убирается, если у потомков есть свои.
func TestVagueErrorsYieldToDescendants(t *testing.T) {
	got := collect(t, `{"shape":{"type":"rect"},"name":"x"}`)
	if _, ok := got["/shape"]; !ok {
		t.Errorf("required у объекта: %v", got)
	}
	if _, vague := got[""]; vague {
		t.Errorf("корень с ошибками потомков: %v", got)
	}
}

func TestPointerSegment(t *testing.T) {
	if got := schemadiag.PointerSegment("a/b~c"); got != "a~1b~0c" {
		t.Error(got)
	}
}
