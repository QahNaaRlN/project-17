package ops

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
)

// Операции извлечения компонента (docs/studio-component-extract.md §4).
const (
	ComponentExtract       = "component.extract"
	ComponentRevertExtract = "component.revertExtract" // только inverse; клиент не передаёт
)

const (
	maxExtractInputs    = 32
	maxExtractSlots     = 8
	maxComposedPerPage  = 64
	maxComponentName    = 80
	maxComponentSummary = 500
)

var (
	identifierPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
	scopePattern      = regexp.MustCompile(`^\$([A-Za-z]+)(?:\.([A-Za-z0-9_]+))?`)
	reservedNames     = []string{"constructor", "prototype", "__proto__"}
	newInputTypes     = []string{"string", "text", "number", "boolean", "enum", "url", "link", "asset"}
	modeNames         = []string{"STRICT", "SYSTEM", "FREE", "CODE"}
)

// ExtractSource — свойство узла поддерева, значение которого становится входом.
type ExtractSource struct {
	NodeID string `json:"nodeId"`
	Prop   string `json:"prop"`
}

// ExtractInput — вход компонента: из свойств поддерева (from) и/или по объявлению (def).
type ExtractInput struct {
	From     []ExtractSource `json:"from,omitempty"`
	Def      map[string]any  `json:"def,omitempty"`
	Required *bool           `json:"required,omitempty"`
}

// SlotPosition — позиция пустого слота в списке children контейнера поддерева.
type SlotPosition struct {
	ParentID string `json:"parentId"`
	Index    int    `json:"index"`
}

// ExtractSlot — слот: непрерывный диапазон соседних узлов либо пустой слот с позицией.
type ExtractSlot struct {
	NodeIDs      []string      `json:"nodeIds"`
	At           *SlotPosition `json:"at,omitempty"`
	AllowedTypes []string      `json:"allowedTypes,omitempty"`
	Min          *int          `json:"min,omitempty"`
	Max          *int          `json:"max,omitempty"`
}

// ExtractPayload — payload component.extract. componentId, instanceNodeId и slotNodeIds
// назначает сервер; они есть только в каноническом payload журнала (redo, rebase).
type ExtractPayload struct {
	NodeID         string                  `json:"nodeId"`
	Name           string                  `json:"name"`
	Description    *string                 `json:"description,omitempty"`
	Inputs         map[string]ExtractInput `json:"inputs,omitempty"`
	Slots          map[string]ExtractSlot  `json:"slots,omitempty"`
	LocalContent   map[string]string       `json:"localContent,omitempty"`
	ComponentID    string                  `json:"componentId,omitempty"`
	InstanceNodeID string                  `json:"instanceNodeId,omitempty"`
	SlotNodeIDs    map[string]string       `json:"slotNodeIds,omitempty"`
}

// Canonical — у payload есть назначенные сервером ID.
func (p ExtractPayload) Canonical() bool { return p.ComponentID != "" }

// ExtractEnv — данные, которых нет в документе. Для канонического payload (redo, rebase)
// PropTypes не нужен: определения входов уже записаны в payload.
type ExtractEnv struct {
	// PropTypes возвращает объявления свойств типа узла (builtin, manifest, вход Composed).
	PropTypes func(node map[string]any) (map[string]any, bool)
	// Container сообщает, принимает ли тип узла children.
	Container func(node map[string]any) bool
	// SourceTypes — list/object-контракты результатов источников активного manifest.
	SourceTypes []map[string]any
	// Project — настройки проекта (defaultMode) для эффективного режима места.
	Project     map[string]any
	ComponentID string
	Gen         ir.IDGenerator
}

// ExtractResult — итог извлечения над страницей.
type ExtractResult struct {
	Component map[string]any
	Payload   ExtractPayload // канонический
	Before    map[string]any
	After     map[string]any
	Inverse   Op
	Warnings  []ir.Diagnostic
}

// DecodeExtract строго разбирает payload component.extract.
func DecodeExtract(raw json.RawMessage) (ExtractPayload, error) {
	var p ExtractPayload
	if err := decode(raw, &p); err != nil {
		return p, err
	}
	return p, nil
}

func extractFail(code, pointer, node, format string, args ...any) *Error {
	msg := fmt.Sprintf(format, args...)
	return &Error{Code: code, Message: msg, Diagnostics: []ir.Diagnostic{{
		Code: ir.Code(code), Severity: ir.SeverityError, Pointer: pointer, NodeID: node, Message: msg,
	}}}
}

// extraction — разбор поддерева: какие узлы уходят в компонент, какие остаются слотами.
type extraction struct {
	page     map[string]any
	root     string
	loc      location
	part     map[string]bool // узлы, которые переходят в компонент
	slotted  map[string]bool // узлы диапазонов слотов и их потомки (остаются на странице)
	parentOf map[string]string
}

func analyse(page map[string]any, rootID string) (*extraction, error) {
	if page["kind"] != "page" {
		return nil, extractFail("COMPONENT_EXTRACT_TARGET_KIND", "/kind", "", "извлечение доступно только для страницы")
	}
	if _, err := node(page, rootID); err != nil {
		return nil, err
	}
	if page["root"] == rootID {
		return nil, extractFail("COMPONENT_EXTRACT_ROOT_DENIED", ir.Pointer("nodes", rootID), rootID, "корень страницы нельзя сохранить как компонент")
	}
	loc, ok := locate(page, rootID)
	if !ok {
		return nil, fail("NODE_NOT_FOUND", "узел %s не найден в дереве", rootID)
	}
	x := &extraction{page: page, root: rootID, loc: loc, part: map[string]bool{}, slotted: map[string]bool{}, parentOf: map[string]string{}}
	for _, id := range subtreeIDs(page, rootID) {
		x.part[id] = true
	}
	for id := range x.part {
		n, _ := nodes(page)[id].(map[string]any)
		for _, c := range children(n) {
			x.parentOf[c] = id
		}
	}
	return x, nil
}

