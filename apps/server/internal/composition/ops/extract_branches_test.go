package ops

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Ветви проверок component.extract, не покрытые основным сценарием (STU-402…410).
func TestExtractEdgeCasesSTU410(t *testing.T) {
	zero, one := 0, 1
	yes := true
	desc := strings.Repeat("я", 501)
	cases := []struct {
		name, code string
		src        string
		env        func(*ExtractEnv)
		edit       func(*ExtractPayload)
	}{
		{"empty node", "PAYLOAD_INVALID", showcase, nil, func(p *ExtractPayload) { p.NodeID = "" }},
		{"description", "PAYLOAD_INVALID", showcase, nil, func(p *ExtractPayload) { p.Description = &desc }},
		{"orphan", "NODE_NOT_FOUND", strings.Replace(showcase, `"nodes":{`, `"nodes":{"n_orphan":{"id":"n_orphan","type":"Box"},`, 1), nil, func(p *ExtractPayload) { p.NodeID = "n_orphan" }},
		{"canonical incomplete", "PAYLOAD_INVALID", showcase, nil, func(p *ExtractPayload) { p.ComponentID = "c" }},
		{"slot max zero", "COMPONENT_EXTRACT_SLOT_INVALID", showcase, nil, func(p *ExtractPayload) {
			p.Slots["footer"] = ExtractSlot{NodeIDs: []string{"n_btn"}, Max: &zero}
		}},
		{"slot at with nodes", "COMPONENT_EXTRACT_SLOT_INVALID", showcase, nil, func(p *ExtractPayload) {
			p.Slots["footer"] = ExtractSlot{NodeIDs: []string{"n_btn"}, At: &SlotPosition{ParentID: "n_stack"}}
		}},
		{"slot overflow", "COMPONENT_EXTRACT_SLOT_INVALID", showcase, nil, func(p *ExtractPayload) {
			p.Slots["footer"] = ExtractSlot{NodeIDs: []string{"n_btn", "n_h1"}}
		}},
		{"slot count", "PAYLOAD_INVALID", showcase, nil, func(p *ExtractPayload) {
			for i := range 9 {
				p.Slots[fmt.Sprintf("s%d", i)] = ExtractSlot{At: &SlotPosition{ParentID: "n_stack"}}
			}
		}},
		{"empty slot inside slot", "COMPONENT_EXTRACT_SLOT_INVALID", strings.Replace(showcase, `"n_btn":{"id":"n_btn","type":"Button",`, `"n_btn":{"id":"n_btn","type":"Button","children":["n_ico"],`, 1) /* Button is not a container */, nil, nil},
		{"input count", "PAYLOAD_INVALID", showcase, nil, func(p *ExtractPayload) {
			for i := range 33 {
				p.Inputs[fmt.Sprintf("x%d", i)] = ExtractInput{Def: map[string]any{"type": "text"}}
			}
		}},
		{"input empty", "COMPONENT_EXTRACT_INPUT_INVALID", showcase, nil, func(p *ExtractPayload) { p.Inputs["x"] = ExtractInput{} }},
		{"source outside", "NODE_NOT_FOUND", showcase, nil, func(p *ExtractPayload) {
			p.Inputs["x"] = ExtractInput{From: []ExtractSource{{NodeID: "n_top", Prop: "text"}}}
		}},
		{"required without value", "COMPONENT_EXTRACT_INPUT_INVALID", showcase, nil, func(p *ExtractPayload) {
			p.Inputs["as"] = ExtractInput{From: []ExtractSource{{NodeID: "n_txt", Prop: "as"}}, Required: &yes}
		}},
		{"values not enum", "COMPONENT_EXTRACT_INPUT_INVALID", showcase, nil, func(p *ExtractPayload) {
			p.Inputs["level"] = ExtractInput{From: []ExtractSource{{NodeID: "n_h1", Prop: "level"}}, Def: map[string]any{"values": []any{"1"}}}
		}},
		{"of not list", "COMPONENT_EXTRACT_INPUT_INVALID", showcase, nil, func(p *ExtractPayload) {
			p.Inputs["level"] = ExtractInput{From: []ExtractSource{{NodeID: "n_h1", Prop: "level"}}, Def: map[string]any{"of": map[string]any{"type": "text"}}}
		}},
		{"asset not image", "COMPONENT_EXTRACT_INPUT_INVALID", showcase, nil, func(p *ExtractPayload) {
			p.Inputs["file"] = ExtractInput{Def: map[string]any{"type": "asset", "assetKind": "file"}}
		}},
		{"enum without values", "OPERATION_INVALID", showcase, nil, func(p *ExtractPayload) {
			p.Inputs["tone"] = ExtractInput{Def: map[string]any{"type": "enum"}}
		}},
		{"no prop types", "COMPONENT_EXTRACT_INPUT_INVALID", showcase, func(e *ExtractEnv) { e.PropTypes = nil }, nil},
		{"pending in chain", "COMPONENT_EXTRACT_PROTECTED", strings.Replace(showcase, `"bindings":{"text":"$local.t_note"}`, `"bindings":{"text":"$local.t_note"},"on":{"click":[{"action":"track"},{"action":"pending:0192f1c5-0000-7000-8000-000000000009"}]}`, 1), nil, nil},
		{"external action", "COMPONENT_EXTRACT_EXTERNAL_SCOPE", strings.Replace(showcase, `"bindings":{"text":"$local.t_note"}`, `"bindings":{"text":"$local.t_note"},"on":{"click":[{"action":"track","args":{"event":"$content.title"}}]}`, 1), nil, nil},
		{"each outside", "COMPONENT_EXTRACT_EXTERNAL_SCOPE", strings.Replace(showcase, `"$local.t_note"`, `"$each.row.title"`, 1), nil, nil},
		{"structural slot prop", "COMPONENT_EXTRACT_INPUT_INVALID", showcase, nil, func(p *ExtractPayload) {
			p.NodeID = "n_stack"
			p.Slots = map[string]ExtractSlot{}
			p.Inputs = map[string]ExtractInput{"title": {From: []ExtractSource{{NodeID: "n_h1", Prop: "text"}}}, "d": {From: []ExtractSource{{NodeID: "n_txt", Prop: "text"}}}}
			p.Inputs["n"] = ExtractInput{From: []ExtractSource{{NodeID: "n_stack", Prop: "direction"}}, Def: map[string]any{"type": "text"}}
		}},
		{"too many instances", "LIMIT_EXCEEDED", manyInstances(), nil, func(p *ExtractPayload) {
			p.Slots = nil
			p.Inputs = map[string]ExtractInput{"title": {From: []ExtractSource{{NodeID: "n_h1", Prop: "text"}}}, "description": {From: []ExtractSource{{NodeID: "n_txt", Prop: "text"}}}, "image": {From: []ExtractSource{{NodeID: "n_img", Prop: "src"}}}}
		}},
		{"instance id taken", "NODE_ID_DUPLICATE", showcase, nil, func(p *ExtractPayload) {
			p.ComponentID, p.InstanceNodeID, p.Slots = "0192f1c5-0000-7000-8000-000000000001", "n_top", nil
		}},
		{"slot id taken", "NODE_ID_DUPLICATE", showcase, nil, func(p *ExtractPayload) {
			p.ComponentID, p.InstanceNodeID, p.SlotNodeIDs = "0192f1c5-0000-7000-8000-000000000001", "n_inst", map[string]string{"footer": "n_top"}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := showcasePayload()
			p.Slots["footer"] = ExtractSlot{NodeIDs: []string{"n_btn"}}
			if c.name == "empty slot inside slot" {
				p.Slots["inner"] = ExtractSlot{At: &SlotPosition{ParentID: "n_btn"}, Min: &zero}
				p.Inputs["x"] = ExtractInput{Def: map[string]any{"type": "text"}, Required: &one2}
			}
			if c.edit != nil {
				c.edit(&p)
			}
			env := builtinEnv()
			if c.env != nil {
				c.env(&env)
			}
			_, err := Extract(parse(t, c.src), p, env)
			if got := extractCode(err); got != c.code {
				t.Fatalf("code %q, want %q (%v)", got, c.code, err)
			}
		})
	}
	_ = one
}

