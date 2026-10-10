// Package ops — операции над документом IR (06-changes-publishing.md §2.2) как чистые функции.
//
// Apply изменяет документ (map, полученный разбором JSON) и возвращает данные «до» и «после»
// (CHG-010) и обратную операцию для отмены (§2.4). Документ после операции проходит
// структурную валидацию (L1); проверки по manifest и политикам добавятся вместе с ними.
//
// Пустые коллекции, которые операция оставила после себя (children, слот, props, design,
// bindings, on), удаляются: так «операция + обратная» возвращает документ в точности к
// исходному виду.
package ops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
)

// Типы операций над узлами.
const (
	NodeInsert       = "node.insert"
	NodeRemove       = "node.remove"
	NodeMove         = "node.move"
	NodeSetProps     = "node.setProps"
	NodeSetDesign    = "node.setDesign"
	NodeSetBinding   = "node.setBinding"
	NodeSetCondition = "node.setCondition"
	NodeSetBehavior  = "node.setBehavior"
	NodeRename       = "node.rename"
)

// Op — операция: тип и payload.
type Op struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Result — итог применения операции.
type Result struct {
	Before  any // минимальный фрагмент состояния до операции (nil — неприменимо)
	After   any // минимальный фрагмент после операции
	Inverse Op  // операция, отменяющая эту
	// Payload — payload в каноническом виде для журнала, если он отличается от переданного:
	// у node.insert — поддерево с назначенными ID, чтобы повторное применение (rebase) давало
	// те же узлы. nil — записывается переданный payload.
	Payload json.RawMessage
}

// Error — операция неприменима к документу.
type Error struct {
	Code        string
	Message     string
	Diagnostics []ir.Diagnostic // для OPERATION_INVALID — диагностики валидатора
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func fail(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

type spec struct {
	right auth.Right
	apply func(doc map[string]any, raw json.RawMessage, gen ir.IDGenerator) (Result, error)
}

var registry = map[string]spec{
	NodeInsert:       {auth.DesignCompose, applyInsert},
	NodeRemove:       {auth.DesignCompose, applyRemove},
	NodeMove:         {auth.DesignCompose, applyMove},
	NodeSetProps:     {auth.DesignCompose, mapSetter("props")},
	NodeSetDesign:    {auth.DesignCompose, mapSetter("design")},
	NodeSetBinding:   {auth.DesignCompose, applySetBinding},
	NodeSetCondition: {auth.DesignCompose, fieldSetter("when")},
	NodeSetBehavior:  {auth.BehaviorUse, applySetBehavior},
	NodeRename:       {auth.DesignCompose, fieldSetter("name")},
}

// Types — поддерживаемые типы операций над документом.
func Types() []string {
	out := make([]string, 0, len(registry))
	for t := range registry {
		out = append(out, t)
	}
	slices.Sort(out)
	return out
}

// Right — право, необходимое для операции; false — тип неизвестен.
func Right(opType string) (auth.Right, bool) {
	s, ok := registry[opType]
	return s.right, ok
}

// Apply применяет операцию к doc на месте. При ошибке doc может быть изменён частично —
// вызывающий применяет операцию к копии (Clone) и отбрасывает её при ошибке.
func Apply(doc map[string]any, op Op, gen ir.IDGenerator) (Result, error) {
	s, ok := registry[op.Type]
	if !ok {
		return Result{}, fail("OPERATION_UNKNOWN", "неизвестный тип операции %q", op.Type)
	}
	if gen == nil {
		gen = ir.GenerateNodeID
	}
	res, err := s.apply(doc, op.Payload, gen)
	if err != nil {
		return Result{}, err
	}
	if v := ir.ValidateDocument(doc); !v.Valid {
		return Result{}, &Error{Code: "OPERATION_INVALID", Message: "документ после операции не проходит валидацию", Diagnostics: v.Diagnostics}
	}
	return res, nil
}

// Clone — глубокая копия документа (значения из разбора JSON).
func Clone(doc map[string]any) map[string]any {
	return cloneValue(doc).(map[string]any)
}

func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = cloneValue(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = cloneValue(val)
		}
		return out
	}
	return v
}

