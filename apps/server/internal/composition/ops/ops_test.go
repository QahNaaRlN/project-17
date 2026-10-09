package ops

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
)

func parse(t testing.TB, src string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(src))
	dec.UseNumber()
	var v map[string]any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func toJSON(t testing.TB, v any) string {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(buf.String())
}

const base = `{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{
  "n_root":{"id":"n_root","type":"Box","children":["n_head","n_card"]},
  "n_head":{"id":"n_head","type":"Heading","props":{"level":1},"bindings":{"text":"$content.title"}},
  "n_card":{"id":"n_card","type":"ProductCard","slots":{"footer":["n_btn1"]}},
  "n_btn1":{"id":"n_btn1","type":"Button","on":{"click":{"action":"track"}}}}}`

func seqGen(ids ...string) ir.IDGenerator {
	i := 0
	return func(map[string]bool) string { id := ids[i]; i++; return id }
}

// roundTrip применяет операцию и обратную и проверяет возврат к исходному документу.
func roundTrip(t *testing.T, src string, op Op, gen ir.IDGenerator) (map[string]any, Result) {
	t.Helper()
	orig := parse(t, src)
	doc := Clone(orig)
	res, err := Apply(doc, op, gen)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	after := Clone(doc)
	if _, err := Apply(doc, res.Inverse, gen); err != nil {
		t.Fatalf("Apply(inverse %s %s): %v", res.Inverse.Type, res.Inverse.Payload, err)
	}
	if !reflect.DeepEqual(doc, orig) {
		t.Fatalf("операция + обратная != исходный документ\nбыло:  %s\nстало: %s", toJSON(t, orig), toJSON(t, doc))
	}
	return after, res
}

func op(typ, payload string) Op { return Op{Type: typ, Payload: json.RawMessage(payload)} }

func TestInsert(t *testing.T) {
	after, res := roundTrip(t, base, op(NodeInsert,
		`{"parentId":"n_root","index":1,"subtree":{"type":"Stack","children":[{"type":"Text","bindings":{"text":"$content.lead"}}]}}`),
		seqGen("n_new1", "n_new2"))
	if got := after["nodes"].(map[string]any)["n_root"].(map[string]any)["children"]; toJSON(t, got) != `["n_head","n_new1","n_card"]` {
		t.Errorf("children: %v", got)
	}
	if toJSON(t, res.After) != `{"index":1,"nodeId":"n_new1","nodeIds":["n_new1","n_new2"],"parentId":"n_root","slot":""}` {
		t.Errorf("after: %s", toJSON(t, res.After))
	}
}

func TestInsertIntoNewSlotAndAtEnd(t *testing.T) {
	after, _ := roundTrip(t, base, op(NodeInsert, `{"parentId":"n_card","slot":"media","subtree":{"id":"n_img1","type":"Image"}}`), nil)
	card := after["nodes"].(map[string]any)["n_card"].(map[string]any)
	if toJSON(t, card["slots"]) != `{"footer":["n_btn1"],"media":["n_img1"]}` {
		t.Errorf("slots: %s", toJSON(t, card["slots"]))
	}
}

func TestRemoveSubtree(t *testing.T) {
	after, res := roundTrip(t, base, op(NodeRemove, `{"nodeId":"n_card"}`), nil)
	n := after["nodes"].(map[string]any)
	if _, ok := n["n_btn1"]; ok || n["n_card"] != nil {
		t.Error("поддерево не удалено")
	}
	if toJSON(t, res.Before.(map[string]any)["subtree"]) != `{"id":"n_card","slots":{"footer":[{"id":"n_btn1","on":{"click":{"action":"track"}},"type":"Button"}]},"type":"ProductCard"}` {
		t.Errorf("before.subtree: %s", toJSON(t, res.Before))
	}
}

func TestRemoveLastSlotItemDropsEmptySlots(t *testing.T) {
	after, _ := roundTrip(t, base, op(NodeRemove, `{"nodeId":"n_btn1"}`), nil)
	if _, ok := after["nodes"].(map[string]any)["n_card"].(map[string]any)["slots"]; ok {
		t.Error("пустые slots должны удаляться")
	}
}