func children(n map[string]any) []string {
	var out []string
	for _, v := range list(n, "") {
		out = append(out, fmt.Sprint(v))
	}
	slots, _ := n["slots"].(map[string]any)
	for _, name := range sortedKeys(slots) {
		for _, v := range list(n, name) {
			out = append(out, fmt.Sprint(v))
		}
	}
	return out
}

func (x *extraction) node(id string) map[string]any {
	n, _ := nodes(x.page)[id].(map[string]any)
	return n
}

// slotRange проверяет диапазон соседей и возвращает родителя, список и начальный индекс.
func (x *extraction) slotRange(name string, ids []string) (string, string, int, error) {
	ptr := "/payload/slots/" + name
	parent, ok := x.parentOf[ids[0]]
	if !ok || !x.part[ids[0]] || ids[0] == x.root {
		return "", "", 0, extractFail("COMPONENT_EXTRACT_SLOT_INVALID", ptr, ids[0], "узел слота должен быть внутри поддерева, но не его корнем")
	}
	p := x.node(parent)
	listName := ""
	start := slices.Index(list(p, ""), any(ids[0]))
	if start < 0 {
		slots, _ := p["slots"].(map[string]any)
		for _, s := range sortedKeys(slots) {
			if i := slices.Index(list(p, s), any(ids[0])); i >= 0 {
				listName, start = s, i
			}
		}
	}
	l := list(p, listName)
	if start+len(ids) > len(l) {
		return "", "", 0, extractFail("COMPONENT_EXTRACT_SLOT_INVALID", ptr, ids[0], "узлы слота должны идти подряд в одном родителе")
	}
	for i, id := range ids {
		if fmt.Sprint(l[start+i]) != id {
			return "", "", 0, extractFail("COMPONENT_EXTRACT_SLOT_INVALID", ptr, id, "узлы слота должны идти подряд в одном родителе")
		}
	}
	return parent, listName, start, nil
}

