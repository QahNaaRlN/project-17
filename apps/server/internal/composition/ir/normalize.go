package ir

import (
	"fmt"
	"strings"
)

// IDGenerator возвращает новый ID узла, не встречающийся в taken.
type IDGenerator func(taken map[string]bool) string

// NormalizeError — документ во вложенной форме нельзя нормализовать.
type NormalizeError struct {
	Diagnostics []Diagnostic
}

func (e *NormalizeError) Error() string {
	msgs := make([]string, len(e.Diagnostics))
	for i, d := range e.Diagnostics {
		msgs[i] = d.Message
	}
	return strings.Join(msgs, "; ")
}

// Normalize преобразует документ во вложенной форме (02-ir.md §10) в нормализованный:
// дети и слоты — узлы, а не ID; ID узла необязателен. Заданные ID сохраняются — на них
// могут ссылаться действия (openModal, anchor); узлам без ID назначаются новые (gen,
// по умолчанию GenerateNodeID). Недопустимые и повторяющиеся ID — *NormalizeError.
// Результат не валидируется: вызывающий проверяет его ValidateDocument.
func Normalize(nested map[string]any, gen IDGenerator) (map[string]any, error) {
	if gen == nil {
		gen = GenerateNodeID
	}
	rootNode, ok := nested["root"].(map[string]any)
	if !ok {
		return nil, &NormalizeError{[]Diagnostic{shapeError("/root", "корень должен быть узлом")}}
	}

	// Первый проход: зарезервировать явно заданные ID, чтобы сгенерированные с ними не совпали.
	taken := map[string]bool{}
	var errs []Diagnostic
	walkNested(rootNode, "/root", func(node map[string]any, ptr string) {
		raw, has := node["id"]
		if !has {
			return
		}
		id, isString := raw.(string)
		switch {
		case !isString || !IsNodeID(id):
			errs = append(errs, Diagnostic{
				Code:     CodeSchemaViolation,
				Severity: SeverityError,
				Pointer:  ptr + "/id",
				Message:  fmt.Sprintf("Недопустимый ID узла: %v", raw),
				Params:   map[string]any{"id": raw},
			})
		case taken[id]:
			errs = append(errs, Diagnostic{
				Code:     CodeNodeIDDuplicate,
				Severity: SeverityError,
				Pointer:  ptr + "/id",
				NodeID:   id,
				Message:  fmt.Sprintf("ID узла %s встречается несколько раз", id),
				Params:   map[string]any{"id": id},
			})
		default:
			taken[id] = true
		}
	}, &errs)
	if len(errs) > 0 {
		return nil, &NormalizeError{errs}
	}

	nodes := map[string]any{}
	var flatten func(node map[string]any) string
	flatten = func(node map[string]any) string {
		id, _ := node["id"].(string)
		if id == "" {
			id = gen(taken)
			taken[id] = true
		}
		flat := map[string]any{}
		for k, v := range node {
			if k != "children" && k != "slots" {
				flat[k] = v
			}
		}
		flat["id"] = id
		nodes[id] = flat
		if children, ok := node["children"].([]any); ok {
			ids := make([]any, len(children))
			for i, c := range children {
				ids[i] = flatten(c.(map[string]any)) // тип проверен в walkNested
			}
			flat["children"] = ids
		}
		if slots, ok := node["slots"].(map[string]any); ok {
			flatSlots := map[string]any{}
			for name, list := range slots {
				items := list.([]any)
				ids := make([]any, len(items))
				for i, c := range items {
					ids[i] = flatten(c.(map[string]any))
				}
				flatSlots[name] = ids
			}
			flat["slots"] = flatSlots
		}
		return id
	}

	out := map[string]any{}
	for k, v := range nested {
		if k != "root" {
			out[k] = v
		}
	}
	out["root"] = flatten(rootNode)
	out["nodes"] = nodes
	return out, nil
}

