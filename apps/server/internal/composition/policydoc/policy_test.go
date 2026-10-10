package policydoc

import (
	"encoding/json"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
	"strings"
	"testing"
)

func parse(s string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		panic(err)
	}
	return m
}

var app = parse(`{"breakpoints":{"md":768},"tokens":{"spacing":{"sm":"8px"},"colors":{"primary":"#112233"},"radius":{"sm":"4px"},"container":{"wide":"1200px"},"borderWidth":{"thin":"1px"},"shadow":{"soft":"none"},"layer":{"modal":100},"transition":{"fast":"none"},"typography":{"body":{"fontWeight":400}}},"components":{"Native":{"container":true}},"primitives":{"Custom":{"container":true}}}`)

func document() map[string]any {
	return parse(`{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{"n_root":{"id":"n_root","type":"Box"}}}`)
}
func has(r Result, code string) bool {
	for _, d := range r.Diagnostics {
		if string(d.Code) == code {
			return true
		}
	}
	return false
}

// DS-020/IR-063: modes are inherited and capped, not supplied by the client per node.
func TestModesTypesAndComponents(t *testing.T) {
	for _, tt := range []struct {
		policy, zone, typ, code string
		project                 string
		certified               bool
		required                Mode
	}{
		{typ: "Box", project: `{}`},
		{policy: `{"mode":"FREE"}`, typ: "Box", project: `{}`, code: "POLICY_MODE_EXCEEDS_PARENT"},
		{policy: `{"mode":"FREE"}`, typ: "Box", project: `{"maxMode":"FREE"}`},
		{zone: `{"id":"z","mode":"CODE"}`, typ: "Box", project: `{"maxMode":"FREE"}`, code: "POLICY_MODE_EXCEEDS_PARENT"},
		{policy: `{"mode":"STRICT"}`, typ: "Box", project: `{}`, code: "POLICY_TYPE_MODE_REQUIRED"},
		{policy: `{"mode":"STRICT"}`, typ: "Native", project: `{}`},
		{policy: `{"mode":"STRICT"}`, typ: "Composed", project: `{}`, required: System, code: "POLICY_COMPONENT_MODE_REQUIRED"},
		{policy: `{"mode":"STRICT"}`, typ: "Composed", project: `{}`, required: Free, certified: true},
		{policy: `{"mode":"STRICT","allowedTypes":["component-id"]}`, typ: "Composed", project: `{}`, required: Free},
		{policy: `{"allowedTypes":["Native"]}`, typ: "Box", project: `{}`, code: "POLICY_TYPE_FORBIDDEN"},
		{policy: `{"allowedTypes":["Box"],"deniedTypes":["Box"]}`, typ: "Box", project: `{}`, code: "POLICY_TYPE_FORBIDDEN"},
		{typ: "Box", project: `{"defaultMode":"INVALID"}`, code: "POLICY_MODE_INVALID"},
		{typ: "Box", project: `{"maxMode":"INVALID"}`, code: "POLICY_MODE_INVALID"},
		{typ: "Box", project: `{"defaultMode":"FREE","maxMode":"SYSTEM"}`, code: "POLICY_MODE_EXCEEDS_MAX"},
	} {
		t.Run(tt.policy+tt.zone+tt.typ+tt.project, func(t *testing.T) {
			d := document()
			n := obj(obj(d["nodes"])["n_root"])
			n["type"] = tt.typ
			n["ref"] = map[string]any{"component": "component-id"}
			if tt.policy != "" {
				d["policy"] = parse(tt.policy)
			}
			if tt.zone != "" {
				n["zone"] = parse(tt.zone)
			}
			r := Validate(d, app, Options{Project: parse(tt.project), ResolveComponent: func(map[string]any) (Component, bool) { return Component{tt.required, tt.certified}, true }})
			if tt.code == "" && !r.Valid || tt.code != "" && !has(r, tt.code) {
				t.Fatal(r)
			}
		})
	}
	d := document()
	n := obj(obj(d["nodes"])["n_root"])
	n["on"] = parse(`{"click":[{"action":"pending:id"}]}`)
	if !has(Validate(d, nil, Options{}), "POLICY_PENDING_REQUIRES_CODE") {
		t.Fatal("pending accepted")
	}
	if r := Validate(d, nil, Options{Project: parse(`{"defaultMode":"CODE"}`)}); !r.Valid || r.RequiredMode != Code {
		t.Fatal(r)
	}
	d["policy"] = parse(`{"allowedTypes":["Box"]}`)
	n["children"] = []any{"n_child"}
	obj(d["nodes"])["n_child"] = parse(`{"id":"n_child","type":"Native","zone":{"id":"nested","mode":"SYSTEM","allowedTypes":["Native"]}}`)
	if !has(Validate(d, app, Options{}), "POLICY_TYPE_FORBIDDEN") {
		t.Fatal("ancestor allowlist lost")
	}
}