// Extract применяет component.extract к странице на месте и возвращает документ компонента.
func Extract(page map[string]any, p ExtractPayload, env ExtractEnv) (ExtractResult, error) {
	if err := checkHeader(p); err != nil {
		return ExtractResult{}, err
	}
	original := Clone(page)
	x, err := analyse(page, p.NodeID)
	if err != nil {
		return ExtractResult{}, err
	}
	if err := x.protected(); err != nil {
		return ExtractResult{}, err
	}
	if p.Canonical() && (p.InstanceNodeID == "" || len(p.SlotNodeIDs) != len(p.Slots)) {
		return ExtractResult{}, fail("PAYLOAD_INVALID", "канонический payload неполон")
	}

	// Слоты: узлы диапазонов остаются на странице.
	type slotPlan struct {
		name, parent, listName string
		start                  int
		ids                    []string
		at                     *SlotPosition
	}
	var slotPlans []slotPlan
	for _, name := range sortedKeys(anyMap(p.Slots)) {
		s := p.Slots[name]
		ptr := "/payload/slots/" + name
		if !identifierPattern.MatchString(name) || slices.Contains(reservedNames, name) {
			return ExtractResult{}, extractFail("COMPONENT_EXTRACT_SLOT_INVALID", ptr, "", "имя слота должно быть идентификатором")
		}
		if s.Max != nil && *s.Max < 1 || s.Min != nil && *s.Min < 0 || s.Min != nil && s.Max != nil && *s.Min > *s.Max {
			return ExtractResult{}, extractFail("COMPONENT_EXTRACT_SLOT_INVALID", ptr, "", "min/max слота противоречат друг другу")
		}
		sp := slotPlan{name: name, ids: s.NodeIDs}
		if len(s.NodeIDs) == 0 {
			if s.At == nil || (s.Min != nil && *s.Min > 0) {
				return ExtractResult{}, extractFail("COMPONENT_EXTRACT_SLOT_INVALID", ptr, "", "пустой слот требует at и min: 0")
			}
			parent := x.node(s.At.ParentID)
			if !x.part[s.At.ParentID] || parent == nil || env.Container != nil && !env.Container(parent) || s.At.Index < 0 || s.At.Index > len(list(parent, "")) {
				return ExtractResult{}, extractFail("COMPONENT_EXTRACT_SLOT_INVALID", ptr+"/at", s.At.ParentID, "позиция пустого слота вне контейнера поддерева")
			}
			sp.at, sp.parent = s.At, s.At.ParentID
		} else {
			if s.At != nil {
				return ExtractResult{}, extractFail("COMPONENT_EXTRACT_SLOT_INVALID", ptr+"/at", "", "at допустим только для пустого слота")
			}
			sp.parent, sp.listName, sp.start, err = x.slotRange(name, s.NodeIDs)
			if err != nil {
				return ExtractResult{}, err
			}
			n := len(s.NodeIDs)
			if s.Min != nil && n < *s.Min || s.Max != nil && n > *s.Max {
				return ExtractResult{}, extractFail("COMPONENT_EXTRACT_SLOT_INVALID", ptr, "", "число узлов слота вне min/max")
			}
			for _, id := range s.NodeIDs {
				for _, sub := range subtreeIDs(page, id) {
					if x.slotted[sub] {
						return ExtractResult{}, extractFail("COMPONENT_EXTRACT_SLOT_INVALID", ptr, sub, "слоты пересекаются")
					}
					x.slotted[sub] = true
				}
			}
		}
		slotPlans = append(slotPlans, sp)
	}
	if len(slotPlans) > maxExtractSlots {
		return ExtractResult{}, fail("PAYLOAD_INVALID", "не больше %d слотов", maxExtractSlots)
	}
	for _, sp := range slotPlans {
		if sp.at != nil && x.slotted[sp.parent] {
			return ExtractResult{}, extractFail("COMPONENT_EXTRACT_SLOT_INVALID", "/payload/slots/"+sp.name+"/at", sp.parent, "позиция пустого слота внутри другого слота")
		}
	}
	component := map[string]bool{}
	for id := range x.part {
		if !x.slotted[id] {
			component[id] = true
		}
	}

	// Входы.
	if len(p.Inputs) > maxExtractInputs {
		return ExtractResult{}, fail("PAYLOAD_INVALID", "не больше %d входов", maxExtractInputs)
	}
	inputs := map[string]any{}
	instanceProps := map[string]any{}
	instanceBindings := map[string]any{}
	sourceOf := map[string]string{} // nodeId/kind/prop → вход
	canonicalInputs := map[string]ExtractInput{}
	for _, name := range sortedKeys(anyMap(p.Inputs)) {
		in := p.Inputs[name]
		ptr := "/payload/inputs/" + name
		if !identifierPattern.MatchString(name) || slices.Contains(reservedNames, name) {
			return ExtractResult{}, extractFail("COMPONENT_EXTRACT_INPUT_INVALID", ptr, "", "имя входа должно быть идентификатором")
		}
		def, value, kind, err := x.input(name, in, component, env, p.Canonical())
		if err != nil {
			return ExtractResult{}, err
		}
		for _, src := range in.From {
			key := src.NodeID + "\x00" + src.Prop
			if _, dup := sourceOf[key]; dup {
				return ExtractResult{}, extractFail("COMPONENT_EXTRACT_INPUT_INVALID", ptr+"/from", src.NodeID, "свойство уже стало другим входом")
			}
			sourceOf[key] = name
		}
		if req, _ := def["required"].(bool); req && kind == "" {
			return ExtractResult{}, extractFail("COMPONENT_EXTRACT_INPUT_INVALID", ptr, "", "обязательный вход без значения на экземпляре (IR-061)")
		}
		inputs[name] = def
		switch kind {
		case "props":
			instanceProps[name] = value
		case "bindings":
			instanceBindings[name] = value
		}
		canonicalInputs[name] = ExtractInput{From: in.From, Def: def}
	}

	// Внешние области, $local и ссылки через границу.
	localDecisions, err := x.dependencies(component, sourceOf, p.LocalContent)
	if err != nil {
		return ExtractResult{}, err
	}

	// ID экземпляра и узлов Slot.
	taken := map[string]bool{}
	for id := range nodes(page) {
		taken[id] = true
	}
	gen := env.Gen
	if gen == nil {
		gen = ir.GenerateNodeID
	}
	instanceID := p.InstanceNodeID
	if instanceID == "" {
		instanceID = gen(taken)
	} else if !ir.IsNodeID(instanceID) || taken[instanceID] && !x.part[instanceID] {
		return ExtractResult{}, fail("NODE_ID_DUPLICATE", "ID экземпляра %s занят", instanceID)
	}
	taken[instanceID] = true
	slotNodeIDs := map[string]string{}
	for _, sp := range slotPlans {
		id := p.SlotNodeIDs[sp.name]
		if id == "" {
			id = gen(taken)
		} else if !ir.IsNodeID(id) || taken[id] {
			return ExtractResult{}, fail("NODE_ID_DUPLICATE", "ID узла Slot %s занят", id)
		}
		taken[id] = true
		slotNodeIDs[sp.name] = id
	}

	// Документ компонента.
	before := map[string]any{"parentId": x.loc.ParentID, "slot": x.loc.Slot, "index": x.loc.Index, "nodes": map[string]any{}}
	compNodes := map[string]any{}
	for _, id := range sortedKeys(boolMap(x.part)) {
		before["nodes"].(map[string]any)[id] = cloneValue(nodes(page)[id])
		if component[id] {
			compNodes[id] = cloneValue(nodes(page)[id])
		}
	}
	// Входы: свойства источников заменяются на $props.
	for key, name := range sourceOf {
		nodeID, prop, _ := strings.Cut(key, "\x00")
		n := compNodes[nodeID].(map[string]any)
		deleteEntry(n, "props", prop)
		setEntryValue(n, "bindings", prop, "$props."+name)
	}
	// Слоты: диапазоны заменяются узлом Slot, пустые слоты вставляются по позиции.
	instanceSlots := map[string]any{}
	slotDefs := map[string]any{}
	type edit struct {
		start, length int
		id            string
		order         string
	}
	edits := map[string][]edit{} // parent + "\x00" + list → правки исходного списка
	for _, sp := range slotPlans {
		s := p.Slots[sp.name]
		def := map[string]any{}
		allowed := s.AllowedTypes
		if allowed == nil && len(sp.ids) > 0 {
			for _, id := range sp.ids {
				t := fmt.Sprint(x.node(id)["type"])
				if !slices.Contains(allowed, t) {
					allowed = append(allowed, t)
				}
			}
			slices.Sort(allowed)
		}
		if allowed != nil {
			def["allowedTypes"] = stringsToAny(allowed)
		}
		if s.Min != nil {
			def["min"] = json.Number(fmt.Sprint(*s.Min))
		}
		if s.Max != nil {
			def["max"] = json.Number(fmt.Sprint(*s.Max))
		}
		slotDefs[sp.name] = def
		slotID := slotNodeIDs[sp.name]
		compNodes[slotID] = map[string]any{"id": slotID, "type": "Slot", "props": map[string]any{"name": sp.name}}
		key := sp.parent + "\x00" + sp.listName
		if sp.at != nil {
			edits[key] = append(edits[key], edit{start: sp.at.Index, id: slotID, order: "0" + sp.name})
		} else {
			edits[key] = append(edits[key], edit{start: sp.start, length: len(sp.ids), id: slotID, order: "1" + sp.name})
			instanceSlots[sp.name] = stringsToAny(sp.ids)
		}
		// Проверка allowedTypes против выбранных узлов.
		if s.AllowedTypes != nil {
			for _, id := range sp.ids {
				if !slices.Contains(s.AllowedTypes, fmt.Sprint(x.node(id)["type"])) {
					return ExtractResult{}, extractFail("COMPONENT_EXTRACT_SLOT_INVALID", "/payload/slots/"+sp.name+"/allowedTypes", id, "тип узла слота не разрешён")
				}
			}
		}
	}
	for key, list := range edits {
		parentID, listName, _ := strings.Cut(key, "\x00")
		n := compNodes[parentID].(map[string]any)
		slices.SortFunc(list, func(a, b edit) int {
			if a.start != b.start {
				return a.start - b.start
			}
			return strings.Compare(a.order, b.order)
		})
		current := slices.Clone(listOf(n, listName))
		var next []any
		pos := 0
		for _, e := range list {
			for pos < e.start {
				next = append(next, current[pos])
				pos++
			}
			next = append(next, e.id)
			pos += e.length
		}
		next = append(next, current[pos:]...)
		setList(n, listName, next)
	}

	effective := effectiveMode(page, x.loc.ParentID, env.Project)
	doc := map[string]any{
		"irVersion": "1.0",
		"kind":      "component",
		"root":      x.root,
		"nodes":     compNodes,
		"policy":    map[string]any{"mode": effective},
		"meta":      map[string]any{"title": strings.TrimSpace(p.Name)},
	}
	if p.Description != nil && *p.Description != "" {
		doc["meta"].(map[string]any)["description"] = *p.Description
	}
	if len(inputs) > 0 {
		doc["inputs"] = inputs
	}
	if len(slotDefs) > 0 {
		doc["slotDefs"] = slotDefs
	}
	pageLocal, _ := page["localContent"].(map[string]any)
	compLocal := map[string]any{}
	movedLocal := map[string]any{}
	for key, decision := range localDecisions {
		if v, ok := pageLocal[key]; ok {
			compLocal[key] = cloneValue(v)
			if decision == "move" {
				movedLocal[key] = cloneValue(v)
			}
		}
	}
	if len(compLocal) > 0 {
		doc["localContent"] = compLocal
	}
	before["localContent"] = movedLocal

	// Страница: поддерево заменяется экземпляром.
	componentID := p.ComponentID
	if componentID == "" {
		componentID = env.ComponentID
	}
	instance := map[string]any{"id": instanceID, "type": "Composed", "ref": map[string]any{"component": componentID, "version": "live"}}
	if len(instanceProps) > 0 {
		instance["props"] = instanceProps
	}
	if len(instanceBindings) > 0 {
		instance["bindings"] = instanceBindings
	}
	if len(instanceSlots) > 0 {
		instance["slots"] = instanceSlots
	}
	for id := range component {
		delete(nodes(page), id)
	}
	nodes(page)[instanceID] = instance
	parent, _ := node(page, x.loc.ParentID)
	l := slices.Clone(list(parent, x.loc.Slot))
	l[x.loc.Index] = instanceID
	setList(parent, x.loc.Slot, l)
	for key := range movedLocal {
		delete(pageLocal, key)
	}
	if pageLocal != nil && len(pageLocal) == 0 {
		delete(page, "localContent")
	}

	// Ссылки $local, нужные странице, нельзя переносить (move).
	used := localRefs(page)
	for key := range movedLocal {
		if used[key] {
			copyBack(page, original)
			return ExtractResult{}, extractFail("COMPONENT_EXTRACT_LOCAL_IN_USE", "/payload/localContent/"+key, "", "ключ %s ещё нужен странице", key)
		}
	}
	composed := 0
	for _, raw := range nodes(page) {
		if n, _ := raw.(map[string]any); n["type"] == "Composed" {
			composed++
		}
	}
	if composed > maxComposedPerPage {
		copyBack(page, original)
		return ExtractResult{}, fail("LIMIT_EXCEEDED", "не больше %d экземпляров Composed на странице", maxComposedPerPage)
	}
	// Привести к виду разбора JSON (json.Number), как у документов из хранилища.
	doc = fromJSON(mustRaw(doc)).(map[string]any)
	if v := ir.ValidateDocument(doc); !v.Valid {
		copyBack(page, original)
		return ExtractResult{}, &Error{Code: "OPERATION_INVALID", Message: "компонент не проходит валидацию", Diagnostics: v.Diagnostics}
	}
	if v := ir.ValidateDocument(page); !v.Valid {
		copyBack(page, original)
		return ExtractResult{}, &Error{Code: "OPERATION_INVALID", Message: "страница после извлечения не проходит валидацию", Diagnostics: v.Diagnostics}
	}

	canonical := ExtractPayload{
		NodeID: p.NodeID, Name: strings.TrimSpace(p.Name), Description: p.Description, Inputs: canonicalInputs,
		Slots: p.Slots, LocalContent: map[string]string{}, ComponentID: componentID, InstanceNodeID: instanceID, SlotNodeIDs: slotNodeIDs,
	}
	for key, decision := range localDecisions {
		canonical.LocalContent[key] = decision
	}
	if len(canonical.Inputs) == 0 {
		canonical.Inputs = nil
	}
	if len(canonical.Slots) == 0 {
		canonical.Slots, canonical.SlotNodeIDs = nil, nil
	}
	if len(canonical.LocalContent) == 0 {
		canonical.LocalContent = nil
	}
	revert := map[string]any{"componentId": componentID, "instanceNodeId": instanceID, "instance": cloneValue(instance), "before": cloneValue(before), "redo": canonical}
	var warnings []ir.Diagnostic
	for _, name := range sortedKeys(inputs) {
		// Объявленный вход пока не используется: на странице нет выражений $props.
		if len(p.Inputs[name].From) == 0 {
			warnings = append(warnings, ir.Diagnostic{Code: "COMPONENT_INPUT_UNUSED", Severity: ir.SeverityWarning, Pointer: "/inputs/" + name, Message: "вход пока не используется внутри компонента"})
		}
	}
	return ExtractResult{
		Component: doc,
		Payload:   canonical,
		Before:    fromJSON(mustRaw(before)).(map[string]any),
		After:     map[string]any{"componentId": componentID, "instanceNodeId": instanceID, "inputs": fromJSON(mustRaw(inputs)), "slotDefs": fromJSON(mustRaw(slotDefs))},
		Inverse:   Op{Type: ComponentRevertExtract, Payload: mustRaw(revert)},
		Warnings:  warnings,
	}, nil
}

