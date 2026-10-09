package ops

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"pgregory.net/rapid"
)

// Свойства операций для любых документов и любых применимых операций (docs/testing.md §3.3).

// randomDoc строит валидный документ: дерево контейнеров и листьев со слотами.
func randomDoc(t *rapid.T) map[string]any {
	nodes := map[string]any{}
	n := 0
	var build func(depth int) string
	build = func(depth int) string {
		id := fmt.Sprintf("n_%04d", n)
		n++
		node := map[string]any{"id": id, "type": rapid.SampledFrom([]string{"Box", "Text", "Card"}).Draw(t, "type")}
		if rapid.Bool().Draw(t, "props") {
			node["props"] = map[string]any{"level": json.Number("1")}
		}
		if rapid.Bool().Draw(t, "design") {
			node["design"] = map[string]any{"gap": "md"}
		}
		nodes[id] = node
		if depth < 3 {
			if k := rapid.IntRange(0, 3).Draw(t, "children"); k > 0 {
				kids := make([]any, k)
				for i := range kids {
					kids[i] = build(depth + 1)
				}
				node["children"] = kids
			}
			if rapid.IntRange(0, 3).Draw(t, "hasSlot") == 0 {
				node["slots"] = map[string]any{"footer": []any{build(depth + 1)}}
			}
		}
		return id
	}
	root := build(0)
	return map[string]any{"irVersion": "1.0", "kind": "page", "root": root, "nodes": nodes}
}

func ids(doc map[string]any) []string { return sortedKeys(nodes(doc)) }

func randomOp(t *rapid.T, doc map[string]any) Op {
	all := ids(doc)
	nonRoot := []string{}
	for _, id := range all {
		if id != doc["root"] {
			nonRoot = append(nonRoot, id)
		}
	}
	pick := func(label string, from []string) string { return rapid.SampledFrom(from).Draw(t, label) }
	raw := func(v any) json.RawMessage { return mustRaw(v) }
	slot := rapid.SampledFrom([]string{"", "", "footer", "media"}).Draw(t, "slot")

	kinds := []string{NodeInsert, NodeSetProps, NodeSetDesign, NodeSetBinding, NodeSetBehavior, NodeRename, NodeSetCondition}
	if len(nonRoot) > 0 {
		kinds = append(kinds, NodeRemove, NodeMove)
	}
	switch kind := rapid.SampledFrom(kinds).Draw(t, "op"); kind {
	case NodeInsert:
		parent := pick("parent", all)
		p := map[string]any{"parentId": parent, "subtree": map[string]any{"type": "Text", "children": []any{map[string]any{"type": "Box"}}}}
		if slot != "" {
			p["slot"] = slot
		}
		if rapid.Bool().Draw(t, "withIndex") {
			p["index"] = rapid.IntRange(0, len(list(nodes(doc)[parent].(map[string]any), slot))).Draw(t, "index")
		}
		return Op{kind, raw(p)}
	case NodeRemove:
		return Op{kind, raw(map[string]any{"nodeId": pick("node", nonRoot)})}
	case NodeMove:
		p := map[string]any{"nodeId": pick("node", nonRoot), "parentId": pick("parent", all)}
		if slot != "" {
			p["slot"] = slot
		}
		return Op{kind, raw(p)}
	case NodeSetProps, NodeSetDesign:
		key := rapid.SampledFrom([]string{"level", "gap", "as"}).Draw(t, "key")
		p := map[string]any{"nodeId": pick("node", all)}
		if rapid.Bool().Draw(t, "unset") {
			p["unset"] = []string{key}
		} else {
			p["set"] = map[string]any{key: "lg"}
		}
		return Op{kind, raw(p)}
	case NodeSetBinding:
		var b any
		if rapid.Bool().Draw(t, "bind") {
			b = "$content.title"
		}
		return Op{kind, raw(map[string]any{"nodeId": pick("node", all), "prop": "text", "binding": b})}
	case NodeSetBehavior:
		var h any
		if rapid.Bool().Draw(t, "handle") {
			h = map[string]any{"action": "track"}
		}
		return Op{kind, raw(map[string]any{"nodeId": pick("node", all), "event": "click", "handlers": h})}
	case NodeRename:
		var name any
		if rapid.Bool().Draw(t, "named") {
			name = "Имя"
		}
		return Op{kind, raw(map[string]any{"nodeId": pick("node", all), "name": name})}
	default:
		var when any
		if rapid.Bool().Draw(t, "cond") {
			when = map[string]any{"op": "exists", "arg": "$content.a"}
		}
		return Op{NodeSetCondition, raw(map[string]any{"nodeId": pick("node", all), "when": when})}
	}
}

// Любая успешно применённая операция + её обратная возвращают документ к исходному виду,
// а документ после операции остаётся валидным.
func TestPropertyInverseRestoresDocument(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		orig := randomDoc(t)
		if v := ir.ValidateDocument(orig); !v.Valid {
			t.Fatalf("генератор дал невалидный документ: %v", v.Diagnostics)
		}
		op := randomOp(t, orig)
		doc := Clone(orig)
		res, err := Apply(doc, op, nil)
		if err != nil {
			if errCode(err) == "" {
				t.Fatalf("ошибка без кода: %v", err)
			}
			return // неприменимая операция (например, перемещение внутрь себя) — допустимо
		}
		if v := ir.ValidateDocument(doc); !v.Valid {
			t.Fatalf("документ после %s невалиден: %v", op.Type, v.Diagnostics)
		}
		if _, err := Apply(doc, res.Inverse, nil); err != nil {
			t.Fatalf("обратная к %s %s не применилась: %v", op.Type, op.Payload, err)
		}
		if !reflect.DeepEqual(doc, orig) {
			t.Fatalf("%s %s: операция + обратная != исходный документ", op.Type, op.Payload)
		}
	})
}

// Последовательность операций, отменённая в обратном порядке, возвращает исходный документ
// (так работает undo нескольких шагов).
func TestPropertyUndoSequence(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		orig := randomDoc(t)
		doc := Clone(orig)
		var inverses []Op
		for range rapid.IntRange(1, 6).Draw(t, "steps") {
			res, err := Apply(doc, randomOp(t, doc), nil)
			if err == nil {
				inverses = append(inverses, res.Inverse)
			}
		}
		for i := len(inverses) - 1; i >= 0; i-- {
			if _, err := Apply(doc, inverses[i], nil); err != nil {
				t.Fatalf("отмена шага %d: %v", i, err)
			}
		}
		if !reflect.DeepEqual(doc, orig) {
			t.Fatal("отмена последовательности не вернула исходный документ")
		}
	})
}
