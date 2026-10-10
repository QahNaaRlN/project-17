package schemadiag

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

func decode(t *testing.T, src string) map[string]any {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	return v.(map[string]any)
}

func TestPropertyNamesLocation(t *testing.T) {
	instance := decode(t, `{"a":{"list":[{"x":1},{"bad":1}],"obj":{"bad":2}},"b":{"bad":3}}`)
	cases := []struct {
		name    string
		trusted []string
		depth   int
		want    []string
	}{
		{"через массив", []string{"a"}, 3, []string{"a", "list", "1"}},
		{"первый кандидат в лексикографическом порядке", nil, 2, []string{"a", "obj"}},
		{"предок длиннее глубины обрезается", []string{"b", "bad"}, 1, []string{"b"}},
		{"не найдено — достоверный путь", []string{"a"}, 4, []string{"a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := propertyNamesLocation(instance, tc.trusted, tc.depth, "bad"); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("получено %v, ожидалось %v", got, tc.want)
			}
		})
	}
}

func TestChild(t *testing.T) {
	arr := []any{"a", "b"}
	for seg, want := range map[string]any{"1": "b", "2": nil, "-1": nil, "x": nil} {
		if got := child(arr, seg); got != want {
			t.Errorf("child(arr, %q) = %v", seg, got)
		}
	}
	if child(42, "a") != nil {
		t.Error("child(scalar) != nil")
	}
}

func TestPropertyNamesMessageWithoutDetail(t *testing.T) {
	if got := errorMessage(schemaError{kind: &kind.PropertyNames{Property: "x"}}); got != "недопустимое имя свойства x" {
		t.Errorf("сообщение: %s", got)
	}
}