// decode разбирает payload строго: неизвестные поля и лишние данные — ошибка.
func decode(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return fail("PAYLOAD_INVALID", "payload операции: %v", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fail("PAYLOAD_INVALID", "лишние данные после payload операции")
	}
	return nil
}

func mustRaw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// fromJSON приводит значение к виду, в котором его даёт разбор JSON (json.Number и т. п.).
func fromJSON(raw json.RawMessage) any {
	if raw == nil {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		panic(err) // raw уже прошёл разбор payload
	}
	return v
}

// --- Доступ к дереву ---------------------------------------------------------------

func nodes(doc map[string]any) map[string]any {
	n, _ := doc["nodes"].(map[string]any)
	return n
}

func node(doc map[string]any, id string) (map[string]any, error) {
	n, ok := nodes(doc)[id].(map[string]any)
	if !ok {
		return nil, fail("NODE_NOT_FOUND", "узел %s не найден", id)
	}
	return n, nil
}

// location — место узла в родителе.
type location struct {
	ParentID string `json:"parentId"`
	Slot     string `json:"slot,omitempty"`
	Index    int    `json:"index"`
}

// list возвращает список детей (slot == "") или слота узла.
func list(n map[string]any, slot string) []any {
	if slot == "" {
		l, _ := n["children"].([]any)
		return l
	}
	slots, _ := n["slots"].(map[string]any)
	l, _ := slots[slot].([]any)
	return l
}

// setList записывает список; пустой список удаляется вместе с пустым объектом slots.
func setList(n map[string]any, slot string, l []any) {
	if slot == "" {
		if len(l) == 0 {
			delete(n, "children")
		} else {
			n["children"] = l
		}
		return
	}
	slots, _ := n["slots"].(map[string]any)
	if slots == nil {
		slots = map[string]any{}
	}
	if len(l) == 0 {
		delete(slots, slot)
	} else {
		slots[slot] = l
	}
	if len(slots) == 0 {
		delete(n, "slots")
	} else {
		n["slots"] = slots
	}
}

// locate находит родителя узла; ok == false — узел корневой или сирота.
func locate(doc map[string]any, id string) (location, bool) {
	for pid, raw := range nodes(doc) {
		p, _ := raw.(map[string]any)
		if i := slices.Index(list(p, ""), any(id)); i >= 0 {
			return location{ParentID: pid, Index: i}, true
		}
		slots, _ := p["slots"].(map[string]any)
		for name := range slots {
			if i := slices.Index(list(p, name), any(id)); i >= 0 {
				return location{ParentID: pid, Slot: name, Index: i}, true
			}
		}
	}
	return location{}, false
}

