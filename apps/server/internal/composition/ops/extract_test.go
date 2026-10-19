package ops

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"pgregory.net/rapid"
)

// Блок «ProductShowcase» шага 2 MVP: Grid → Stack(Heading, Text, Button) + Image.
const showcase = `{"irVersion":"1.0","kind":"page","root":"n_root",
 "localContent":{
  "t_title":{"type":"text","value":{"ru":"Новинки","en":"New"}},
  "t_desc":{"type":"text","value":{"ru":"Описание"}},
  "t_cta":{"type":"text","value":{"ru":"Купить"}},
  "t_note":{"type":"text","value":{"ru":"Общая подпись"}},
  "t_page":{"type":"text","value":{"ru":"Заголовок страницы"}}},
 "nodes":{
  "n_root":{"id":"n_root","type":"Box","children":["n_top","n_grid"]},
  "n_top":{"id":"n_top","type":"Heading","bindings":{"text":"$local.t_page"}},
  "n_grid":{"id":"n_grid","type":"Grid","design":{"gap":"md"},"children":["n_stack","n_img"]},
  "n_stack":{"id":"n_stack","type":"Stack","children":["n_h1","n_txt","n_note","n_btn"]},
  "n_h1":{"id":"n_h1","type":"Heading","props":{"level":2},"bindings":{"text":"$local.t_title"}},
  "n_txt":{"id":"n_txt","type":"Text","bindings":{"text":"$local.t_desc"}},
  "n_note":{"id":"n_note","type":"Text","bindings":{"text":"$local.t_note"}},
  "n_btn":{"id":"n_btn","type":"Button","bindings":{"label":"$local.t_cta"}},
  "n_img":{"id":"n_img","type":"Image","bindings":{"src":"$content.cover"}}}}`

var productList = map[string]any{"type": "list", "of": map[string]any{"type": "object", "fields": map[string]any{
	"id": map[string]any{"type": "string"}, "title": map[string]any{"type": "text"},
}}}

func builtinEnv() ExtractEnv {
	return ExtractEnv{
		PropTypes: func(n map[string]any) (map[string]any, bool) {
			c := manifest.BuiltinContract(fmt.Sprint(n["type"]))
			if c == nil {
				return nil, false
			}
			p, _ := c["props"].(map[string]any)
			return p, true
		},
		Container: func(n map[string]any) bool {
			c := manifest.BuiltinContract(fmt.Sprint(n["type"]))
			b, _ := c["container"].(bool)
			return b
		},
		SourceTypes: SourceTypes(map[string]any{"dataSources": map[string]any{"store.products": map[string]any{"result": productList}}}),
		ComponentID: "0192f1c5-0000-7000-8000-000000000001",
		Gen:         seqGen("n_inst", "n_slot1", "n_slot2", "n_slot3"),
	}
}

func showcasePayload() ExtractPayload {
	yes := true
	return ExtractPayload{
		NodeID: "n_grid", Name: " ProductShowcase ",
		Inputs: map[string]ExtractInput{
			"title":       {From: []ExtractSource{{NodeID: "n_h1", Prop: "text"}}, Required: &yes},
			"description": {From: []ExtractSource{{NodeID: "n_txt", Prop: "text"}}},
			"image":       {From: []ExtractSource{{NodeID: "n_img", Prop: "src"}}},
			"products":    {Def: productList},
		},
		Slots: map[string]ExtractSlot{"footer": {NodeIDs: []string{"n_btn"}}},
	}
}

func extractCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestExtractShowcaseSTU402(t *testing.T) {
	page := parse(t, showcase)
	orig := toJSON(t, page)
	r, err := Extract(page, showcasePayload(), builtinEnv())
	if err != nil {
		t.Fatal(err)
	}
	comp := r.Component
	if comp["kind"] != "component" || comp["root"] != "n_grid" || toJSON(t, comp["meta"]) != `{"title":"ProductShowcase"}` {
		t.Fatalf("component header: %s", toJSON(t, comp))
	}
	if toJSON(t, comp["policy"]) != `{"mode":"SYSTEM"}` {
		t.Errorf("policy: %s", toJSON(t, comp["policy"]))
	}
	inputs := toJSON(t, comp["inputs"])
	for _, want := range []string{`"title":{"content":true,"required":true,"type":"text"}`, `"image":{"assetKind":"image","content":true,"type":"asset"}`, `"products":{"of"`} {
		if !strings.Contains(inputs, want) {
			t.Errorf("inputs %s: missing %s", inputs, want)
		}
	}
	nodes := comp["nodes"].(map[string]any)
	if toJSON(t, nodes["n_h1"]) != `{"bindings":{"text":"$props.title"},"id":"n_h1","props":{"level":2},"type":"Heading"}` {
		t.Errorf("source node: %s", toJSON(t, nodes["n_h1"]))
	}
	if toJSON(t, nodes["n_stack"].(map[string]any)["children"]) != `["n_h1","n_txt","n_note","n_slot1"]` {
		t.Errorf("slot replacement: %s", toJSON(t, nodes["n_stack"]))
	}
	if toJSON(t, nodes["n_slot1"]) != `{"id":"n_slot1","props":{"name":"footer"},"type":"Slot"}` || toJSON(t, comp["slotDefs"]) != `{"footer":{"allowedTypes":["Button"]}}` {
		t.Errorf("slot: %s %s", toJSON(t, nodes["n_slot1"]), toJSON(t, comp["slotDefs"]))
	}
	if _, ok := nodes["n_btn"]; ok {
		t.Error("slot content must stay on the page")
	}
	// Q-4: текст без входа копируется и остаётся на странице.
	if toJSON(t, comp["localContent"]) != `{"t_note":{"type":"text","value":{"ru":"Общая подпись"}}}` {
		t.Errorf("component localContent: %s", toJSON(t, comp["localContent"]))
	}
	pn := page["nodes"].(map[string]any)
	if toJSON(t, pn["n_inst"]) != `{"bindings":{"description":"$local.t_desc","image":"$content.cover","title":"$local.t_title"},"id":"n_inst","ref":{"component":"0192f1c5-0000-7000-8000-000000000001","version":"live"},"slots":{"footer":["n_btn"]},"type":"Composed"}` {
		t.Errorf("instance: %s", toJSON(t, pn["n_inst"]))
	}
	if toJSON(t, pn["n_root"].(map[string]any)["children"]) != `["n_top","n_inst"]` {
		t.Error("instance position")
	}
	for _, gone := range []string{"n_grid", "n_stack", "n_h1", "n_img"} {
		if _, ok := pn[gone]; ok {
			t.Errorf("%s must leave the page", gone)
		}
	}
	if _, ok := page["localContent"].(map[string]any)["t_note"]; !ok {
		t.Error("copy keeps the page key")
	}
	if len(r.Warnings) != 1 || r.Warnings[0].Code != "COMPONENT_INPUT_UNUSED" {
		t.Errorf("warnings: %v", r.Warnings)
	}
	// STU-408: inverse восстанавливает страницу байт-в-байт.
	res, _, err := RevertExtract(page, r.Inverse.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if toJSON(t, page) != orig {
		t.Fatalf("revert:\n%s\n%s", orig, toJSON(t, page))
	}
	if res.Inverse.Type != ComponentExtract {
		t.Fatal("redo payload")
	}
	// Канонический payload детерминирован: повтор даёт те же документы.
	again := parse(t, showcase)
	canon, err := DecodeExtract(res.Inverse.Payload)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Extract(again, canon, ExtractEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if toJSON(t, r2.Component) != toJSON(t, comp) {
		t.Fatalf("redo component differs:\n%s\n%s", toJSON(t, comp), toJSON(t, r2.Component))
	}
}

func TestExtractMoveAndEmptySlotSTU405(t *testing.T) {
	page := parse(t, showcase)
	p := showcasePayload()
	p.LocalContent = map[string]string{"t_note": "move"}
	zero := 0
	p.Slots["badge"] = ExtractSlot{NodeIDs: []string{}, At: &SlotPosition{ParentID: "n_stack", Index: 0}, Min: &zero}
	r, err := Extract(page, p, builtinEnv())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := page["localContent"].(map[string]any)["t_note"]; ok {
		t.Error("move removes the page key")
	}
	stack := r.Component["nodes"].(map[string]any)["n_stack"].(map[string]any)
	if toJSON(t, stack["children"]) != `["n_slot1","n_h1","n_txt","n_note","n_slot2"]` {
		t.Errorf("empty slot position: %s", toJSON(t, stack["children"]))
	}
	inst := page["nodes"].(map[string]any)["n_inst"].(map[string]any)
	if toJSON(t, inst["slots"]) != `{"footer":["n_btn"]}` {
		t.Errorf("empty slot must not be on the instance: %s", toJSON(t, inst["slots"]))
	}
	if toJSON(t, r.Component["slotDefs"].(map[string]any)["badge"]) != `{"min":0}` {
		t.Errorf("badge def: %s", toJSON(t, r.Component["slotDefs"]))
	}
	before := toJSON(t, parse(t, showcase))
	if _, _, err := RevertExtract(page, r.Inverse.Payload); err != nil {
		t.Fatal(err)
	}
	if toJSON(t, page) != before {
		t.Fatal("revert after move")
	}
}

func TestExtractRejectionsSTU410(t *testing.T) {
	zero, one := 0, 1
	no := false
	cases := []struct {
		name, code string
		src        string
		edit       func(*ExtractPayload)
	}{
		{"root", "COMPONENT_EXTRACT_ROOT_DENIED", showcase, func(p *ExtractPayload) { p.NodeID = "n_root" }},
		{"missing", "NODE_NOT_FOUND", showcase, func(p *ExtractPayload) { p.NodeID = "n_nope" }},
		{"kind", "COMPONENT_EXTRACT_TARGET_KIND", strings.Replace(showcase, `"kind":"page"`, `"kind":"component"`, 1), nil},
		{"name", "PAYLOAD_INVALID", showcase, func(p *ExtractPayload) { p.Name = "  " }},
		{"zone", "COMPONENT_EXTRACT_PROTECTED", strings.Replace(showcase, `"type":"Stack",`, `"type":"Stack","zone":{"id":"z","mode":"FREE"},`, 1), nil},
		{"pending", "COMPONENT_EXTRACT_PROTECTED", strings.Replace(showcase, `"type":"Button",`, `"type":"Button","on":{"click":{"action":"pending:x"}},`, 1), func(p *ExtractPayload) { p.Slots = nil }},
		{"external", "COMPONENT_EXTRACT_EXTERNAL_SCOPE", showcase, func(p *ExtractPayload) { delete(p.Inputs, "image") }},
		{"external when", "COMPONENT_EXTRACT_EXTERNAL_SCOPE", strings.Replace(showcase, `"type":"Text",`, `"type":"Text","when":{"op":"exists","arg":"$content.lead"},`, 1), nil},
		{"node ref", "COMPONENT_EXTRACT_NODE_REF", strings.Replace(showcase, `"bindings":{"text":"$local.t_note"}`, `"bindings":{"text":"$local.t_note"},"on":{"click":{"action":"scrollTo","args":{"target":{"lit":"n_top"}}}}`, 1), nil},
		{"node ref inbound", "COMPONENT_EXTRACT_NODE_REF", strings.Replace(showcase, `"type":"Heading","bindings"`, `"type":"Heading","on":{"click":{"action":"scrollTo","args":{"target":{"lit":"n_h1"}}}},"bindings"`, 1), nil},
		{"local in use", "COMPONENT_EXTRACT_LOCAL_IN_USE", strings.Replace(showcase, `"$local.t_page"`, `"$local.t_note"`, 1), func(p *ExtractPayload) { p.LocalContent = map[string]string{"t_note": "move"} }},
		{"local unknown", "PAYLOAD_INVALID", showcase, func(p *ExtractPayload) { p.LocalContent = map[string]string{"t_page": "copy"} }},
		{"local decision", "PAYLOAD_INVALID", showcase, func(p *ExtractPayload) { p.LocalContent = map[string]string{"t_note": "drop"} }},
		{"input name", "COMPONENT_EXTRACT_INPUT_INVALID", showcase, func(p *ExtractPayload) { p.Inputs["__proto__"] = p.Inputs["title"] }},
		{"input twice", "COMPONENT_EXTRACT_INPUT_INVALID", showcase, func(p *ExtractPayload) { p.Inputs["title2"] = ExtractInput{From: p.Inputs["title"].From} }},
		{"input unknown prop", "COMPONENT_EXTRACT_INPUT_INVALID", showcase, func(p *ExtractPayload) {
			p.Inputs["x"] = ExtractInput{From: []ExtractSource{{NodeID: "n_h1", Prop: "nope"}}}
		}},
		{"input differ", "COMPONENT_EXTRACT_INPUT_INCOMPATIBLE", showcase, func(p *ExtractPayload) {
			p.Inputs["title"] = ExtractInput{From: []ExtractSource{{NodeID: "n_h1", Prop: "text"}, {NodeID: "n_txt", Prop: "text"}}}
			delete(p.Inputs, "description")
		}},
		{"input widen", "COMPONENT_EXTRACT_INPUT_INVALID", showcase, func(p *ExtractPayload) {
			p.Inputs["title"] = ExtractInput{From: p.Inputs["title"].From, Def: map[string]any{"type": "richText"}}
		}},
		{"input type", "COMPONENT_EXTRACT_INPUT_INVALID", showcase, func(p *ExtractPayload) { p.Inputs["x"] = ExtractInput{Def: map[string]any{"type": "nodeRef"}} }},
		{"input list", "COMPONENT_EXTRACT_INPUT_INVALID", showcase, func(p *ExtractPayload) {
			p.Inputs["products"] = ExtractInput{Def: map[string]any{"type": "list", "of": map[string]any{"type": "string"}}}
		}},
		{"required empty", "COMPONENT_EXTRACT_INPUT_INVALID", showcase, func(p *ExtractPayload) {
			yes := true
			p.Inputs["level"] = ExtractInput{From: []ExtractSource{{NodeID: "n_txt", Prop: "maxLines"}}, Required: &yes}
		}},
		{"slot gap", "COMPONENT_EXTRACT_SLOT_INVALID", showcase, func(p *ExtractPayload) { p.Slots["footer"] = ExtractSlot{NodeIDs: []string{"n_h1", "n_note"}} }},
		{"slot root", "COMPONENT_EXTRACT_SLOT_INVALID", showcase, func(p *ExtractPayload) { p.Slots["footer"] = ExtractSlot{NodeIDs: []string{"n_grid"}} }},
		{"slot source", "COMPONENT_EXTRACT_SLOT_INVALID", showcase, func(p *ExtractPayload) { p.Slots["footer"] = ExtractSlot{NodeIDs: []string{"n_h1"}} }},
		{"slot overlap", "COMPONENT_EXTRACT_SLOT_INVALID", showcase, func(p *ExtractPayload) {
			p.Slots["other"] = ExtractSlot{NodeIDs: []string{"n_note", "n_btn"}}
		}},
		{"slot max", "COMPONENT_EXTRACT_SLOT_INVALID", showcase, func(p *ExtractPayload) {
			p.Slots["footer"] = ExtractSlot{NodeIDs: []string{"n_note", "n_btn"}, Max: &one}
		}},
		{"slot types", "COMPONENT_EXTRACT_SLOT_INVALID", showcase, func(p *ExtractPayload) {
			p.Slots["footer"] = ExtractSlot{NodeIDs: []string{"n_btn"}, AllowedTypes: []string{"Link"}}
		}},
		{"empty slot min", "COMPONENT_EXTRACT_SLOT_INVALID", showcase, func(p *ExtractPayload) {
			p.Slots["badge"] = ExtractSlot{At: &SlotPosition{ParentID: "n_stack"}, Min: &one}
		}},
		{"empty slot place", "COMPONENT_EXTRACT_SLOT_INVALID", showcase, func(p *ExtractPayload) {
			p.Slots["badge"] = ExtractSlot{At: &SlotPosition{ParentID: "n_h1"}, Min: &zero}
		}},
		{"empty slot index", "COMPONENT_EXTRACT_SLOT_INVALID", showcase, func(p *ExtractPayload) {
			p.Slots["badge"] = ExtractSlot{At: &SlotPosition{ParentID: "n_stack", Index: 9}}
		}},
		{"slot name", "COMPONENT_EXTRACT_SLOT_INVALID", showcase, func(p *ExtractPayload) { p.Slots["1x"] = p.Slots["footer"]; delete(p.Slots, "footer") }},
		{"enum subset", "COMPONENT_EXTRACT_INPUT_INVALID", showcase, func(p *ExtractPayload) {
			p.Inputs["fit"] = ExtractInput{From: []ExtractSource{{NodeID: "n_img", Prop: "fit"}}, Def: map[string]any{"values": []any{"stretch"}}}
		}},
		{"optional ok but required flag false", "", showcase, func(p *ExtractPayload) { p.Inputs["title"] = ExtractInput{From: p.Inputs["title"].From, Required: &no} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := showcasePayload()
			if c.edit != nil {
				c.edit(&p)
			}
			page := parse(t, c.src)
			orig := toJSON(t, page)
			_, err := Extract(page, p, builtinEnv())
			if got := extractCode(err); got != c.code {
				t.Fatalf("code %q, want %q (%v)", got, c.code, err)
			}
			if c.code != "" && toJSON(t, page) != orig {
				t.Fatal("rejected extraction must not change the page")
			}
		})
	}
}

func TestExtractRepeatItemsSTU404(t *testing.T) {
	src := `{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{
	 "n_root":{"id":"n_root","type":"Box","children":["n_list"]},
	 "n_list":{"id":"n_list","type":"Stack","children":["n_rep"]},
	 "n_rep":{"id":"n_rep","type":"Repeat","props":{"key":"$item.id"},"bindings":{"items":"$data.featured.items"},"children":["n_card"]},
	 "n_card":{"id":"n_card","type":"Text","bindings":{"text":"$item.title"}}}}`
	p := ExtractPayload{NodeID: "n_list", Name: "List", Inputs: map[string]ExtractInput{
		"products": {From: []ExtractSource{{NodeID: "n_rep", Prop: "items"}}, Def: map[string]any{"of": productList["of"]}},
	}}
	page := parse(t, src)
	r, err := Extract(page, p, builtinEnv())
	if err != nil {
		t.Fatal(err)
	}
	if got := toJSON(t, r.Component["nodes"].(map[string]any)["n_rep"].(map[string]any)["bindings"]); got != `{"items":"$props.products"}` {
		t.Errorf("repeat binding: %s", got)
	}
	// Без контракта элемента список не выводится.
	p.Inputs["products"] = ExtractInput{From: p.Inputs["products"].From}
	if _, err := Extract(parse(t, src), p, builtinEnv()); extractCode(err) != "COMPONENT_EXTRACT_INPUT_INVALID" {
		t.Fatal(err)
	}
	// $item внутри Repeat поддерева — внутренняя область; Repeat вне поддерева — внешняя.
	inner := ExtractPayload{NodeID: "n_card", Name: "Card"}
	if _, err := Extract(parse(t, src), inner, builtinEnv()); extractCode(err) != "COMPONENT_EXTRACT_EXTERNAL_SCOPE" {
		t.Fatal(err)
	}
	inner.Inputs = map[string]ExtractInput{"title": {From: []ExtractSource{{NodeID: "n_card", Prop: "text"}}}}
	page = parse(t, src)
	if _, err := Extract(page, inner, builtinEnv()); err != nil {
		t.Fatal(err)
	}
	if got := toJSON(t, page["nodes"].(map[string]any)["n_inst"].(map[string]any)["bindings"]); got != `{"title":"$item.title"}` {
		t.Errorf("instance keeps $item: %s", got)
	}
}

func TestRevertExtractMismatchSTU408(t *testing.T) {
	page := parse(t, showcase)
	r, err := Extract(page, showcasePayload(), builtinEnv())
	if err != nil {
		t.Fatal(err)
	}
	changed := Clone(page)
	changed["nodes"].(map[string]any)["n_inst"].(map[string]any)["props"] = map[string]any{"x": "y"}
	if _, _, err := RevertExtract(changed, r.Inverse.Payload); extractCode(err) != "UNDO_STATE_MISMATCH" {
		t.Fatalf("instance change: %v", err)
	}
	moved := Clone(page)
	moved["nodes"].(map[string]any)["n_root"].(map[string]any)["children"] = []any{"n_inst", "n_top"}
	if _, _, err := RevertExtract(moved, r.Inverse.Payload); extractCode(err) != "UNDO_STATE_MISMATCH" {
		t.Fatalf("position change: %v", err)
	}
	slot := Clone(page)
	slot["nodes"].(map[string]any)["n_btn"].(map[string]any)["props"] = map[string]any{"variant": "primary"}
	if _, _, err := RevertExtract(slot, r.Inverse.Payload); extractCode(err) != "UNDO_STATE_MISMATCH" {
		t.Fatalf("slot change: %v", err)
	}
}

func TestExtractJournalReplaySTU409(t *testing.T) {
	page := parse(t, showcase)
	r, err := Extract(page, showcasePayload(), builtinEnv())
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := json.Marshal(r.Payload)
	// Клиентский (неканонический) payload через общий реестр не применяется.
	if _, err := Apply(parse(t, showcase), op(ComponentExtract, `{"nodeId":"n_grid","name":"X"}`), nil); extractCode(err) != "PAYLOAD_INVALID" {
		t.Fatal(err)
	}
	after, res := roundTrip(t, showcase, Op{Type: ComponentExtract, Payload: canonical}, nil)
	if toJSON(t, after) != toJSON(t, page) || !reflect.DeepEqual(res.Before, r.Before) {
		t.Fatal("journal replay must match the first application")
	}
	right, ok := Right(ComponentExtract)
	if !ok || right != "component.write" {
		t.Fatal(right)
	}
}

func TestPlanExtractSTU401(t *testing.T) {
	plan := PlanExtract(parse(t, showcase), "n_grid", builtinEnv(), false)
	if len(plan.Blockers) != 0 || plan.EffectiveMode != "SYSTEM" {
		t.Fatalf("blockers: %v", plan.Blockers)
	}
	byProp := map[string]ExtractCandidate{}
	for _, c := range plan.Candidates {
		byProp[c.NodeID+"."+c.Prop] = c
	}
	if c := byProp["n_img.src"]; !c.External || c.Current != "content" || !c.Restricted || c.Expression != nil {
		t.Errorf("content candidate: %+v", c)
	}
	if c := byProp["n_h1.text"]; c.External || c.Current != "local" || !c.Content || c.SuggestedName != "text" {
		t.Errorf("local candidate: %+v", c)
	}
	if c := byProp["n_note.text"]; c.SuggestedName != "text2" {
		t.Errorf("suggested name: %+v", c)
	}
	if c := byProp["n_h1.level"]; c.Current != "literal" {
		t.Errorf("literal candidate: %+v", c)
	}
	keys := []string{}
	for _, k := range plan.LocalKeys {
		keys = append(keys, k.Key)
	}
	if !slices.Equal(keys, []string{"t_cta", "t_desc", "t_note", "t_title"}) {
		t.Errorf("local keys: %v", keys)
	}
	if len(plan.SlotPositions) != 2 || len(plan.SlotRanges) != 2 || len(plan.SourceListTypes) != 2 {
		t.Errorf("slots: %+v %+v %d", plan.SlotPositions, plan.SlotRanges, len(plan.SourceListTypes))
	}
	// Корень и защищённые места — блокеры.
	if p := PlanExtract(parse(t, showcase), "n_root", builtinEnv(), true); len(p.Blockers) != 1 || p.Blockers[0].Code != "COMPONENT_EXTRACT_ROOT_DENIED" {
		t.Fatalf("root: %v", p.Blockers)
	}
	ref := strings.Replace(showcase, `"type":"Button",`, `"type":"Button","on":{"click":{"action":"scrollTo","args":{"target":{"lit":"n_top"}}}},`, 1)
	if p := PlanExtract(parse(t, ref), "n_grid", builtinEnv(), true); len(p.Blockers) != 1 || p.Blockers[0].Code != "COMPONENT_EXTRACT_NODE_REF" {
		t.Fatalf("node ref: %v", p.Blockers)
	}
	zone := strings.Replace(showcase, `"type":"Box",`, `"type":"Box","zone":{"id":"z","mode":"FREE"},`, 1)
	if p := PlanExtract(parse(t, zone), "n_grid", builtinEnv(), true); p.EffectiveMode != "FREE" {
		t.Fatalf("mode: %s", p.EffectiveMode)
	}
}

// Извлечение любого некорневого узла без входов и отмена возвращают документ в точности.
func TestExtractRevertPropertySTU408(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		doc := randomDoc(t)
		var ids []string
		for id := range doc["nodes"].(map[string]any) {
			if id != doc["root"] {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return
		}
		slices.Sort(ids)
		id := rapid.SampledFrom(ids).Draw(t, "node")
		orig := mustRaw(doc)
		r, err := Extract(doc, ExtractPayload{NodeID: id, Name: "C"}, ExtractEnv{ComponentID: "0192f1c5-0000-7000-8000-000000000001"})
		if err != nil {
			t.Fatalf("extract %s: %v", id, err)
		}
		if _, _, err := RevertExtract(doc, r.Inverse.Payload); err != nil {
			t.Fatal(err)
		}
		if string(mustRaw(doc)) != string(orig) {
			t.Fatalf("revert differs\n%s\n%s", orig, mustRaw(doc))
		}
	})
}