func checkHeader(p ExtractPayload) error {
	name := strings.TrimSpace(p.Name)
	if name == "" || utf8.RuneCountInString(name) > maxComponentName {
		return fail("PAYLOAD_INVALID", "имя компонента: 1…%d символов", maxComponentName)
	}
	if p.Description != nil && utf8.RuneCountInString(*p.Description) > maxComponentSummary {
		return fail("PAYLOAD_INVALID", "описание компонента: до %d символов", maxComponentSummary)
	}
	if p.NodeID == "" {
		return fail("PAYLOAD_INVALID", "nodeId обязателен")
	}
	for key, decision := range p.LocalContent {
		if decision != "copy" && decision != "move" {
			return fail("PAYLOAD_INVALID", "localContent.%s: copy или move", key)
		}
	}
	return nil
}

func (x *extraction) protected() error {
	for _, id := range sortedKeys(boolMap(x.part)) {
		n := x.node(id)
		if n["zone"] != nil || n["locked"] != nil {
			return extractFail("COMPONENT_EXTRACT_PROTECTED", ir.Pointer("nodes", id, "zone"), id, "поддерево содержит зону или защищённый узел")
		}
		if _, placeholder := n["pending"].(string); placeholder {
			return extractFail("CAPABILITY_PENDING_INVALID", ir.Pointer("nodes", id, "pending"), id, "поддерево содержит заглушку компонента")
		}
		if pendingAction(n["on"]) {
			return extractFail("COMPONENT_EXTRACT_PROTECTED", ir.Pointer("nodes", id, "on"), id, "поддерево содержит заглушку Missing Capability")
		}
	}
	return nil
}

