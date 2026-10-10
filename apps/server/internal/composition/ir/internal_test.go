package ir

import (
	"errors"
	"reflect"
	"testing"
)

// Тесты внутренних функций, которые трудно достичь через публичный API.

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