// DS-020: exercise the catalogue with meaningful accepted and rejected values.
func TestDesignCatalogue(t *testing.T) {
	for _, tt := range []struct {
		typ, parent, key, value string
		mode                    Mode
		valid                   bool
	}{
		{"Box", "", "padding", `"sm"`, System, true}, {"Box", "", "padding", `"missing"`, System, false},
		{"Box", "", "padding", `{"raw":"32rem"}`, Free, true}, {"Box", "", "padding", `{"raw":"32.01rem"}`, Free, false},
		{"Box", "", "padding", `{"raw":"8px"}`, System, false}, {"Native", "", "padding", `"sm"`, Strict, false},
		{"Native", "", "width", `"full"`, Strict, true}, {"Box", "", "width", `{"raw":"101%"}`, Free, false},
		{"Box", "", "height", `{"raw":"80vh"}`, Free, true}, {"Box", "", "marginTop", `{"raw":"-256px"}`, Free, true},
		{"Box", "", "marginLeft", `"auto"`, Free, true}, {"Box", "", "marginLeft", `"sm"`, System, false},
		{"Box", "", "minHeight", `"screen"`, System, true}, {"Box", "", "minWidth", `"screen"`, System, false},
		{"Box", "", "maxWidth", `"wide"`, System, true}, {"Image", "", "aspectRatio", `"16:9"`, System, true},
		{"Image", "", "aspectRatio", `{"raw":"32:1"}`, Free, true}, {"Image", "", "aspectRatio", `{"raw":"33:0"}`, Free, false},
		{"Grid", "", "columns", `12`, System, true}, {"Grid", "", "columns", `13`, System, false},
		{"Grid", "", "columns", `["1fr","auto"]`, System, true}, {"Grid", "", "columns", `["13fr"]`, System, false},
		{"Grid", "", "rows", `[{"raw":"minmax(100px, 2fr)"}]`, Free, true},
		{"Grid", "", "rows", `[{"raw":"repeat(auto-fill, minmax(100px, 1fr))"}]`, Free, true},
		{"Grid", "", "rows", `[{"raw":"repeat(auto-fill, minmax(100px, 1fr))"}]`, System, false},
		{"Grid", "", "rows", `[{"raw":"url(evil)"}]`, Free, false},
		{"Text", "Grid", "colSpan", `"full"`, System, true}, {"Text", "Grid", "order", `-10`, System, true},
		{"Text", "Grid", "colStart", `13`, Free, true}, {"Text", "Box", "colStart", `1`, Free, false},
		{"Box", "", "background", `{"raw":"#aabbccff"}`, Free, true}, {"Box", "", "color", `{"raw":"red"}`, Free, false},
		{"Text", "", "typography", `"body"`, System, true}, {"Text", "", "fontWeight", `400`, Free, true},
		{"Text", "", "fontWeight", `600`, Free, false}, {"Text", "", "lineClamp", `1.5`, System, false},
		{"Box", "", "opacity", `0.95`, Free, true}, {"Box", "", "opacity", `0.91`, Free, false},
		{"Box", "", "zIndex", `"modal"`, Free, true}, {"Box", "", "zIndex", `{"raw":100}`, Free, false},
		{"Box", "", "position", `"fixed"`, Code, false}, {"Box", "", "position", `"absolute"`, Free, true},
		{"Box", "", "overflow", `"auto"`, Free, true}, {"Box", "", "overflow", `"auto"`, System, false},
		{"Box", "", "hidden", `true`, System, true}, {"Box", "", "hidden", `1`, System, false},
		{"Box", "", "transform", `"none"`, Code, false}, {"Text", "", "gap", `"sm"`, System, false},
		{"Custom", "", "padding", `"sm"`, System, true},
		{"Box", "", "padding", `{"base":"sm","md":"sm"}`, System, true}, {"Box", "", "padding", `{"base":"sm","lg":"sm"}`, System, false},
		{"Box", "", "top", `{"raw":"1px"}`, Free, false}, {"Button", "", "outline", `"primary"`, System, false},
	} {
		t.Run(tt.key+tt.value+tt.mode.String()+tt.parent, func(t *testing.T) {
			d := document()
			n := obj(obj(d["nodes"])["n_root"])
			n["type"] = tt.typ
			n["design"] = parse(`{"` + tt.key + `":` + tt.value + `}`)
			if tt.parent != "" {
				d["root"] = "n_parent"
				obj(d["nodes"])["n_parent"] = map[string]any{"id": "n_parent", "type": tt.parent, "children": []any{"n_root"}}
			}
			r := Validate(d, app, Options{Project: map[string]any{"defaultMode": tt.mode.String(), "allowRawColors": true}})
			if r.Valid != tt.valid {
				t.Fatal(r)
			}
		})
	}
	d := document()
	n := obj(obj(d["nodes"])["n_root"])
	n["type"] = "Button"
	n["design"] = parse(`{"states":{"focusVisible":{"outline":"primary"},"hover":{"background":"primary"}}}`)
	if r := Validate(d, app, Options{}); !r.Valid {
		t.Fatal(r)
	}
	n["design"] = parse(`{"states":{"hover":{"color":{"base":"primary"}}}}`)
	if !has(Validate(d, app, Options{}), "DESIGN_STATE_RESPONSIVE_FORBIDDEN") {
		t.Fatal("responsive state")
	}
	n["design"] = parse(`{"position":"relative","top":{"raw":"-1%"}}`)
	if r := Validate(d, app, Options{Project: parse(`{"defaultMode":"FREE"}`)}); !r.Valid {
		t.Fatal(r)
	}
	n["design"] = parse(`{"color":{"raw":"#aabbcc"}}`)
	if Validate(d, app, Options{Project: parse(`{"defaultMode":"FREE"}`)}).Valid {
		t.Fatal("raw color without consent")
	}
}