var one2 = false

func manyInstances() string {
	extra := ""
	children := ""
	for i := range 64 {
		extra += fmt.Sprintf(`"n_c%02d":{"id":"n_c%02d","type":"Composed","ref":{"component":"0192f1c5-0000-7000-8000-000000000002"}},`, i, i)
		children += fmt.Sprintf(`"n_c%02d",`, i)
	}
	s := strings.Replace(showcase, `"nodes":{`, `"nodes":{`+extra, 1)
	return strings.Replace(s, `"children":["n_top","n_grid"]`, `"children":[`+children+`"n_top","n_grid"]`, 1)
}

func TestExtractVariantsSTU403405(t *testing.T) {
	// Слот в именованном слоте родителя, общий старт пустого слота и диапазона, перенос
	// единственного ключа localContent, описание и режим из настроек проекта и документа.
	src := `{"irVersion":"1.0","kind":"page","root":"n_root","policy":{"mode":"FREE"},
	 "localContent":{"t_one":{"type":"text","value":{"ru":"Один"}}},
	 "nodes":{
	  "n_root":{"id":"n_root","type":"Box","children":["n_box"]},
	  "n_box":{"id":"n_box","type":"Stack","children":["n_aa","n_bb"]},
	  "n_aa":{"id":"n_aa","type":"Text","bindings":{"text":"$local.t_one"}},
	  "n_bb":{"id":"n_bb","type":"Repeat","bindings":{"items":"$data.list.items"},"props":{"as":"row"},"children":["n_row"],"slots":{"empty":["n_none"]}},
	  "n_row":{"id":"n_row","type":"Stack","children":["n_cell"]},
	  "n_cell":{"id":"n_cell","type":"Text","bindings":{"text":"$each.row.title"},"when":{"op":"exists","arg":"$item.title"}},
	  "n_none":{"id":"n_none","type":"Text","props":{"text":"—"}}}}`
	list := map[string]any{"type": "list", "of": map[string]any{"type": "object", "fields": map[string]any{"title": map[string]any{"type": "text"}}}}
	env := builtinEnv()
	env.SourceTypes = append(env.SourceTypes, list)
	env.Project = map[string]any{"defaultMode": "SYSTEM"}
	description := "Описание"
	zero := 0
	p := ExtractPayload{
		NodeID: "n_box", Name: "List", Description: &description,
		Inputs:       map[string]ExtractInput{"rows": {From: []ExtractSource{{NodeID: "n_bb", Prop: "items"}}, Def: map[string]any{"of": list["of"], "description": "строки", "required": false}}},
		Slots:        map[string]ExtractSlot{"empty": {NodeIDs: []string{"n_none"}}, "lead": {At: &SlotPosition{ParentID: "n_box", Index: 0}, Min: &zero}, "head": {NodeIDs: []string{"n_aa"}}},
		LocalContent: nil,
	}
	page := parse(t, src)
	before := toJSON(t, page)
	r, err := Extract(page, p, env)
	if err != nil {
		t.Fatal(err, err.(*Error).Diagnostics)
	}
	if toJSON(t, r.Component["policy"]) != `{"mode":"FREE"}` || toJSON(t, r.Component["meta"]) != `{"description":"Описание","title":"List"}` {
		t.Fatalf("header: %s %s", toJSON(t, r.Component["policy"]), toJSON(t, r.Component["meta"]))
	}
	box := r.Component["nodes"].(map[string]any)["n_box"].(map[string]any)
	if got := toJSON(t, box["children"]); got != `["n_slot3","n_slot2","n_bb"]` {
		t.Fatalf("lead and head share position 0: %s", got)
	}
	if got := toJSON(t, r.Component["nodes"].(map[string]any)["n_bb"].(map[string]any)["slots"]); got != `{"empty":["n_slot1"]}` {
		t.Fatalf("slot inside a named slot: %s", got)
	}
	if _, _, err := RevertExtract(page, r.Inverse.Payload); err != nil || toJSON(t, page) != before {
		t.Fatalf("revert: %v", err)
	}
	// Перенос единственного ключа удаляет localContent страницы, отмена его возвращает.
	p.Slots = map[string]ExtractSlot{"empty": {NodeIDs: []string{"n_none"}}}
	p.LocalContent = map[string]string{"t_one": "move"}
	env.Gen = seqGen("n_i2", "n_s2")
	page = parse(t, src)
	r, err = Extract(page, p, env)
	if err != nil {
		t.Fatal(err)
	}
	if page["localContent"] != nil {
		t.Fatal("empty localContent must be removed")
	}
	if _, _, err := RevertExtract(page, r.Inverse.Payload); err != nil || toJSON(t, page) != before {
		t.Fatalf("revert move: %v", err)
	}
	if mode := effectiveMode(map[string]any{}, "", map[string]any{"mode": "CODE"}); mode != "CODE" {
		t.Fatal(mode)
	}
}