func TestMove(t *testing.T) {
	after, res := roundTrip(t, base, op(NodeMove, `{"nodeId":"n_btn1","parentId":"n_root","index":0}`), nil)
	if toJSON(t, after["nodes"].(map[string]any)["n_root"].(map[string]any)["children"]) != `["n_btn1","n_head","n_card"]` {
		t.Error("перемещение")
	}
	if toJSON(t, res.Before) != `{"parentId":"n_card","slot":"footer","index":0}` {
		t.Errorf("before: %s", toJSON(t, res.Before))
	}
	// Внутри одного списка индекс — позиция после изъятия.
	after, _ = roundTrip(t, base, op(NodeMove, `{"nodeId":"n_head","parentId":"n_root"}`), nil)
	if toJSON(t, after["nodes"].(map[string]any)["n_root"].(map[string]any)["children"]) != `["n_card","n_head"]` {
		t.Error("перемещение в конец того же списка")
	}
}

func TestSetProps(t *testing.T) {
	after, res := roundTrip(t, base, op(NodeSetProps, `{"nodeId":"n_head","set":{"level":2,"as":"h"},"unset":["missing"]}`), nil)
	if toJSON(t, after["nodes"].(map[string]any)["n_head"].(map[string]any)["props"]) != `{"as":"h","level":2}` {
		t.Error("props")
	}
	if toJSON(t, res.Before) != `{"set":{"level":1},"unset":["as","missing"]}` {
		t.Errorf("before: %s", toJSON(t, res.Before))
	}
	after, _ = roundTrip(t, base, op(NodeSetProps, `{"nodeId":"n_head","unset":["level"]}`), nil)
	if _, ok := after["nodes"].(map[string]any)["n_head"].(map[string]any)["props"]; ok {
		t.Error("пустые props должны удаляться")
	}
}

func TestSetDesign(t *testing.T) {
	after, _ := roundTrip(t, base, op(NodeSetDesign, `{"nodeId":"n_root","set":{"gap":{"base":"md","lg":"xl"},"padding":{"raw":"12px"}}}`), nil)
	if toJSON(t, after["nodes"].(map[string]any)["n_root"].(map[string]any)["design"]) != `{"gap":{"base":"md","lg":"xl"},"padding":{"raw":"12px"}}` {
		t.Error("design")
	}
}

func TestSetBinding(t *testing.T) {
	after, _ := roundTrip(t, base, op(NodeSetBinding, `{"nodeId":"n_head","prop":"text","binding":{"expr":"$content.title","default":""}}`), nil)
	if toJSON(t, after["nodes"].(map[string]any)["n_head"].(map[string]any)["bindings"]) != `{"text":{"default":"","expr":"$content.title"}}` {
		t.Error("binding")
	}
	after, _ = roundTrip(t, base, op(NodeSetBinding, `{"nodeId":"n_head","prop":"text","binding":null}`), nil)
	if _, ok := after["nodes"].(map[string]any)["n_head"].(map[string]any)["bindings"]; ok {
		t.Error("пустые bindings должны удаляться")
	}
	roundTrip(t, base, op(NodeSetBinding, `{"nodeId":"n_root","prop":"title","binding":"$content.title"}`), nil)
}

func TestSetBehavior(t *testing.T) {
	roundTrip(t, base, op(NodeSetBehavior, `{"nodeId":"n_btn1","event":"click","handlers":[{"action":"track"},{"action":"navigate","args":{"to":{"kind":"url","url":"/x"}}}]}`), nil)
	after, _ := roundTrip(t, base, op(NodeSetBehavior, `{"nodeId":"n_btn1","event":"click","handlers":null}`), nil)
	if _, ok := after["nodes"].(map[string]any)["n_btn1"].(map[string]any)["on"]; ok {
		t.Error("пустой on должен удаляться")
	}
}