// ToNested — обратное преобразование во вложенную форму (для агентов и экспорта).
// Узлы, недостижимые от корня, отбрасываются. Документ должен быть валиден.
func ToNested(doc map[string]any) (map[string]any, error) {
	nodes, _ := doc["nodes"].(map[string]any)
	visiting := map[string]bool{}

	var build func(id, ptr string) (map[string]any, error)
	build = func(id, ptr string) (map[string]any, error) {
		node, ok := nodes[id].(map[string]any)
		if !ok {
			return nil, &NormalizeError{[]Diagnostic{{
				Code:     CodeNodeNotFound,
				Severity: SeverityError,
				Pointer:  ptr,
				Message:  fmt.Sprintf("Узел %s отсутствует в nodes", id),
				Params:   map[string]any{"nodeId": id},
			}}}
		}
		if visiting[id] {
			return nil, &NormalizeError{[]Diagnostic{{
				Code:     CodeNodeCycle,
				Severity: SeverityError,
				Pointer:  Pointer("nodes", id),
				NodeID:   id,
				Message:  fmt.Sprintf("Узел %s входит в цикл", id),
				Params:   map[string]any{},
			}}}
		}
		visiting[id] = true
		defer delete(visiting, id)

		nested := map[string]any{}
		for k, v := range node {
			if k != "children" && k != "slots" {
				nested[k] = v
			}
		}
		if children, ok := node["children"].([]any); ok {
			list := make([]any, len(children))
			for i, c := range children {
				childID, _ := c.(string)
				child, err := build(childID, Pointer("nodes", id, "children", i))
				if err != nil {
					return nil, err
				}
				list[i] = child
			}
			nested["children"] = list
		}
		if slots, ok := node["slots"].(map[string]any); ok {
			nestedSlots := map[string]any{}
			for _, name := range sortedKeys(slots) {
				items, _ := slots[name].([]any)
				list := make([]any, len(items))
				for i, c := range items {
					childID, _ := c.(string)
					child, err := build(childID, Pointer("nodes", id, "slots", name, i))
					if err != nil {
						return nil, err
					}
					list[i] = child
				}
				nestedSlots[name] = list
			}
			nested["slots"] = nestedSlots
		}
		return nested, nil
	}

	root, _ := doc["root"].(string)
	rootNode, err := build(root, "/root")
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	for k, v := range doc {
		if k != "root" && k != "nodes" {
			out[k] = v
		}
	}
	out["root"] = rootNode
	return out, nil
}

// walkNested обходит вложенное дерево (children, затем слоты по имени) и сообщает в errs
// о детях, не являющихся узлами.
func walkNested(node map[string]any, ptr string, visit func(map[string]any, string), errs *[]Diagnostic) {
	visit(node, ptr)
	visitList := func(list any, listPtr string) {
		items, ok := list.([]any)
		if !ok {
			*errs = append(*errs, shapeError(listPtr, "должно быть массивом узлов"))
			return
		}
		for i, c := range items {
			childPtr := fmt.Sprintf("%s/%d", listPtr, i)
			child, ok := c.(map[string]any)
			if !ok {
				*errs = append(*errs, shapeError(childPtr, "должно быть узлом"))
				continue
			}
			walkNested(child, childPtr, visit, errs)
		}
	}
	if children, has := node["children"]; has {
		visitList(children, ptr+"/children")
	}
	if rawSlots, has := node["slots"]; has {
		slots, ok := rawSlots.(map[string]any)
		if !ok {
			*errs = append(*errs, shapeError(ptr+"/slots", "должно быть объектом слотов"))
			return
		}
		for _, name := range sortedKeys(slots) {
			visitList(slots[name], ptr+"/slots/"+PointerSegment(name))
		}
	}
}

func shapeError(ptr, msg string) Diagnostic {
	return Diagnostic{Code: CodeSchemaViolation, Severity: SeverityError, Pointer: ptr, Message: msg}
}
