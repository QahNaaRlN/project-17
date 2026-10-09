package ir

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"pgregory.net/rapid"
)

// Свойства, верные для любых входных данных (docs/testing.md §3.3).
// Те же свойства проверяет packages/ir/test/properties.test.ts.

// nestedTree порождает случайное дерево во вложенной форме.
func nestedTree(depth int) *rapid.Generator[map[string]any] {
	return rapid.Custom(func(t *rapid.T) map[string]any {
		leaf := func() map[string]any {
			node := map[string]any{"type": rapid.SampledFrom([]string{"Text", "Heading", "Image", "Button"}).Draw(t, "leafType")}
			if rapid.Bool().Draw(t, "hasBinding") {
				node["bindings"] = rapid.SampledFrom([]map[string]any{
					{"text": "$content.title"},
					{"text": map[string]any{"expr": "$item.price", "format": map[string]any{"fn": "currency"}}},
					{"src": "$props.image"},
				}).Draw(t, "bindings")
			}
			if rapid.Bool().Draw(t, "hasDesign") {
				node["design"] = rapid.SampledFrom([]map[string]any{
					{"color": "text"},
					{"padding": map[string]any{"raw": "12px"}},
					{"gap": map[string]any{"base": "md", "lg": "xl"}},
				}).Draw(t, "design")
			}
			return node
		}
		if depth == 0 {
			return leaf()
		}
		switch rapid.IntRange(0, 2).Draw(t, "kind") {
		case 0:
			return leaf()
		case 1:
			n := rapid.IntRange(0, 4).Draw(t, "children")
			children := make([]any, n)
			for i := range children {
				children[i] = nestedTree(depth-1).Draw(t, "child")
			}
			return map[string]any{"type": rapid.SampledFrom([]string{"Box", "Stack", "Grid"}).Draw(t, "containerType"), "children": children}
		default:
			slots := map[string]any{}
			for _, name := range []string{"footer", "media"} {
				if rapid.Bool().Draw(t, "hasSlot") {
					n := rapid.IntRange(0, 2).Draw(t, "slotItems")
					items := make([]any, n)
					for i := range items {
						items[i] = nestedTree(depth-1).Draw(t, "slotChild")
					}
					slots[name] = items
				}
			}
			return map[string]any{"type": "ProductCard", "slots": slots}
		}
	})
}

func nestedDocument() *rapid.Generator[map[string]any] {
	return rapid.Custom(func(t *rapid.T) map[string]any {
		return map[string]any{"irVersion": "1.0", "kind": "page", "root": nestedTree(4).Draw(t, "root")}
	})
}

// roundTripJSON приводит значение к виду, в котором его вернул бы разбор JSON.
func roundTripJSON(t *rapid.T, v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	out, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func normalized(t *rapid.T) map[string]any {
	doc, err := Normalize(roundTripJSON(t, nestedDocument().Draw(t, "nested")).(map[string]any), nil)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestPropertyNormalizeYieldsValidDocument(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		if res := ValidateDocument(normalized(t)); !res.Valid || len(res.Diagnostics) != 0 {
			t.Fatalf("диагностики: %s", mustJSON(res.Diagnostics))
		}
	})
}

func TestPropertyRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		doc := normalized(t)
		nested, err := ToNested(doc)
		if err != nil {
			t.Fatal(err)
		}
		back, err := Normalize(nested, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(back, doc) {
			t.Fatalf("Normalize(ToNested(doc)) != doc")
		}
	})
}

type link struct {
	parent string
	list   []any
	index  int
}

func childLinks(doc map[string]any) []link {
	var links []link
	nodes := doc["nodes"].(map[string]any)
	for _, id := range sortedKeys(nodes) {
		node := nodes[id].(map[string]any)
		if children, ok := node["children"].([]any); ok {
			for i := range children {
				links = append(links, link{id, children, i})
			}
		}
		if slots, ok := node["slots"].(map[string]any); ok {
			for _, name := range sortedKeys(slots) {
				list := slots[name].([]any)
				for i := range list {
					links = append(links, link{id, list, i})
				}
			}
		}
	}
	return links
}

