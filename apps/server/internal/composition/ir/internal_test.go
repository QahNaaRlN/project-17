package ir

import (
	"errors"
	"reflect"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// Тесты внутренних функций, которые трудно достичь через публичный API.

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

func TestMalformedSlotsDoNotHideOtherSlots(t *testing.T) {
	res := ValidateDocument(decode(t, doc(nodes(
		`"n_root":{"id":"n_root","type":"Card","slots":{"a":5,"b":["n_kid1",7],"c":["n_kid1"]}}`,
		box("n_kid1")))))
	var multi []string
	for _, d := range res.Diagnostics {
		if d.Code == CodeNodeMultipleParents {
			multi = append(multi, d.Pointer)
		}
	}
	if !reflect.DeepEqual(multi, []string{"/nodes/n_root/slots/c/0"}) {
		t.Errorf("получено %s", mustJSON(res.Diagnostics))
	}
}

// Узел с пустым ключом не должен приниматься за цикл: отсутствие родителя и пустой ID
// различаются (validate.go, классификация недостижимых узлов).
func TestOrphanWithEmptyKeyIsNotACycle(t *testing.T) {
	res := ValidateDocument(decode(t, doc(nodes(box("n_root"), `"":{"id":"","type":"Box"}`))))
	codes := map[Code]bool{}
	for _, d := range res.Diagnostics {
		codes[d.Code] = true
	}
	if !codes[CodeNodeOrphan] || codes[CodeNodeCycle] {
		t.Errorf("получено %s", mustJSON(res.Diagnostics))
	}
}

func TestNormalizeRejectsNonStringID(t *testing.T) {
	_, err := Normalize(decode(t, `{"root":{"id":42,"type":"Box"}}`), nil)
	var nerr *NormalizeError
	if !errors.As(err, &nerr) || nerr.Diagnostics[0].Pointer != "/root/id" || nerr.Diagnostics[0].Message != "Недопустимый ID узла: 42" {
		t.Errorf("получено %v", err)
	}
}