// CHG-011: the structural guard covers a lock itself, descendants, slots and relocations.
func TestProtectedStructure(t *testing.T) {
	before := parse(`{"root":"n_root","nodes":{"n_root":{"type":"Box","children":["n_lock","n_other"]},"n_lock":{"type":"Box","locked":true,"slots":{"body":["n_child"]}},"n_child":{"type":"Text"},"n_other":{"type":"Box"}}}`)
	for _, change := range []func(map[string]any){
		func(d map[string]any) { delete(obj(d["nodes"]), "n_lock") },
		func(d map[string]any) { obj(obj(d["nodes"])["n_lock"])["locked"] = false },
		func(d map[string]any) { obj(obj(d["nodes"])["n_child"])["type"] = "Image" },
		func(d map[string]any) { obj(obj(d["nodes"])["n_root"])["children"] = []any{"n_other", "n_lock"} },
		func(d map[string]any) { d["policy"] = parse(`{"mode":"STRICT"}`) },
	} {
		after := ops.Clone(before)
		change(after)
		if CheckEdit(before, after, false).Valid {
			t.Fatal("guard missed change")
		}
		if !CheckEdit(before, after, true).Valid {
			t.Fatal("manager denied")
		}
	}
	after := ops.Clone(before)
	obj(obj(after["nodes"])["n_child"])["props"] = map[string]any{"text": "editable"}
	if !CheckEdit(before, after, false).Valid {
		t.Fatal("content blocked")
	}
	after = ops.Clone(before)
	obj(obj(after["nodes"])["n_lock"])["zone"] = parse(`{"locked":true}`)
	delete(obj(obj(after["nodes"])["n_lock"]), "locked")
	if CheckEdit(before, after, false).Valid {
		t.Fatal("zone changed")
	}
	// Moving an unlocked ancestor also moves its locked descendant.
	before = parse(`{"root":"n_root","nodes":{"n_root":{"type":"Box","children":["n_parent","n_other"]},"n_parent":{"type":"Box","children":["n_lock"]},"n_lock":{"type":"Box","locked":true},"n_other":{"type":"Box"}}}`)
	after = ops.Clone(before)
	obj(obj(after["nodes"])["n_root"])["children"] = []any{"n_other", "n_parent"}
	if CheckEdit(before, after, false).Valid {
		t.Fatal("locked descendant moved with ancestor")
	}
}
func TestRawGrammar(t *testing.T) {
	for _, s := range []string{"calc(1px)", "1.00001px", "NaNpx", "1e2px", "-1px", "4001px", "1em", "1px!important"} {
		if length(s, "px", 0, 4000) {
			t.Fatal(s)
		}
	}
	if !length("1.25rem", "rem", 0, 20) {
		t.Fatal("rem")
	}
	if strings.Contains(Code.String(), " ") {
		t.Fatal("mode")
	}
	if n, ok := number(json.Number("12")); !ok || n != 12 {
		t.Fatal(n)
	}
	if _, ok := number(json.Number("bad")); ok {
		t.Fatal("number")
	}
	if _, ok := number(true); ok {
		t.Fatal("number")
	}
}

