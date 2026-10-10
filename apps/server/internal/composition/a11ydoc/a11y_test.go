package a11ydoc

import (
	"encoding/json"
	"fmt"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"math"
	"testing"
)

func document(nodes ...map[string]any) map[string]any {
	m := map[string]any{}
	ids := []any{}
	for i, n := range nodes {
		id := fmt.Sprintf("n_%02d", i)
		n["id"] = id
		m[id] = n
		ids = append(ids, id)
	}
	m["n_root"] = map[string]any{"id": "n_root", "type": "Box", "children": ids}
	return map[string]any{"irVersion": "1.0", "kind": "page", "root": "n_root", "nodes": m}
}
func node(typ string, props map[string]any) map[string]any {
	return map[string]any{"type": typ, "props": props}
}
func has(r ir.Result, code string) bool {
	for _, d := range r.Diagnostics {
		if string(d.Code) == code {
			return true
		}
	}
	return false
}

// 03 §6: every static L7 rule has a blocking and a valid case.
func TestStaticRules(t *testing.T) {
	cases := []struct {
		code      string
		bad, good map[string]any
	}{
		{"A11Y_IMAGE_ALT_MISSING", node("Image", nil), node("Image", map[string]any{"decorative": true})},
		{"A11Y_BUTTON_LABEL", node("Button", nil), map[string]any{"type": "Button", "bindings": map[string]any{"label": "$context.locale"}}},
		{"A11Y_LINK_TARGET", node("Link", nil), node("Link", map[string]any{"to": "/"})},
		{"A11Y_VIDEO_AUTOPLAY", node("Video", map[string]any{"autoplay": true}), node("Video", map[string]any{"autoplay": true, "muted": true})},
		{"A11Y_MODAL_TITLE", node("Modal", nil), node("Modal", map[string]any{"title": "Title"})},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			r := Validate(document(tc.bad), nil, nil)
			if r.Valid || !has(r, tc.code) {
				t.Fatal(r)
			}
			if r := Validate(document(tc.good), nil, nil); !r.Valid {
				t.Fatal(r)
			}
		})
	}
	for _, v := range []any{nil, "", "  "} {
		if present(node("Image", map[string]any{"alt": v}), "alt") {
			t.Fatal(v)
		}
	}
	if !present(node("Image", map[string]any{"alt": true}), "alt") || !present(map[string]any{"bindings": map[string]any{"alt": true}}, "alt") {
		t.Fatal("presence")
	}
	for _, v := range []any{[]any{map[string]any{"action": "navigate"}}, map[string]any{"action": "navigate"}} {
		if r := Validate(document(map[string]any{"type": "Link", "on": map[string]any{"click": v}}), nil, nil); !r.Valid {
			t.Fatal(r)
		}
	}
}
func TestHeadingsHiddenAndInteractive(t *testing.T) {
	d := document(node("Heading", map[string]any{"level": 1}), node("Heading", map[string]any{"level": json.Number("1")}), node("Heading", map[string]any{"level": float64(4)}))
	r := Validate(d, nil, nil)
	if !has(r, "A11Y_HEADING_MULTIPLE_H1") || !has(r, "A11Y_HEADING_ORDER") {
		t.Fatal(r)
	}
	d["kind"] = "component"
	if r := Validate(d, nil, nil); !r.Valid || len(r.Diagnostics) != 0 {
		t.Fatal(r)
	}
	d = document(node("Heading", nil), node("Heading", map[string]any{"level": 3}))
	if r := Validate(d, nil, nil); len(r.Diagnostics) != 0 {
		t.Fatal(r)
	}
	d = document(node("Link", map[string]any{"to": "/"}), node("Button", nil), node("Icon", nil))
	ns := obj(d["nodes"])
	obj(ns["n_root"])["children"] = []any{"n_00"}
	obj(ns["n_00"])["children"] = []any{"n_01"}
	obj(ns["n_01"])["children"] = []any{"n_02"}
	r = Validate(d, nil, nil)
	if !has(r, "A11Y_ICON_ONLY_LABEL") || !has(r, "A11Y_NESTED_INTERACTIVE") {
		t.Fatal(r)
	}
	obj(ns["n_00"])["design"] = map[string]any{"hidden": true}
	if r := Validate(d, nil, nil); !r.Valid {
		t.Fatal(r)
	}
	d = document(node("Box", nil))
	obj(obj(d["nodes"])["n_00"])["children"] = []any{"missing"}
	if r := Validate(d, nil, nil); !r.Valid {
		t.Fatal(r)
	}
}