func TestExtractDecodeErrorsSTU409(t *testing.T) {
	if _, err := DecodeExtract(json.RawMessage(`{"x":1}`)); extractCode(err) != "PAYLOAD_INVALID" {
		t.Fatal(err)
	}
	if _, _, err := RevertExtract(parse(t, showcase), json.RawMessage(`[]`)); extractCode(err) != "PAYLOAD_INVALID" {
		t.Fatal(err)
	}
	if _, err := Apply(parse(t, showcase), op(ComponentExtract, `[]`), nil); extractCode(err) != "PAYLOAD_INVALID" {
		t.Fatal(err)
	}
	canonical := `{"nodeId":"n_nope","name":"X","componentId":"c","instanceNodeId":"n_inst"}`
	if _, err := Apply(parse(t, showcase), op(ComponentExtract, canonical), nil); extractCode(err) != "NODE_NOT_FOUND" {
		t.Fatal(err)
	}
	if _, err := Apply(parse(t, showcase), op(ComponentRevertExtract, `{"componentId":"c","instanceNodeId":"n_x"}`), nil); extractCode(err) != "UNDO_STATE_MISMATCH" {
		t.Fatal(err)
	}
	// Отмена, когда родителя экземпляра больше нет.
	page := parse(t, showcase)
	r, err := Extract(page, showcasePayload(), builtinEnv())
	if err != nil {
		t.Fatal(err)
	}
	var revert map[string]any
	_ = json.Unmarshal(r.Inverse.Payload, &revert)
	revert["before"].(map[string]any)["parentId"] = "n_gone"
	raw, _ := json.Marshal(revert)
	if _, _, err := RevertExtract(page, raw); extractCode(err) != "UNDO_STATE_MISMATCH" {
		t.Fatal(err)
	}
}