func TestRenameAndCondition(t *testing.T) {
	after, res := roundTrip(t, base, op(NodeRename, `{"nodeId":"n_head","name":"Заголовок"}`), nil)
	if after["nodes"].(map[string]any)["n_head"].(map[string]any)["name"] != "Заголовок" || toJSON(t, res.Before) != `{"name":null}` {
		t.Errorf("rename: %s", toJSON(t, res.Before))
	}
	roundTrip(t, `{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{"n_root":{"id":"n_root","type":"Box","name":"Старое"}}}`,
		op(NodeRename, `{"nodeId":"n_root","name":null}`), nil)
	roundTrip(t, base, op(NodeSetCondition, `{"nodeId":"n_head","when":{"op":"exists","arg":"$content.title"}}`), nil)
}

func errCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestErrors(t *testing.T) {
	cases := []struct {
		name string
		op   Op
		code string
	}{
		{"неизвестный тип", op("node.explode", `{}`), "OPERATION_UNKNOWN"},
		{"неизвестное поле", op(NodeRemove, `{"nodeId":"n_head","x":1}`), "PAYLOAD_INVALID"},
		{"лишние данные", op(NodeRemove, `{"nodeId":"n_head"} 1`), "PAYLOAD_INVALID"},
		{"нет узла", op(NodeRemove, `{"nodeId":"n_none"}`), "NODE_NOT_FOUND"},
		{"удаление корня", op(NodeRemove, `{"nodeId":"n_root"}`), "ROOT_IMMUTABLE"},
		{"перемещение корня", op(NodeMove, `{"nodeId":"n_root","parentId":"n_card"}`), "ROOT_IMMUTABLE"},
		{"внутрь себя", op(NodeMove, `{"nodeId":"n_card","parentId":"n_card","slot":"footer"}`), "MOVE_INTO_DESCENDANT"},
		{"внутрь потомка", op(NodeMove, `{"nodeId":"n_card","parentId":"n_btn1"}`), "MOVE_INTO_DESCENDANT"},
		{"перемещение: нет узла", op(NodeMove, `{"nodeId":"n_none","parentId":"n_root"}`), "NODE_NOT_FOUND"},
		{"перемещение: нет родителя", op(NodeMove, `{"nodeId":"n_head","parentId":"n_none"}`), "NODE_NOT_FOUND"},
		{"индекс за пределами", op(NodeMove, `{"nodeId":"n_head","parentId":"n_root","index":5}`), "INDEX_OUT_OF_RANGE"},
		{"отрицательный индекс", op(NodeInsert, `{"parentId":"n_root","index":-1,"subtree":{"type":"Box"}}`), "INDEX_OUT_OF_RANGE"},
		{"вставка: нет поддерева", op(NodeInsert, `{"parentId":"n_root"}`), "PAYLOAD_INVALID"},
		{"вставка: нет родителя", op(NodeInsert, `{"parentId":"n_none","subtree":{"type":"Box"}}`), "NODE_NOT_FOUND"},
		{"вставка: занятый ID", op(NodeInsert, `{"parentId":"n_root","subtree":{"id":"n_head","type":"Box"}}`), "NODE_ID_DUPLICATE"},
		{"вставка: плохой ID", op(NodeInsert, `{"parentId":"n_root","subtree":{"id":"x","type":"Box"}}`), "PAYLOAD_INVALID"},
		{"вставка ломает документ", op(NodeInsert, `{"parentId":"n_root","subtree":{"type":"Text","bindings":{"text":"$bad"}}}`), "OPERATION_INVALID"},
		{"set и unset пусты", op(NodeSetProps, `{"nodeId":"n_head"}`), "PAYLOAD_INVALID"},
		{"ключ в set и unset", op(NodeSetProps, `{"nodeId":"n_head","set":{"a":1},"unset":["a"]}`), "PAYLOAD_INVALID"},
		{"props: нет узла", op(NodeSetProps, `{"nodeId":"n_none","set":{"a":1}}`), "NODE_NOT_FOUND"},
		{"дизайн: CSS-строка", op(NodeSetDesign, `{"nodeId":"n_root","set":{"gap":"1px; color: red"}}`), "OPERATION_INVALID"},
		{"привязка без prop", op(NodeSetBinding, `{"nodeId":"n_head","binding":"$content.a"}`), "PAYLOAD_INVALID"},
		{"привязка без значения", op(NodeSetBinding, `{"nodeId":"n_head","prop":"text"}`), "PAYLOAD_INVALID"},
		{"привязка: нет узла", op(NodeSetBinding, `{"nodeId":"n_none","prop":"text","binding":null}`), "NODE_NOT_FOUND"},
		{"переименование: нет name", op(NodeRename, `{"nodeId":"n_head"}`), "PAYLOAD_INVALID"},
		{"переименование: нет nodeId", op(NodeRename, `{"name":"x","other":1}`), "PAYLOAD_INVALID"},
		{"переименование: лишнее поле", op(NodeRename, `{"nodeId":"n_head","name":"x","y":1}`), "PAYLOAD_INVALID"},
		{"переименование: nodeId не строка", op(NodeRename, `{"nodeId":1,"name":"x"}`), "PAYLOAD_INVALID"},
		{"переименование: нет узла", op(NodeRename, `{"nodeId":"n_none","name":"x"}`), "NODE_NOT_FOUND"},
		{"переименование: не объект", op(NodeRename, `[]`), "PAYLOAD_INVALID"},
		{"обработчики: не тот payload", op(NodeSetBehavior, `{"nodeId":"n_btn1"}`), "PAYLOAD_INVALID"},
		{"обработчики: лишнее поле", op(NodeSetBehavior, `{"x":1}`), "PAYLOAD_INVALID"},
		{"props: не объект", op(NodeSetProps, `[]`), "PAYLOAD_INVALID"},
		{"вставка: не объект", op(NodeInsert, `[]`), "PAYLOAD_INVALID"},
		{"перемещение: не объект", op(NodeMove, `[]`), "PAYLOAD_INVALID"},
		{"привязка: не объект", op(NodeSetBinding, `[]`), "PAYLOAD_INVALID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := parse(t, base)
			_, err := Apply(doc, tc.op, nil)
			if got := errCode(err); got != tc.code {
				t.Errorf("код %q, ожидалось %q (%v)", got, tc.code, err)
			}
		})
	}
}