// subtreeIDs — ID узла и всех его потомков.
func subtreeIDs(doc map[string]any, id string) []string {
	out := []string{id}
	n, _ := nodes(doc)[id].(map[string]any)
	refs := slices.Clone(list(n, ""))
	slots, _ := n["slots"].(map[string]any)
	for _, name := range sortedKeys(slots) {
		refs = append(refs, list(n, name)...)
	}
	for _, c := range refs {
		if cid, ok := c.(string); ok {
			out = append(out, subtreeIDs(doc, cid)...)
		}
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func insertAt(l []any, index int, v any) []any {
	return slices.Insert(slices.Clone(l), index, v)
}

func checkIndex(index *int, length int) (int, error) {
	if index == nil {
		return length, nil
	}
	if *index < 0 || *index > length {
		return 0, fail("INDEX_OUT_OF_RANGE", "индекс %d вне диапазона 0…%d", *index, length)
	}
	return *index, nil
}

// --- node.insert / node.remove / node.move -----------------------------------------

type insertPayload struct {
	ParentID string         `json:"parentId"`
	Slot     string         `json:"slot,omitempty"`
	Index    *int           `json:"index,omitempty"`
	Subtree  map[string]any `json:"subtree"`
}

func applyInsert(doc map[string]any, raw json.RawMessage, gen ir.IDGenerator) (Result, error) {
	var p insertPayload
	if err := decode(raw, &p); err != nil {
		return Result{}, err
	}
	if p.Subtree == nil {
		return Result{}, fail("PAYLOAD_INVALID", "subtree обязателен")
	}
	parent, err := node(doc, p.ParentID)
	if err != nil {
		return Result{}, err
	}
	index, err := checkIndex(p.Index, len(list(parent, p.Slot)))
	if err != nil {
		return Result{}, err
	}

	// Нормализуем поддерево как отдельный документ, не допуская совпадения ID с документом.
	all := nodes(doc)
	taken := make(map[string]bool, len(all))
	for id := range all {
		taken[id] = true
	}
	flat, err := ir.Normalize(map[string]any{"root": p.Subtree}, func(t map[string]bool) string {
		for id := range t {
			taken[id] = true
		}
		return gen(taken)
	})
	if err != nil {
		var nerr *ir.NormalizeError
		errors.As(err, &nerr)
		return Result{}, &Error{Code: "PAYLOAD_INVALID", Message: err.Error(), Diagnostics: nerr.Diagnostics}
	}
	inserted := flat["nodes"].(map[string]any)
	ids := sortedKeys(inserted)
	for _, id := range ids {
		if _, exists := all[id]; exists {
			return Result{}, fail("NODE_ID_DUPLICATE", "узел с ID %s уже есть в документе", id)
		}
	}
	for _, id := range ids {
		all[id] = inserted[id]
	}
	rootID := flat["root"].(string)
	setList(parent, p.Slot, insertAt(list(parent, p.Slot), index, rootID))
	withIDs, err := ir.ToNested(flat)
	if err != nil {
		return Result{}, err
	}

	return Result{
		After:   map[string]any{"nodeId": rootID, "nodeIds": ids, "parentId": p.ParentID, "slot": p.Slot, "index": index},
		Inverse: Op{Type: NodeRemove, Payload: mustRaw(map[string]any{"nodeId": rootID})},
		Payload: mustRaw(insertPayload{ParentID: p.ParentID, Slot: p.Slot, Index: p.Index, Subtree: withIDs["root"].(map[string]any)}),
	}, nil
}

type nodeRef struct {
	NodeID string `json:"nodeId"`
}

func applyRemove(doc map[string]any, raw json.RawMessage, _ ir.IDGenerator) (Result, error) {
	var p nodeRef
	if err := decode(raw, &p); err != nil {
		return Result{}, err
	}
	if _, err := node(doc, p.NodeID); err != nil {
		return Result{}, err
	}
	loc, ok := locate(doc, p.NodeID)
	if !ok {
		return Result{}, fail("ROOT_IMMUTABLE", "корневой узел нельзя удалить")
	}
	nested, err := ir.ToNested(map[string]any{"root": p.NodeID, "nodes": nodes(doc)})
	if err != nil {
		return Result{}, err
	}
	subtree := nested["root"]

	parent, _ := node(doc, loc.ParentID)
	setList(parent, loc.Slot, slices.Delete(slices.Clone(list(parent, loc.Slot)), loc.Index, loc.Index+1))
	for _, id := range subtreeIDs(doc, p.NodeID) {
		delete(nodes(doc), id)
	}

	insert := insertPayload{ParentID: loc.ParentID, Slot: loc.Slot, Index: &loc.Index, Subtree: subtree.(map[string]any)}
	return Result{
		Before:  map[string]any{"parentId": loc.ParentID, "slot": loc.Slot, "index": loc.Index, "subtree": subtree},
		Inverse: Op{Type: NodeInsert, Payload: mustRaw(insert)},
	}, nil
}

type movePayload struct {
	NodeID   string `json:"nodeId"`
	ParentID string `json:"parentId"`
	Slot     string `json:"slot,omitempty"`
	Index    *int   `json:"index,omitempty"`
}

func applyMove(doc map[string]any, raw json.RawMessage, _ ir.IDGenerator) (Result, error) {
	var p movePayload
	if err := decode(raw, &p); err != nil {
		return Result{}, err
	}
	if _, err := node(doc, p.NodeID); err != nil {
		return Result{}, err
	}
	target, err := node(doc, p.ParentID)
	if err != nil {
		return Result{}, err
	}
	from, ok := locate(doc, p.NodeID)
	if !ok {
		return Result{}, fail("ROOT_IMMUTABLE", "корневой узел нельзя переместить")
	}
	if slices.Contains(subtreeIDs(doc, p.NodeID), p.ParentID) {
		return Result{}, fail("MOVE_INTO_DESCENDANT", "узел нельзя переместить внутрь самого себя")
	}
	source, _ := node(doc, from.ParentID)
	setList(source, from.Slot, slices.Delete(slices.Clone(list(source, from.Slot)), from.Index, from.Index+1))
	// Индекс — позиция в целевом списке после изъятия узла.
	index, err := checkIndex(p.Index, len(list(target, p.Slot)))
	if err != nil {
		return Result{}, err
	}
	setList(target, p.Slot, insertAt(list(target, p.Slot), index, p.NodeID))

	back := movePayload{NodeID: p.NodeID, ParentID: from.ParentID, Slot: from.Slot, Index: &from.Index}
	return Result{
		Before:  from,
		After:   location{ParentID: p.ParentID, Slot: p.Slot, Index: index},
		Inverse: Op{Type: NodeMove, Payload: mustRaw(back)},
	}, nil
}

// --- Изменение полей узла ------------------------------------------------------------

type mapPayload struct {
	NodeID string                     `json:"nodeId"`
	Set    map[string]json.RawMessage `json:"set,omitempty"`
	Unset  []string                   `json:"unset,omitempty"`
}

// mapSetter — операция set/unset над полем-объектом узла (props, design).
func mapSetter(field string) func(map[string]any, json.RawMessage, ir.IDGenerator) (Result, error) {
	return func(doc map[string]any, raw json.RawMessage, _ ir.IDGenerator) (Result, error) {
		var p mapPayload
		if err := decode(raw, &p); err != nil {
			return Result{}, err
		}
		if len(p.Set) == 0 && len(p.Unset) == 0 {
			return Result{}, fail("PAYLOAD_INVALID", "нужно хотя бы одно поле в set или unset")
		}
		for _, k := range p.Unset {
			if _, both := p.Set[k]; both {
				return Result{}, fail("PAYLOAD_INVALID", "ключ %q одновременно в set и unset", k)
			}
		}
		n, err := node(doc, p.NodeID)
		if err != nil {
			return Result{}, err
		}
		m, _ := n[field].(map[string]any)
		if m == nil {
			m = map[string]any{}
		}

		// before: прежние значения затронутых ключей и ключи, которых не было.
		prevSet := map[string]any{}
		var prevUnset []string
		touched := slices.Sorted(func(yield func(string) bool) {
			for k := range p.Set {
				if !yield(k) {
					return
				}
			}
			for _, k := range p.Unset {
				if !yield(k) {
					return
				}
			}
		})
		for _, k := range slices.Compact(touched) {
			if v, ok := m[k]; ok {
				prevSet[k] = v
			} else {
				prevUnset = append(prevUnset, k)
			}
		}
		for k, v := range p.Set {
			m[k] = fromJSON(v)
		}
		for _, k := range p.Unset {
			delete(m, k)
		}
		if len(m) == 0 {
			delete(n, field)
		} else {
			n[field] = m
		}

		inverse := map[string]any{"nodeId": p.NodeID, "set": prevSet, "unset": prevUnset}
		after := map[string]any{}
		for k := range p.Set {
			after[k] = m[k]
		}
		return Result{
			Before:  map[string]any{"set": prevSet, "unset": prevUnset},
			After:   after,
			Inverse: Op{Type: opTypeForField(field), Payload: mustRaw(inverse)},
		}, nil
	}
}

func opTypeForField(field string) string {
	switch field {
	case "zone":
		return NodeSetZone
	case "locked":
		return NodeSetLocked
	case "props":
		return NodeSetProps
	case "design":
		return NodeSetDesign
	case "when":
		return NodeSetCondition
	}
	return NodeRename
}

// fieldSetter — замена или удаление (null) скалярного поля узла (name, when).
// Payload: {"nodeId", "<поле>": значение | null}; для name — "name", для when — "when".
func fieldSetter(field string) func(map[string]any, json.RawMessage, ir.IDGenerator) (Result, error) {
	return func(doc map[string]any, raw json.RawMessage, _ ir.IDGenerator) (Result, error) {
		var generic map[string]json.RawMessage
		if err := decode(raw, &generic); err != nil {
			return Result{}, err
		}
		value, hasValue := generic[field]
		var nodeID string
		if !hasValue || len(generic) != 2 || json.Unmarshal(generic["nodeId"], &nodeID) != nil {
			return Result{}, fail("PAYLOAD_INVALID", "payload: {\"nodeId\": строка, %q: значение | null}", field)
		}
		n, err := node(doc, nodeID)
		if err != nil {
			return Result{}, err
		}
		prev, had := n[field]
		if string(value) == "null" {
			delete(n, field)
		} else {
			n[field] = fromJSON(value)
		}
		var before any
		if had {
			before = prev
		}
		return Result{
			Before:  map[string]any{field: before},
			After:   map[string]any{field: n[field]},
			Inverse: Op{Type: opTypeForField(field), Payload: mustRaw(map[string]any{"nodeId": nodeID, field: before})},
		}, nil
	}
}

type bindingPayload struct {
	NodeID  string          `json:"nodeId"`
	Prop    string          `json:"prop"`
	Binding json.RawMessage `json:"binding"`
}

func applySetBinding(doc map[string]any, raw json.RawMessage, _ ir.IDGenerator) (Result, error) {
	var p bindingPayload
	if err := decode(raw, &p); err != nil {
		return Result{}, err
	}
	return setEntry(doc, p.NodeID, "bindings", p.Prop, p.Binding, NodeSetBinding, func(prev any) map[string]any {
		return map[string]any{"nodeId": p.NodeID, "prop": p.Prop, "binding": prev}
	})
}

type behaviorPayload struct {
	NodeID   string          `json:"nodeId"`
	Event    string          `json:"event"`
	Handlers json.RawMessage `json:"handlers"`
}

func applySetBehavior(doc map[string]any, raw json.RawMessage, _ ir.IDGenerator) (Result, error) {
	var p behaviorPayload
	if err := decode(raw, &p); err != nil {
		return Result{}, err
	}
	return setEntry(doc, p.NodeID, "on", p.Event, p.Handlers, NodeSetBehavior, func(prev any) map[string]any {
		return map[string]any{"nodeId": p.NodeID, "event": p.Event, "handlers": prev}
	})
}

// setEntry задаёт или удаляет (null/отсутствие) один ключ в поле-объекте узла.
func setEntry(doc map[string]any, nodeID, field, key string, value json.RawMessage, opType string,
	inverse func(prev any) map[string]any) (Result, error) {
	if key == "" {
		return Result{}, fail("PAYLOAD_INVALID", "не указан ключ в %s", field)
	}
	if value == nil {
		return Result{}, fail("PAYLOAD_INVALID", "значение обязательно (null — удалить)")
	}
	n, err := node(doc, nodeID)
	if err != nil {
		return Result{}, err
	}
	m, _ := n[field].(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	prev, had := m[key]
	if string(value) == "null" {
		delete(m, key)
	} else {
		m[key] = fromJSON(value)
	}
	if len(m) == 0 {
		delete(n, field)
	} else {
		n[field] = m
	}
	var before any
	if had {
		before = prev
	}
	return Result{
		Before:  map[string]any{key: before},
		After:   map[string]any{key: m[key]},
		Inverse: Op{Type: opType, Payload: mustRaw(inverse(before))},
	}, nil
}