func TestPlanExtractVariantsSTU401(t *testing.T) {
	src := strings.Replace(showcase, `"bindings":{"text":"$local.t_note"}`, `"bindings":{"text":"$context.locale"},"when":{"op":"exists","arg":"$data.x"}`, 1)
	src = strings.Replace(src, `"n_stack":{"id":"n_stack","type":"Stack",`, `"n_stack":{"id":"n_stack","type":"Stack","zone":{"id":"z"},`, 1)
	plan := PlanExtract(parse(t, src), "n_grid", builtinEnv(), true)
	codes := []string{}
	for _, b := range plan.Blockers {
		codes = append(codes, string(b.Code))
	}
	if strings.Join(codes, ",") != "COMPONENT_EXTRACT_PROTECTED,COMPONENT_EXTRACT_EXTERNAL_SCOPE" {
		t.Fatalf("blockers: %v", codes)
	}
	for _, c := range plan.Candidates {
		if c.NodeID == "n_note" && c.Prop == "text" && c.Current != "context" {
			t.Fatalf("context: %+v", c)
		}
	}
	if p := PlanExtract(parse(t, showcase), "n_nope", builtinEnv(), true); len(p.Blockers) != 1 || p.Blockers[0].Code != "NODE_NOT_FOUND" {
		t.Fatalf("missing node: %v", p.Blockers)
	}
	// Repeat внутри поддерева: $item внутренний, $each по alias, внешний $item — обязательный вход.
	repeat := `{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{
	 "n_root":{"id":"n_root","type":"Box","children":["n_rep"]},
	 "n_rep":{"id":"n_rep","type":"Repeat","props":{"as":"row"},"bindings":{"items":"$data.list.items"},"children":["n_block"],"slots":{"empty":["n_none"]}},
	 "n_block":{"id":"n_block","type":"Stack","children":["n_in"]},
	 "n_in":{"id":"n_in","type":"Repeat","props":{"as":"cell"},"bindings":{"items":"$item.cells"},"children":["n_t"]},
	 "n_t":{"id":"n_t","type":"Text","bindings":{"text":"$each.cell.title"},"props":{"as":"p"}},
	 "n_none":{"id":"n_none","type":"Text"}}}`
	plan = PlanExtract(parse(t, repeat), "n_block", builtinEnv(), true)
	kinds := map[string]string{}
	for _, c := range plan.Candidates {
		kinds[c.NodeID+"."+c.Prop] = fmt.Sprintf("%s %v", c.Current, c.External)
	}
	if kinds["n_in.items"] != "item true" || kinds["n_t.text"] != "item false" {
		t.Fatalf("kinds: %v", kinds)
	}
	other := strings.Replace(repeat, `"$each.cell.title"`, `"$each.other.title"`, 1)
	plan = PlanExtract(parse(t, other), "n_block", builtinEnv(), true)
	for _, c := range plan.Candidates {
		if c.NodeID == "n_t" && c.Prop == "text" && !c.External {
			t.Fatalf("$each outside: %+v", c)
		}
	}
	if len(PlanExtract(parse(t, repeat), "n_rep", builtinEnv(), true).SlotRanges) != 4 {
		t.Fatal("named slot ranges")
	}
}