func pendingAction(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if s, ok := val.(string); ok && k == "action" && strings.HasPrefix(s, "pending:") {
				return true
			}
			if pendingAction(val) {
				return true
			}
		}
	case []any:
		for _, val := range t {
			if pendingAction(val) {
				return true
			}
		}
	}
	return false
}

// input вычисляет определение входа и значение для экземпляра.
func (x *extraction) input(name string, in ExtractInput, component map[string]bool, env ExtractEnv, canonical bool) (map[string]any, any, string, error) {
	ptr := "/payload/inputs/" + name
	def := map[string]any{}
	var value any
	kind := ""
	if len(in.From) == 0 && in.Def == nil {
		return nil, nil, "", extractFail("COMPONENT_EXTRACT_INPUT_INVALID", ptr, "", "нужен from или def")
	}
	var derived map[string]any
	for i, src := range in.From {
		n := x.node(src.NodeID)
		if n == nil || !x.part[src.NodeID] {
			return nil, nil, "", fail("NODE_NOT_FOUND", "узел %s не найден в поддереве", src.NodeID)
		}
		if !component[src.NodeID] {
			return nil, nil, "", extractFail("COMPONENT_EXTRACT_SLOT_INVALID", ptr+"/from", src.NodeID, "узел-источник входа находится в слоте")
		}
		if structuralProp(n, src.Prop) {
			return nil, nil, "", extractFail("COMPONENT_EXTRACT_INPUT_INVALID", ptr+"/from", src.NodeID, "свойство %s нельзя сделать входом", src.Prop)
		}
		k, v := "", any(nil)
		if b, ok := asMap(n["bindings"])[src.Prop]; ok {
			k, v = "bindings", b
		} else if lit, ok := asMap(n["props"])[src.Prop]; ok {
			k, v = "props", lit
		}
		if i == 0 {
			kind, value = k, v
		} else if k != kind || string(mustRaw(v)) != string(mustRaw(value)) {
			return nil, nil, "", extractFail("COMPONENT_EXTRACT_INPUT_INCOMPATIBLE", ptr+"/from", src.NodeID, "источники входа различаются значением")
		}
		if !canonical && env.PropTypes != nil {
			props, ok := env.PropTypes(n)
			t, known := props[src.Prop].(map[string]any)
			if !ok || !known {
				return nil, nil, "", extractFail("COMPONENT_EXTRACT_INPUT_INVALID", ptr+"/from", src.NodeID, "свойство %s не объявлено для типа узла", src.Prop)
			}
			t = typeOnly(t)
			if derived == nil {
				derived = t
			} else if !reflect.DeepEqual(fromJSON(mustRaw(derived)), fromJSON(mustRaw(t))) {
				return nil, nil, "", extractFail("COMPONENT_EXTRACT_INPUT_INCOMPATIBLE", ptr+"/from", src.NodeID, "типы источников входа различаются")
			}
		}
	}
	if canonical {
		// Определение уже вычислено при первом применении.
		for k, v := range in.Def {
			def[k] = v
		}
		return def, value, kind, nil
	}
	if derived != nil {
		for k, v := range derived {
			def[k] = v
		}
		for k, v := range in.Def {
			switch k {
			case "required", "default", "description":
				def[k] = v
			case "values":
				allowed, _ := def["values"].([]any)
				subset, ok := v.([]any)
				if !ok || len(subset) == 0 || def["type"] != "enum" {
					return nil, nil, "", extractFail("COMPONENT_EXTRACT_INPUT_INVALID", ptr+"/def/values", "", "values сужают только enum")
				}
				for _, s := range subset {
					if !slices.Contains(allowed, s) {
						return nil, nil, "", extractFail("COMPONENT_EXTRACT_INPUT_INVALID", ptr+"/def/values", "", "расширение типа запрещено")
					}
				}
				def[k] = v
			case "of":
				// Список без типа элемента (Repeat.items) уточняется контрактом источника.
				if def["type"] != "list" || def["of"] != nil || !sourceType(env.SourceTypes, map[string]any{"type": "list", "of": v}) {
					return nil, nil, "", extractFail("COMPONENT_EXTRACT_INPUT_INVALID", ptr+"/def/of", "", "тип элемента списка берётся из контракта источника")
				}
				def[k] = v
			default:
				return nil, nil, "", extractFail("COMPONENT_EXTRACT_INPUT_INVALID", ptr+"/def/"+k, "", "расширение типа запрещено")
			}
		}
		if def["type"] == "list" && def["of"] == nil {
			return nil, nil, "", extractFail("COMPONENT_EXTRACT_INPUT_INVALID", ptr+"/def", "", "для списка нужен тип элемента из контракта источника")
		}
	} else if len(in.From) == 0 {
		t, _ := in.Def["type"].(string)
		bare := map[string]any{}
		for k, v := range in.Def {
			if k != "required" && k != "description" && k != "default" {
				bare[k] = v
			}
		}
		if t == "list" || t == "object" {
			if !sourceType(env.SourceTypes, bare) {
				return nil, nil, "", extractFail("COMPONENT_EXTRACT_INPUT_INVALID", ptr+"/def", "", "list/object копируется из контракта источника")
			}
		} else if !slices.Contains(newInputTypes, t) {
			return nil, nil, "", extractFail("COMPONENT_EXTRACT_INPUT_INVALID", ptr+"/def/type", "", "недопустимый тип входа")
		} else if t == "asset" && in.Def["assetKind"] != "image" {
			return nil, nil, "", extractFail("COMPONENT_EXTRACT_INPUT_INVALID", ptr+"/def/assetKind", "", "новый вход-ассет — только image")
		}
		for k, v := range in.Def {
			def[k] = v
		}
	} else {
		// Без manifest тип не выводится: вход из свойства требует PropTypes.
		return nil, nil, "", extractFail("COMPONENT_EXTRACT_INPUT_INVALID", ptr, "", "тип свойства неизвестен")
	}
	if in.Required != nil {
		def["required"] = *in.Required
	}
	return fromJSON(mustRaw(def)).(map[string]any), value, kind, nil
}