// DS-020: project limits may narrow raw ranges but never widen them.
func TestProjectLimitsAndSlots(t *testing.T) {
	for _, limits := range []string{`{"padding":{"max":1024}}`, `{"padding":{"min":-1}}`, `{"padding":{"min":20,"max":10}}`, `{"padding":{"max":"ten"}}`, `{"padding":{"other":10}}`, `{"color":{"max":10}}`, `{"unknown":{"max":1}}`, `{"padding":false}`} {
		d := document()
		if r := Validate(d, app, Options{Project: parse(`{"limits":` + limits + `}`)}); !has(r, "POLICY_LIMIT_INVALID") {
			t.Fatal(r)
		}
	}
	d := document()
	obj(obj(d["nodes"])["n_root"])["design"] = parse(`{"padding":{"raw":"11px"}}`)
	if r := Validate(d, app, Options{Project: parse(`{"defaultMode":"FREE","limits":{"padding":{"min":0,"max":10}}}`)}); r.Valid {
		t.Fatal(r)
	}
	obj(obj(d["nodes"])["n_root"])["design"] = parse(`{"padding":{"raw":"10px"}}`)
	if r := Validate(d, app, Options{Project: parse(`{"defaultMode":"FREE","limits":{"padding":{"min":0,"max":10}}}`)}); !r.Valid {
		t.Fatal(r)
	}
	obj(obj(d["nodes"])["n_root"])["slots"] = map[string]any{"content": []any{"n_child"}}
	obj(d["nodes"])["n_child"] = parse(`{"type":"Composed","ref":{"component":"missing"},"zone":{"id":"z","mode":"CODE"}}`)
	if r := Validate(d, app, Options{Project: parse(`{"defaultMode":"FREE"}`), ResolveComponent: func(map[string]any) (Component, bool) { return Component{}, false }}); !has(r, "POLICY_MODE_EXCEEDS_PARENT") {
		t.Fatal(r)
	}
	if !nativeContainer(app, "Custom") || nativeContainer(app, "Unknown") {
		t.Fatal("containers")
	}
	c := checker{app: app}
	if c.tracks([]any{}, policy{}) || c.tracks([]any{true}, policy{}) || c.tracks([]any{map[string]any{"raw": false}}, policy{mode: Free}) {
		t.Fatal("tracks")
	}
	if c.value("padding", catalogue["padding"], map[string]any{"raw": 10}, policy{mode: Free}) || c.value("aspectRatio", catalogue["aspectRatio"], map[string]any{"raw": "a:b"}, policy{mode: Free}) {
		t.Fatal("raw")
	}
	if c.numeric(catalogue["opacity"], "1") || c.numeric(rule{}, 1) {
		t.Fatal("number")
	}
	if n, ok := number(1); !ok || n != 1 {
		t.Fatal(n)
	}
}

func TestDiagnosticContractAndExtendedTypography(t *testing.T) {
	d := document()
	d["policy"] = parse(`{"mode":"FREE"}`)
	r := Validate(d, app, Options{})
	if len(r.Diagnostics) != 1 || r.Diagnostics[0].Pointer != "/policy" || r.Diagnostics[0].Params["maxMode"] != "SYSTEM" || r.Diagnostics[0].Severity != "error" {
		t.Fatal(r)
	}
	d = document()
	n := obj(obj(d["nodes"])["n_root"])
	n["type"] = "Label"
	n["design"] = parse(`{"typography":"body","textAlign":"center"}`)
	if r := Validate(d, app, Options{}); !r.Valid {
		t.Fatal(r)
	}
	extended := ops.Clone(app)
	obj(extended["primitives"])["CustomText"] = parse(`{"group":"Typography"}`)
	n["type"] = "CustomText"
	if r := Validate(d, extended, Options{}); !r.Valid {
		t.Fatal(r)
	}
	for _, settings := range []string{`{"limits":false}`, `{"allowRawColors":"true"}`} {
		if r := Validate(document(), app, Options{Project: parse(settings)}); r.Valid {
			t.Fatal(r)
		}
	}
}