// IR-041/03 §6: page-wide checks cross Composed and Slot boundaries.
func TestCompositionInputsAndSlots(t *testing.T) {
	child := document(node("Heading", map[string]any{"level": 1}))
	child["kind"] = "component"
	parent := document(node("Heading", map[string]any{"level": 1}), map[string]any{"type": "Composed", "ref": map[string]any{"component": "a", "version": "live"}})
	resolve := func(map[string]any) (map[string]any, bool) { return child, true }
	r := Validate(parent, nil, resolve)
	if !has(r, "A11Y_HEADING_MULTIPLE_H1") || r.Diagnostics[0].Pointer != "/nodes/n_01/ref" || r.Diagnostics[0].Params["sourcePointer"] == nil {
		t.Fatal(r)
	}
	child = document(map[string]any{"type": "Button", "bindings": map[string]any{"label": "$props.caption.text"}})
	child["kind"] = "component"
	child["inputs"] = map[string]any{"caption": map[string]any{"type": "object", "default": map[string]any{"text": "Default"}}}
	parent = document(map[string]any{"type": "Composed", "ref": map[string]any{"component": "a", "version": "live"}})
	if r := Validate(parent, nil, resolve); !r.Valid {
		t.Fatal(r)
	}
	obj(obj(parent["nodes"])["n_00"])["props"] = map[string]any{"caption": map[string]any{}}
	if r := Validate(parent, nil, resolve); !has(r, "A11Y_BUTTON_LABEL") {
		t.Fatal(r)
	}
	obj(obj(parent["nodes"])["n_00"])["props"] = map[string]any{"caption": map[string]any{"text": "$context.locale"}}
	if r := Validate(parent, nil, resolve); !r.Valid {
		t.Fatal(r)
	}
	child = document(map[string]any{"type": "Slot", "props": map[string]any{"name": "body"}})
	child["kind"] = "component"
	parent = document(map[string]any{"type": "Composed", "ref": map[string]any{"component": "a", "version": "live"}, "slots": map[string]any{"body": []any{"n_01"}}}, node("Image", nil))
	obj(obj(parent["nodes"])["n_root"])["children"] = []any{"n_00"}
	if r := Validate(parent, nil, resolve); !has(r, "A11Y_IMAGE_ALT_MISSING") || r.Diagnostics[0].Pointer != "/nodes/n_01/bindings/alt" {
		t.Fatal(r)
	}
	// Unresolved/invalid children and cycles are already diagnosed by L1/L3; L7 terminates.
	child = map[string]any{}
	Validate(parent, nil, resolve)
	child = parent
	Validate(parent, nil, resolve)
	Validate(parent, nil, func(map[string]any) (map[string]any, bool) { return nil, false })
}
func TestExpansionGuard(t *testing.T) {
	for _, size := range []int{131, 10002} {
		d := document()
		ns := obj(d["nodes"])
		prev := "n_root"
		for i := 0; i < size; i++ {
			id := fmt.Sprintf("n_%02d", i)
			ns[id] = map[string]any{"id": id, "type": "Box"}
			if size < 200 {
				obj(ns[prev])["children"] = []any{id}
				prev = id
			} else {
				obj(ns["n_root"])["children"] = append(array(obj(ns["n_root"])["children"]), id)
			}
		}
		if r := Validate(d, nil, nil); !has(r, "A11Y_EXPANSION_LIMIT") {
			t.Fatal(r)
		}
	}
}
func app() map[string]any {
	return map[string]any{"breakpoints": map[string]any{"md": 768}, "tokens": map[string]any{"colors": map[string]any{"white": "#ffffff", "black": "#000000", "gray": "#777777", "soft": "#ffffff80", "themed": map[string]any{"value": "#000000", "modes": map[string]any{"dark": "#ffffff"}}}, "typography": map[string]any{"large": map[string]any{"large": true}}}}
}
func colored(fg, bg any) map[string]any {
	d := document(node("Text", nil))
	obj(obj(d["nodes"])["n_root"])["design"] = map[string]any{"background": bg}
	obj(obj(d["nodes"])["n_00"])["design"] = map[string]any{"color": fg}
	return d
}