func structuralProp(n map[string]any, prop string) bool {
	// Slot встречается только в документах-компонентах, а извлечение — только из страницы.
	return n["type"] == "Repeat" && (prop == "key" || prop == "as" || prop == "limit")
}

// typeOnly — тип свойства без значения по умолчанию и описания (их задаёт вход).
func typeOnly(t map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range t {
		switch k {
		case "default", "description", "required", "label", "group", "nodeType":
			continue
		}
		out[k] = v
	}
	return out
}

func sourceType(types []map[string]any, def map[string]any) bool {
	want := string(mustRaw(def))
	for _, t := range types {
		if string(mustRaw(t)) == want {
			return true
		}
	}
	return false
}

// SourceTypes собирает list/object-типы из result-контрактов источников manifest.
func SourceTypes(app map[string]any) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(v any) {
		t, _ := v.(map[string]any)
		if t == nil {
			return
		}
		if t["type"] == "list" || t["type"] == "object" {
			out = append(out, typeOnly(t))
		}
		walk(t["of"])
		fields, _ := t["fields"].(map[string]any)
		for _, k := range sortedKeys(fields) {
			walk(fields[k])
		}
	}
	sources, _ := app["dataSources"].(map[string]any)
	for _, name := range sortedKeys(sources) {
		s, _ := sources[name].(map[string]any)
		walk(s["result"])
	}
	return out
}