// Литерал узла без привязок становится входом; описание и default типа свойства не копируются.
func TestExtractLiteralInputSTU403(t *testing.T) {
	src := `{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{
	 "n_root":{"id":"n_root","type":"Box","children":["n_card"]},
	 "n_card":{"id":"n_card","type":"Card","props":{"tone":"dark"}}}}`
	env := ExtractEnv{
		PropTypes: func(map[string]any) (map[string]any, bool) {
			return map[string]any{"tone": map[string]any{"type": "enum", "values": []any{"dark", "light"}, "default": "light", "description": "Тон"}}, true
		},
		ComponentID: "0192f1c5-0000-7000-8000-000000000001",
	}
	page := parse(t, src)
	r, err := Extract(page, ExtractPayload{NodeID: "n_card", Name: "Card", Inputs: map[string]ExtractInput{"tone": {From: []ExtractSource{{NodeID: "n_card", Prop: "tone"}}}}}, env)
	if err != nil {
		t.Fatal(err)
	}
	if got := toJSON(t, r.Component["inputs"]); got != `{"tone":{"type":"enum","values":["dark","light"]}}` {
		t.Fatal(got)
	}
	if got := toJSON(t, r.Component["nodes"].(map[string]any)["n_card"]); got != `{"bindings":{"tone":"$props.tone"},"id":"n_card","type":"Card"}` {
		t.Fatal(got)
	}
	inst := page["nodes"].(map[string]any)[r.Payload.InstanceNodeID].(map[string]any)
	if toJSON(t, inst["props"]) != `{"tone":"dark"}` {
		t.Fatal(toJSON(t, inst))
	}
}