// 03 §6 / WCAG G18: themes, responsive values, alpha and inherited states.
func TestContrast(t *testing.T) {
	if r := Validate(colored("black", "white"), app(), nil); !r.Valid {
		t.Fatal(r)
	}
	d := colored("gray", "white")
	r := Validate(d, app(), nil)
	if r.Valid || !has(r, "A11Y_CONTRAST") {
		t.Fatal(r)
	}
	obj(obj(d["nodes"])["n_00"])["design"] = map[string]any{"color": "gray", "typography": "large"}
	if r := Validate(d, app(), nil); !r.Valid {
		t.Fatal(r)
	}
	r = Validate(colored("themed", "white"), app(), nil)
	if r.Valid {
		t.Fatal(r)
	}
	r = Validate(colored(map[string]any{"base": "black", "md": "white"}, "white"), app(), nil)
	if r.Valid {
		t.Fatal(r)
	}
	r = Validate(colored(map[string]any{"raw": "#777"}, "white"), app(), nil)
	if !r.Valid || !has(r, "A11Y_CONTRAST") {
		t.Fatal(r)
	}
	for _, unknown := range []any{nil, "missing", map[string]any{"raw": "rgb(1 2 3)"}, map[string]any{"foo": "bar"}} {
		if r := Validate(colored("white", unknown), app(), nil); len(r.Diagnostics) != 0 {
			t.Fatal(r)
		}
	}
	d = colored("black", "white")
	obj(obj(d["nodes"])["n_root"])["design"] = map[string]any{"background": "white", "states": map[string]any{"hover": map[string]any{"background": "black"}}}
	r = Validate(d, app(), nil)
	if r.Valid || r.Diagnostics[0].Params["state"] != "hover" {
		t.Fatal(r)
	}
	d = colored("white", nil)
	obj(obj(d["nodes"])["n_00"])["design"] = map[string]any{"color": "white", "background": "soft"}
	if r := Validate(d, app(), nil); len(r.Diagnostics) != 0 {
		t.Fatal(r)
	}
	d = colored("white", "white")
	obj(obj(d["nodes"])["n_00"])["design"] = map[string]any{"color": "white", "background": "soft"}
	if r := Validate(d, app(), nil); r.Valid {
		t.Fatal(r)
	}
	d = colored("white", "white")
	obj(obj(d["nodes"])["n_root"])["design"] = map[string]any{"background": "white", "backgroundImage": "image"}
	if r := Validate(d, app(), nil); len(r.Diagnostics) != 0 {
		t.Fatal(r)
	}
}
func TestColorMath(t *testing.T) {
	for _, s := range []string{"#000", "#000f", "#000000", "black"} {
		if c := parseColor(s); !c.known || c.a != 1 || luminance(c) != 0 {
			t.Fatal(s, c)
		}
	}
	for _, s := range []string{"#fff", "white"} {
		if c := parseColor(s); !c.known || luminance(c) != 1 {
			t.Fatal(s, c)
		}
	}
	for _, s := range []string{"#ggg", "#00000", "var(--x)", ""} {
		if parseColor(s).known {
			t.Fatal(s)
		}
	}
	if !parseColor("red").known || parseColor("transparent").a != 0 {
		t.Fatal("named")
	}
	if r := ratio(parseColor("black"), parseColor("white")); r != 21 {
		t.Fatal(r)
	}
	if over(color{}, parseColor("white")).known || over(parseColor("#0008"), color{}).known {
		t.Fatal("unknown")
	}
	if c := over(parseColor("#0008"), parseColor("white")); !c.known || c.a != 1 {
		t.Fatal(c)
	}
	if responsive(map[string]any{"base": "black"}, "md") != "black" {
		t.Fatal("fallback")
	}
	if c := (&checker{app: app()}).color(42, ""); c.known {
		t.Fatal(c)
	}
	if integer("1") != 0 || integer(json.Number("bad")) != 0 {
		t.Fatal("integer")
	}
	// The mathematically exact boundary is tested without decimal rounding.
	level := mathColor(4.5)
	if r := ratio(level, parseColor("white")); r < 4.499999999 || r > 4.500000001 {
		t.Fatal(r)
	}

}
func mathColor(r float64) color {
	l := (1.05/r - .05)
	v := 1.055*math.Pow(l, 1/2.4) - .055
	return color{v, v, v, 1, true, false}
}

// 03 §6 / IR-041: arbitrary decoded JSON cannot panic or make diagnostics nondeterministic.
func FuzzValidate(f *testing.F) {
	for _, seed := range []string{`{}`, `{"root":"n_root","nodes":{"n_root":{"type":"Box","children":["n_root"]}}}`, `{"root":"n_root","nodes":{"n_root":{"type":"Image"}}}`, `null`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		var d map[string]any
		if json.Unmarshal([]byte(raw), &d) != nil {
			return
		}
		a := Validate(d, app(), nil)
		b := Validate(d, app(), nil)
		x, _ := json.Marshal(a)
		y, _ := json.Marshal(b)
		if string(x) != string(y) {
			t.Fatal("nondeterministic")
		}
	})
}

// 03 §6: mutually exclusive responsive headings are not counted together.
func TestResponsiveVisibility(t *testing.T) {
	d := document(node("Heading", map[string]any{"level": 1}), node("Heading", map[string]any{"level": 1}))
	ns := obj(d["nodes"])
	obj(ns["n_00"])["design"] = map[string]any{"hidden": map[string]any{"base": false, "md": true}}
	obj(ns["n_01"])["design"] = map[string]any{"hidden": map[string]any{"base": true, "md": false}}
	if r := Validate(d, app(), nil); !r.Valid {
		t.Fatal(r)
	}
	obj(ns["n_01"])["design"] = map[string]any{"hidden": map[string]any{"base": false, "md": false}}
	if r := Validate(d, app(), nil); r.Valid || !has(r, "A11Y_HEADING_MULTIPLE_H1") {
		t.Fatal(r)
	}
	d = colored("white", "white")
	obj(obj(d["nodes"])["n_00"])["design"] = map[string]any{"color": "white", "hidden": true}
	if r := Validate(d, app(), nil); !r.Valid {
		t.Fatal(r)
	}
}