// dependencies проверяет области выражений, $local и ссылки на узлы через границу.
func (x *extraction) dependencies(component map[string]bool, sourceOf map[string]string, decisions map[string]string) (map[string]string, error) {
	locals := map[string]string{}
	for _, id := range sortedKeys(boolMap(component)) {
		n := x.node(id)
		for _, field := range []string{"props", "bindings"} {
			m, _ := n[field].(map[string]any)
			for _, prop := range sortedKeys(m) {
				if _, ok := sourceOf[id+"\x00"+prop]; ok {
					continue
				}
				if n["type"] == "Repeat" && prop == "key" {
					continue // ключ вычисляется по элементу самого Repeat
				}
				if err := x.scopes(id, m[prop], ir.Pointer("nodes", id, field, prop), false, locals); err != nil {
					return nil, err
				}
			}
		}
		if err := x.scopes(id, n["when"], ir.Pointer("nodes", id, "when"), true, locals); err != nil {
			return nil, err
		}
		if err := x.scopes(id, n["on"], ir.Pointer("nodes", id, "on"), true, locals); err != nil {
			return nil, err
		}
	}
	for key, decision := range decisions {
		if _, ok := locals[key]; !ok {
			return nil, fail("PAYLOAD_INVALID", "localContent.%s не используется поддеревом", key)
		}
		locals[key] = decision
	}
	// Ссылки на узлы через границу компонента в обе стороны.
	if err := x.nodeRefs(component); err != nil {
		return nil, err
	}
	return locals, nil
}

// scopes проверяет выражения значения; strict — поле, где внешняя область не может стать входом.
func (x *extraction) scopes(id string, v any, ptr string, strict bool, locals map[string]string) error {
	var problem error
	strs(v, func(s string) {
		if problem != nil {
			return
		}
		m := scopePattern.FindStringSubmatch(s)
		if m == nil {
			return
		}
		external := false
		switch m[1] {
		case "local":
			if m[2] != "" {
				if _, ok := locals[m[2]]; !ok {
					locals[m[2]] = "copy"
				}
			}
		case "content", "data":
			external = true
		case "item", "index":
			external = !x.itemScoped(id, "")
		case "each":
			external = !x.itemScoped(id, m[2])
		}
		if external {
			msg := "выражение %s использует область вне поддерева: сделайте свойство входом"
			if strict {
				msg = "выражение %s в условии или действии использует область вне поддерева"
			}
			problem = extractFail("COMPONENT_EXTRACT_EXTERNAL_SCOPE", ptr, id, msg, s)
		}
	})
	return problem
}

// itemScoped — у узла есть Repeat-предок внутри поддерева (alias — для $each).
func (x *extraction) itemScoped(id, alias string) bool {
	for child := id; child != x.root; {
		parent := x.parentOf[child] // каждый некорневой узел поддерева имеет родителя в нём
		p := x.node(parent)
		if p["type"] == "Repeat" && slices.Contains(list(p, ""), any(child)) {
			as, _ := asMap(p["props"])["as"].(string)
			if alias == "" || as == alias {
				return true
			}
		}
		child = parent
	}
	return false
}

// strs вызывает f для каждой строки-выражения значения, пропуская литералы {"lit": …}.
func strs(v any, f func(string)) { walkStrings(v, false, f) }

// allStrs — все строки значения, включая литералы (ссылки на узлы).
func allStrs(v any, f func(string)) { walkStrings(v, true, f) }

func walkStrings(v any, literals bool, f func(string)) {
	switch t := v.(type) {
	case string:
		f(t)
	case map[string]any:
		for _, k := range sortedKeys(t) {
			if k == "lit" && !literals {
				continue
			}
			walkStrings(t[k], literals, f)
		}
	case []any:
		for _, val := range t {
			walkStrings(val, literals, f)
		}
	}
}

func localRefs(doc map[string]any) map[string]bool {
	used := map[string]bool{}
	for _, raw := range nodes(doc) {
		n, _ := raw.(map[string]any)
		for _, field := range []string{"props", "bindings", "when", "on"} {
			strs(n[field], func(s string) {
				if m := scopePattern.FindStringSubmatch(s); m != nil && m[1] == "local" && m[2] != "" {
					used[m[2]] = true
				}
			})
		}
	}
	return used
}

// effectiveMode — режим места извлечения: проект → документ → зоны предков.
func effectiveMode(page map[string]any, parentID string, project map[string]any) string {
	mode := "SYSTEM"
	if m, ok := project["defaultMode"].(string); ok && slices.Contains(modeNames, m) {
		mode = m
	} else if m, ok := project["mode"].(string); ok && slices.Contains(modeNames, m) {
		mode = m
	}
	if m, ok := asMap(page["policy"])["mode"].(string); ok && slices.Contains(modeNames, m) {
		mode = m
	}
	var chain []string
	for id := parentID; id != ""; {
		chain = append(chain, id)
		loc, ok := locate(page, id)
		if !ok {
			break
		}
		id = loc.ParentID
	}
	for i := len(chain) - 1; i >= 0; i-- {
		if m, ok := asMap(asMap(nodes(page)[chain[i]])["zone"])["mode"].(string); ok && slices.Contains(modeNames, m) {
			mode = m
		}
	}
	return mode
}