func TestPropertyRemovedLinkMakesOrphan(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		doc := normalized(t)
		links := childLinks(doc)
		if len(links) == 0 {
			t.Skip("нет ссылок")
		}
		l := links[rapid.IntRange(0, len(links)-1).Draw(t, "link")]
		orphan := l.list[l.index].(string)
		// Удаляем ссылку на месте: в документе тот же срез.
		replaceList(doc, l.parent, l.list, slices.Delete(slices.Clone(l.list), l.index, l.index+1))
		found := false
		for _, d := range ValidateDocument(doc).Diagnostics {
			if d.Code == CodeNodeOrphan && d.NodeID == orphan {
				found = true
			}
		}
		if !found {
			t.Fatalf("сирота %s не обнаружена", orphan)
		}
	})
}

// replaceList заменяет список детей или слота узла parent, идентичный old, на new.
func replaceList(doc map[string]any, parent string, old, new []any) {
	node := doc["nodes"].(map[string]any)[parent].(map[string]any)
	same := func(a []any) bool { return len(a) > 0 && len(old) > 0 && &a[0] == &old[0] }
	if c, ok := node["children"].([]any); ok && same(c) {
		node["children"] = new
		return
	}
	for name, l := range node["slots"].(map[string]any) {
		if same(l.([]any)) {
			node["slots"].(map[string]any)[name] = new
			return
		}
	}
	panic("список не найден")
}

func TestPropertySecondParentDetected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		doc := normalized(t)
		nodes := doc["nodes"].(map[string]any)
		var nonRoot, containers []string
		for _, id := range sortedKeys(nodes) {
			if id != doc["root"] {
				nonRoot = append(nonRoot, id)
			}
			if _, ok := nodes[id].(map[string]any)["children"]; ok {
				containers = append(containers, id)
			}
		}
		if len(nonRoot) == 0 || len(containers) == 0 {
			t.Skip("нет подходящих узлов")
		}
		child := rapid.SampledFrom(nonRoot).Draw(t, "child")
		parent := nodes[rapid.SampledFrom(containers).Draw(t, "parent")].(map[string]any)
		parent["children"] = append(parent["children"].([]any), child)
		for _, d := range ValidateDocument(doc).Diagnostics {
			// Если новый родитель — потомок ребёнка, получается цикл, а не второй родитель.
			if d.Code == CodeNodeMultipleParents || d.Code == CodeNodeCycle {
				return
			}
		}
		t.Fatalf("второй родитель %v узла %s не обнаружен", parent["id"], child)
	})
}

// jsonValue порождает произвольные значения JSON.
func jsonValue(depth int) *rapid.Generator[any] {
	return rapid.Custom(func(t *rapid.T) any {
		max := 5
		if depth == 0 {
			max = 3
		}
		switch rapid.IntRange(0, max).Draw(t, "kind") {
		case 0:
			return nil
		case 1:
			return rapid.Bool().Draw(t, "bool")
		case 2:
			return json.Number(rapid.SampledFrom([]string{"0", "-1", "3.5", "1e3"}).Draw(t, "num"))
		case 3:
			return rapid.SampledFrom([]string{"", "n_root", "1.0", "page", "$content.title", "x"}).Draw(t, "str")
		case 4:
			return rapid.SliceOfN(jsonValue(depth-1), 0, 4).Draw(t, "arr")
		default:
			keys := rapid.SliceOfN(rapid.SampledFrom([]string{"irVersion", "kind", "root", "nodes", "n_root", "id", "type", "children", "slots", "props", "bindings", "when", "op", "args", "x"}), 0, 6).Draw(t, "keys")
			obj := map[string]any{}
			for _, k := range keys {
				obj[k] = jsonValue(depth-1).Draw(t, "val")
			}
			return obj
		}
	})
}

func TestPropertyValidatorNeverPanics(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		v := jsonValue(4).Draw(t, "doc")
		res := ValidateDocument(v)
		hasError := slices.ContainsFunc(res.Diagnostics, func(d Diagnostic) bool { return d.Severity == SeverityError })
		if res.Valid == hasError {
			t.Fatalf("Valid=%v не согласован с диагностиками", res.Valid)
		}
	})
}

func TestPropertyValidatorNeverPanicsOnDocumentShapes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		nodes := rapid.MapOfN(rapid.SampledFrom([]string{"n_root", "n_aaaa", "n_bbbb", "x", "n/a"}), jsonValue(3), 0, 5).Draw(t, "nodes")
		doc := map[string]any{"irVersion": "1.0", "kind": "page", "root": jsonValue(1).Draw(t, "root"), "nodes": map[string]any{}}
		for k, v := range nodes {
			doc["nodes"].(map[string]any)[k] = v
		}
		ValidateDocument(doc)
	})
}