func TestOperationInvalidCarriesDiagnostics(t *testing.T) {
	_, err := Apply(parse(t, base), op(NodeSetDesign, `{"nodeId":"n_root","set":{"gap":"1px; x"}}`), nil)
	var e *Error
	if !errors.As(err, &e) || len(e.Diagnostics) == 0 || e.Diagnostics[0].Pointer != "/nodes/n_root/design/gap" {
		t.Errorf("диагностики: %+v", e)
	}
	if !strings.HasPrefix(e.Error(), "OPERATION_INVALID: ") {
		t.Errorf("Error(): %s", e.Error())
	}
}

func TestRightsAndTypes(t *testing.T) {
	if r, ok := Right(NodeSetBehavior); !ok || r != auth.BehaviorUse {
		t.Errorf("node.setBehavior: %v %v", r, ok)
	}
	if r, ok := Right(NodeInsert); !ok || r != auth.DesignCompose {
		t.Errorf("node.insert: %v", r)
	}
	if _, ok := Right("x"); ok {
		t.Error("неизвестный тип")
	}
	if got := Types(); len(got) != 9 || got[0] != NodeInsert {
		t.Errorf("Types: %v", got)
	}
}

func TestCloneIsDeep(t *testing.T) {
	orig := parse(t, base)
	c := Clone(orig)
	c["nodes"].(map[string]any)["n_root"].(map[string]any)["children"].([]any)[0] = "x"
	if orig["nodes"].(map[string]any)["n_root"].(map[string]any)["children"].([]any)[0] != "n_head" {
		t.Error("Clone не глубокая")
	}
}