// RevertExtract — обратная операция: восстанавливает поддерево по сохранённому снимку.
func RevertExtract(page map[string]any, raw json.RawMessage) (Result, ExtractRevert, error) {
	var p ExtractRevert
	if err := decode(raw, &p); err != nil {
		return Result{}, p, err
	}
	current, ok := nodes(page)[p.InstanceNodeID]
	if !ok || string(mustRaw(current)) != string(mustRaw(p.Instance)) {
		return Result{}, p, fail("UNDO_STATE_MISMATCH", "экземпляр %s изменён после извлечения", p.InstanceNodeID)
	}
	parent, err := node(page, p.Before.ParentID)
	if err != nil {
		return Result{}, p, fail("UNDO_STATE_MISMATCH", "место экземпляра изменено")
	}
	l := slices.Clone(list(parent, p.Before.Slot))
	if p.Before.Index >= len(l) || fmt.Sprint(l[p.Before.Index]) != p.InstanceNodeID {
		return Result{}, p, fail("UNDO_STATE_MISMATCH", "место экземпляра изменено")
	}
	var root string
	for id, raw := range p.Before.Nodes {
		if _, exists := nodes(page)[id]; exists {
			// Узлы слотов остаются на странице и должны совпадать со снимком.
			if string(mustRaw(nodes(page)[id])) != string(mustRaw(raw)) {
				return Result{}, p, fail("UNDO_STATE_MISMATCH", "узел слота %s изменён после извлечения", id)
			}
		}
	}
	for id, raw := range p.Before.Nodes {
		nodes(page)[id] = cloneValue(raw)
		if !slicesContainsChild(p.Before.Nodes, id) {
			root = id
		}
	}
	delete(nodes(page), p.InstanceNodeID)
	l[p.Before.Index] = root
	setList(parent, p.Before.Slot, l)
	if len(p.Before.LocalContent) > 0 {
		local, _ := page["localContent"].(map[string]any)
		if local == nil {
			local = map[string]any{}
			page["localContent"] = local
		}
		for k, v := range p.Before.LocalContent {
			local[k] = cloneValue(v)
		}
	}
	if v := ir.ValidateDocument(page); !v.Valid {
		return Result{}, p, &Error{Code: "OPERATION_INVALID", Message: "страница после отмены не проходит валидацию", Diagnostics: v.Diagnostics}
	}
	return Result{
		Before:  map[string]any{"componentId": p.ComponentID, "instanceNodeId": p.InstanceNodeID},
		After:   map[string]any{"nodeId": root},
		Inverse: Op{Type: ComponentExtract, Payload: p.Redo},
	}, p, nil
}

// ExtractRevert — payload component.revertExtract.
type ExtractRevert struct {
	ComponentID    string          `json:"componentId"`
	InstanceNodeID string          `json:"instanceNodeId"`
	Instance       map[string]any  `json:"instance"`
	Redo           json.RawMessage `json:"redo"`
	Before         struct {
		ParentID     string         `json:"parentId"`
		Slot         string         `json:"slot"`
		Index        int            `json:"index"`
		Nodes        map[string]any `json:"nodes"`
		LocalContent map[string]any `json:"localContent"`
	} `json:"before"`
}

func slicesContainsChild(all map[string]any, id string) bool {
	for _, raw := range all {
		n, _ := raw.(map[string]any)
		if slices.Contains(children(n), id) {
			return true
		}
	}
	return false
}

// --- Регистрация для журнала: переигрывание над страницей (rebase) ---------------

// applyExtractPage переигрывает канонический component.extract над страницей.
// Документ компонента создаёт слой Change Set; здесь меняется только страница.
func applyExtractPage(doc map[string]any, raw json.RawMessage, _ ir.IDGenerator) (Result, error) {
	p, err := DecodeExtract(raw)
	if err != nil {
		return Result{}, err
	}
	if !p.Canonical() {
		return Result{}, fail("PAYLOAD_INVALID", "component.extract применяется через Change Set")
	}
	r, err := Extract(doc, p, ExtractEnv{})
	if err != nil {
		return Result{}, err
	}
	return Result{Before: r.Before, After: r.After, Inverse: r.Inverse, Payload: mustRaw(r.Payload)}, nil
}

func applyRevertExtractPage(doc map[string]any, raw json.RawMessage, _ ir.IDGenerator) (Result, error) {
	r, _, err := RevertExtract(doc, raw)
	return r, err
}

// --- Вспомогательные ---------------------------------------------------------------

func anyMap[V any](m map[string]V) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func boolMap(m map[string]bool) map[string]any {
	out := make(map[string]any, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func stringsToAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

func listOf(n map[string]any, name string) []any { return list(n, name) }

func deleteEntry(n map[string]any, field, key string) {
	m, _ := n[field].(map[string]any)
	delete(m, key)
	if m != nil && len(m) == 0 {
		delete(n, field)
	}
}

func setEntryValue(n map[string]any, field, key string, v any) {
	m, _ := n[field].(map[string]any)
	if m == nil {
		m = map[string]any{}
		n[field] = m
	}
	m[key] = v
}

func copyBack(dst, src map[string]any) {
	for k := range dst {
		delete(dst, k)
	}
	for k, v := range src {
		dst[k] = v
	}
}
