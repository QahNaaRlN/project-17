package manifestdoc

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"pgregory.net/rapid"
)

func load(t *testing.T, path string) object {
	t.Helper()
	b, err := os.ReadFile("../../../../../packages/" + path)
	if err != nil {
		t.Fatal(err)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v object
	if err = d.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}
func fixture(t *testing.T) (object, object) {
	return load(t, "ir/fixtures/valid/minimal.json"), load(t, "manifest/fixtures/valid/store.json")
}
func root(doc object) object { return asObject(asObject(doc["nodes"])["n_root"]) }
func leafDoc(doc object, name string, props object) {
	doc["nodes"] = object{"n_root": object{"id": "n_root", "type": name, "props": props}}
}
func requireCode(t *testing.T, r ir.Result, code, pointer string) {
	t.Helper()
	for _, d := range r.Diagnostics {
		if string(d.Code) == code && d.Pointer == pointer && d.NodeID == "n_root" {
			return
		}
	}
	t.Fatalf("missing %s %s: %+v", code, pointer, r)
}

// MF-003, IR-020/021/031/032/041/051/061, MF-023: validate affected documents before activation.
func TestDocumentContracts(t *testing.T) {
	t.Run("valid builtins", func(t *testing.T) {
		d, m := fixture(t)
		r := Validate(d, m, nil)
		if !r.Valid || len(r.Diagnostics) != 0 {
			t.Fatal(r)
		}
	})
	for _, value := range []string{"default", "primary", "secondary", "ghost"} {
		t.Run("Button "+value, func(t *testing.T) {
			d, m := fixture(t)
			leafDoc(d, "Button", object{"variant": value, "size": "md", "disabled": false})
			if r := Validate(d, m, nil); !r.Valid {
				t.Fatal(r)
			}
		})
	}
	t.Run("manifest not ready", func(t *testing.T) {
		d, _ := fixture(t)
		r := Validate(d, nil, nil)
		if r.Valid || r.Diagnostics[0].Code != "MANIFEST_NOT_READY" {
			t.Fatal(r)
		}
	})
	t.Run("invalid manifest", func(t *testing.T) {
		d, _ := fixture(t)
		r := Validate(d, object{}, nil)
		if r.Valid || !strings.HasPrefix(string(r.Diagnostics[0].Code), "MANIFEST_") {
			t.Fatal(r)
		}
	})
	t.Run("L1 before L3", func(t *testing.T) {
		d, m := fixture(t)
		root(d)["id"] = "different"
		r := Validate(d, m, nil)
		if r.Valid || r.Diagnostics[0].Code != ir.CodeNodeIDMismatch {
			t.Fatal(r)
		}
	})
	t.Run("unknown type", func(t *testing.T) {
		d, m := fixture(t)
		root(d)["type"] = "Missing"
		requireCode(t, Validate(d, m, nil), "TYPE_UNKNOWN", "/nodes/n_root/type")
	})
	t.Run("static and bound unknown properties", func(t *testing.T) {
		d, m := fixture(t)
		root(d)["props"] = object{"zz": true}
		root(d)["bindings"] = object{"aa": "$local.x"}
		r := Validate(d, m, nil)
		requireCode(t, r, "PROP_UNKNOWN", "/nodes/n_root/props/zz")
		requireCode(t, r, "PROP_UNKNOWN", "/nodes/n_root/bindings/aa")
		if !reflect.DeepEqual(r, Validate(d, m, nil)) {
			t.Fatal("non deterministic")
		}
	})
	t.Run("content literal", func(t *testing.T) {
		d, m := fixture(t)
		leafDoc(d, "Button", object{"label": "secret"})
		requireCode(t, Validate(d, m, nil), "PROP_CONTENT_LITERAL", "/nodes/n_root/props/label")
	})
	t.Run("fixed enum rejects unsupported variants", func(t *testing.T) {
		d, m := fixture(t)
		leafDoc(d, "Divider", object{"orientation": "diagonal"})
		requireCode(t, Validate(d, m, nil), "PROP_TYPE_MISMATCH", "/nodes/n_root/props/orientation")
	})
	t.Run("children forbidden", func(t *testing.T) {
		d, m := fixture(t)
		root(d)["type"] = "Text"
		requireCode(t, Validate(d, m, nil), "CHILDREN_NOT_ALLOWED", "/nodes/n_root/children")
	})
	t.Run("events", func(t *testing.T) {
		d, m := fixture(t)
		root(d)["on"] = object{"click": []any{object{"action": "track", "args": object{"event": object{"lit": "x"}}}}}
		requireCode(t, Validate(d, m, nil), "EVENT_UNKNOWN", "/nodes/n_root/on/click")
		root(d)["type"] = "Link"
		if r := Validate(d, m, nil); !r.Valid {
			t.Fatal(r)
		}
	})
	t.Run("unknown slot", func(t *testing.T) {
		d, m := fixture(t)
		delete(root(d), "children")
		root(d)["slots"] = object{"oops": []any{"n_text"}}
		requireCode(t, Validate(d, m, nil), "SLOT_UNKNOWN", "/nodes/n_root/slots/oops")
	})
	t.Run("native required prop and default", func(t *testing.T) {
		d, m := fixture(t)
		leafDoc(d, "ProductCard", object{})
		requireCode(t, Validate(d, m, nil), "REQUIRED_INPUT_MISSING", "/nodes/n_root/props/product")
		root(d)["bindings"] = object{"product": "$content.product"}
		if r := Validate(d, m, nil); !r.Valid {
			t.Fatal(r)
		}
	})
	t.Run("mandatory new slot invalidates empty document", func(t *testing.T) {
		d, m := fixture(t)
		leafDoc(d, "ProductCard", object{"product": object{"entityId": "0192f1c4-7a1e-7c2b-9d10-3b5f2a9e4c11"}})
		slot := asObject(asObject(asObject(asObject(m["components"])["ProductCard"])["slots"])["footer"])
		slot["min"] = float64(1)
		requireCode(t, Validate(d, m, nil), "SLOT_COUNT_OUT_OF_RANGE", "/nodes/n_root/slots/footer")
		slot["min"] = float64(0)
		if r := Validate(d, m, nil); !r.Valid {
			t.Fatal(r)
		}
	})
	t.Run("slot type and maximum", func(t *testing.T) {
		d, m := fixture(t)
		root(d)["type"] = "ProductCard"
		root(d)["bindings"] = object{"product": "$content.product"}
		delete(root(d), "children")
		root(d)["slots"] = object{"footer": []any{"n_text"}}
		slot := asObject(asObject(asObject(asObject(m["components"])["ProductCard"])["slots"])["footer"])
		slot["max"] = float64(1)
		asObject(d["nodes"])["n_second"] = object{"id": "n_second", "type": "Text"}
		root(d)["slots"] = object{"footer": []any{"n_text", "n_second"}}
		r := Validate(d, m, nil)
		requireCode(t, r, "SLOT_COUNT_OUT_OF_RANGE", "/nodes/n_root/slots/footer")
		requireCode(t, r, "SLOT_TYPE_DENIED", "/nodes/n_root/slots/footer/0")
		slot["max"] = float64(2)
		slot["allowedTypes"] = []any{"Text"}
		if r = Validate(d, m, nil); !r.Valid {
			t.Fatal(r)
		}
	})
	t.Run("custom primitive and deprecated warning", func(t *testing.T) {
		d, m := fixture(t)
		root(d)["type"] = "PriceTag"
		delete(root(d), "children")
		delete(asObject(d["nodes"]), "n_text")
		root(d)["bindings"] = object{"amount": "$local.price"}
		def := asObject(asObject(m["primitives"])["PriceTag"])
		def["deprecated"] = object{"since": "3.0.0", "message": "use another type"}
		r := Validate(d, m, nil)
		if !r.Valid || len(r.Diagnostics) != 1 || r.Diagnostics[0].Severity != ir.SeverityWarning {
			t.Fatal(r)
		}
	})
	t.Run("responsive scalar and structured object", func(t *testing.T) {
		d, m := fixture(t)
		leafDoc(d, "Stack", object{"direction": object{"base": "vertical", "md": "horizontal"}})
		if r := Validate(d, m, nil); !r.Valid {
			t.Fatal(r)
		}
		root(d)["props"] = object{"direction": object{"md": "bad", "bogus": "horizontal"}}
		r := Validate(d, m, nil)
		requireCode(t, r, "PROP_TYPE_MISMATCH", "/nodes/n_root/props/direction")
		requireCode(t, r, "PROP_TYPE_MISMATCH", "/nodes/n_root/props/direction/md")
		requireCode(t, r, "BREAKPOINT_UNKNOWN", "/nodes/n_root/props/direction/bogus")
		leafDoc(d, "Button", object{"size": object{"base": "md"}})
		requireCode(t, Validate(d, m, nil), "PROP_TYPE_MISMATCH", "/nodes/n_root/props/size")
		m["components"] = object{"Test": object{"props": object{"value": object{"type": "object", "responsive": true, "fields": object{"title": object{"type": "string", "required": true}}}}}}
		leafDoc(d, "Test", object{"value": object{"base": object{"title": "ok"}, "md": object{"title": "large"}}})
		if r := Validate(d, m, nil); !r.Valid {
			t.Fatal(r)
		}
		root(d)["props"] = object{"value": object{"title": "ambiguous"}}
		if r := Validate(d, m, nil); r.Valid {
			t.Fatal(r)
		}
	})
	t.Run("static object and node reference", func(t *testing.T) {
		d, m := fixture(t)
		m["components"] = object{"Test": object{"props": object{"value": object{"type": "object", "fields": object{"title": object{"type": "string"}}}, "target": object{"type": "nodeRef", "nodeType": "Text"}}}}
		root(d)["type"] = "Test"
		delete(root(d), "children")
		root(d)["props"] = object{"value": object{"title": "ok"}, "target": "n_root"}
		delete(asObject(d["nodes"]), "n_text")
		requireCode(t, Validate(d, m, nil), "PROP_TYPE_MISMATCH", "/nodes/n_root/props/target")
		delete(asObject(root(d)["props"]), "target")
		if r := Validate(d, m, nil); !r.Valid {
			t.Fatal(r)
		}
	})
	t.Run("slot placement", func(t *testing.T) {
		d, m := fixture(t)
		leafDoc(d, "Slot", object{"name": "footer"})
		requireCode(t, Validate(d, m, nil), "SLOT_UNKNOWN", "/nodes/n_root/props/name")
		d["kind"] = "component"
		d["slotDefs"] = object{"footer": object{}}
		if r := Validate(d, m, nil); !r.Valid {
			t.Fatal(r)
		}
	})
	t.Run("Composed context", func(t *testing.T) {
		d, m := fixture(t)
		leafDoc(d, "Composed", object{})
		root(d)["ref"] = object{"component": "0192f1c4-7a1e-7c2b-9d10-3b5f2a9e4c11", "version": "live"}
		requireCode(t, Validate(d, m, nil), "COMPONENT_NOT_FOUND", "/nodes/n_root/ref")
		resolve := func(ref map[string]any) (map[string]any, bool) {
			if ref["version"] != "live" {
				t.Fatal(ref)
			}
			return object{"kind": "component", "inputs": object{"title": object{"type": "text", "content": true, "required": true}}, "slotDefs": object{"footer": object{"min": float64(1)}}}, true
		}
		r := Validate(d, m, resolve)
		requireCode(t, r, "REQUIRED_INPUT_MISSING", "/nodes/n_root/props/title")
		requireCode(t, r, "SLOT_COUNT_OUT_OF_RANGE", "/nodes/n_root/slots/footer")
		requireCode(t, Validate(d, m, func(object) (object, bool) { return object{"kind": "page"}, true }), "COMPONENT_NOT_FOUND", "/nodes/n_root/ref")
		requireCode(t, Validate(d, m, func(object) (object, bool) { return nil, false }), "COMPONENT_NOT_FOUND", "/nodes/n_root/ref")
	})
}

func TestSharedBuiltinCatalogue(t *testing.T) {
	cat := object{}
	for _, name := range manifest.BuiltinPrimitives {
		cat[name] = manifest.BuiltinContract(name)
	}
	want := append([]string(nil), manifest.BuiltinPrimitives...)
	if !reflect.DeepEqual(keys(cat), sorted(want)) {
		t.Fatal(keys(cat))
	}
	d, m := fixture(t)
	c := checker{doc: d, app: m}
	for _, name := range keys(cat) {
		for _, prop := range keys(asObject(asObject(cat[name])["props"])) {
			schema := asObject(asObject(asObject(cat[name])["props"])[prop])
			if values := array(schema["values"]); len(values) > 0 {
				for _, value := range values {
					if !c.value(schema, value, 0) {
						t.Fatalf("%s.%s=%v", name, prop, value)
					}
				}
			}
		}
	}
	if !c.value(object{"type": "enum", "enumSource": "icons"}, "cart", 0) || c.value(object{"type": "enum", "enumSource": "icons"}, "unknown", 0) {
		t.Fatal("icons")
	}
}
func sorted(values []string) []string { // keep catalogue order independent of JSON
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
	return values
}

func TestDocumentOrderProperty(t *testing.T) {
	d, m := fixture(t)
	rapid.Check(t, func(t *rapid.T) {
		// IR-005: diagnostic order is independent of JSON key insertion order.
		n := rapid.IntRange(1, 40).Draw(t, "count")
		props := object{}
		for i := 0; i < n; i++ {
			props[strings.Repeat("x", i+1)] = true
		}
		leafDoc(d, "Button", props)
		first := Validate(d, m, nil)
		second := Validate(d, m, nil)
		if !reflect.DeepEqual(first, second) || len(first.Diagnostics) != n {
			t.Fatal(first, second)
		}
	})
}

func TestValidationDoesNotMutateInputs(t *testing.T) {
	d, m := fixture(t)
	beforeDoc, _ := json.Marshal(d)
	beforeApp, _ := json.Marshal(m)
	Validate(d, m, nil)
	afterDoc, _ := json.Marshal(d)
	afterApp, _ := json.Marshal(m)
	if !bytes.Equal(beforeDoc, afterDoc) || !bytes.Equal(beforeApp, afterApp) {
		t.Fatal("inputs changed")
	}
}

func FuzzDocumentValidation(f *testing.F) {
	app := map[string]any{"manifestVersion": "1.0", "app": map[string]any{"id": "test", "version": "1.0.0", "framework": "react"}, "irVersions": []any{"1.0"}, "tokens": map[string]any{}, "breakpoints": map[string]any{}}
	for _, seed := range []string{`{}`, `null`, `{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{"n_root":{"id":"n_root","type":"Button","props":{"variant":"primary"}}}}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		decoder := json.NewDecoder(strings.NewReader(s))
		decoder.UseNumber()
		var d any
		if err := decoder.Decode(&d); err != nil {
			return
		}
		if !reflect.DeepEqual(Validate(d, app, nil), Validate(d, app, nil)) {
			t.Fatal("non deterministic")
		}
	})
}

// IR-005: JSON key ordering follows UTF-16, as in TypeScript.
func TestUnicodeDiagnosticOrder(t *testing.T) {
	d, m := fixture(t)
	leafDoc(d, "Button", object{"😀": true, "\uffff": true})
	r := Validate(d, m, nil)
	if len(r.Diagnostics) != 2 || r.Diagnostics[0].Pointer != "/nodes/n_root/props/😀" {
		t.Fatal(r)
	}
	if compareUTF16("a", "a") != 0 || compareUTF16("b", "a") <= 0 {
		t.Fatal("ordering")
	}
}
func TestSeveralCodesAtOnePointer(t *testing.T) {
	d, m := fixture(t)
	m["components"] = object{"Test": object{"props": object{"value": object{"type": "text", "content": true, "required": true}}}}
	leafDoc(d, "Test", object{"value": nil})
	r := Validate(d, m, nil)
	if len(r.Diagnostics) != 2 || r.Diagnostics[0].Code != "PROP_CONTENT_LITERAL" || r.Diagnostics[1].Code != "REQUIRED_INPUT_MISSING" {
		t.Fatal(r)
	}
}
