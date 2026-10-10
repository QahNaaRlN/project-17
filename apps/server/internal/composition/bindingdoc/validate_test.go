package bindingdoc

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
)

func app(t *testing.T) object {
	t.Helper()
	raw, err := os.ReadFile("../../../../../packages/manifest/fixtures/valid/store.json")
	if err != nil {
		t.Fatal(err)
	}
	var m object
	if err = json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
func document(node object) object {
	node["id"] = "n_root"
	return object{"irVersion": "1.0", "kind": "page", "root": "n_root", "nodes": object{"n_root": node}, "localContent": object{"t_title": object{"type": "text", "value": object{"ru": "Title"}}, "t_num": object{"type": "number", "value": object{"ru": 4.0}}}}
}
func has(r ir.Result, code string) bool {
	for _, d := range r.Diagnostics {
		if string(d.Code) == code {
			return true
		}
	}
	return false
}
func require(t *testing.T, r ir.Result, code string) {
	t.Helper()
	if code == "" {
		if !r.Valid {
			t.Fatal(r)
		}
	} else if !has(r, code) {
		t.Fatalf("expected %s: %+v", code, r)
	}
}

// IR-040/042/043: scopes, reference depth, nested paths and required fallback types.
func TestBindings(t *testing.T) {
	for _, tc := range []struct {
		name    string
		binding any
		code    string
	}{
		{"local", "$local.t_title", ""}, {"numericFormatting", "$local.t_num", ""}, {"context", "$context.locale", ""},
		{"missing", "$local.absent", "BINDING_PATH_UNRESOLVED"}, {"syntax", "$local.t_title + 1", "BINDING_SYNTAX"},
		{"item", "$item.title", "BINDING_SCOPE_UNAVAILABLE"}, {"propsOnPage", "$props.title", "BINDING_SCOPE_UNAVAILABLE"},
		{"type", "$context.viewer.authenticated", "BINDING_TYPE_MISMATCH"}, {"leaf", "$local.t_title.nope", "BINDING_PATH_UNRESOLVED"},
		{"formatter", object{"expr": "$local.t_num", "format": object{"fn": "currency", "args": object{"code": "RUB"}}}, ""},
		{"unknownFormatter", object{"expr": "$local.t_title", "format": object{"fn": "nope"}}, "BINDING_PATH_UNRESOLVED"},
		{"formatterInput", object{"expr": "$local.t_title", "format": object{"fn": "number"}}, "BINDING_TYPE_MISMATCH"},
		{"formatterArgs", object{"expr": "$local.t_num", "format": object{"fn": "currency", "args": object{"wrong": "RUB"}}}, "BINDING_TYPE_MISMATCH"},
		{"fallback", object{"expr": "$local.t_title", "default": true}, "BINDING_TYPE_MISMATCH"},
		{"template", object{"template": "{x}", "vars": object{"x": "$local.t_title"}}, ""},
		{"templateMissing", object{"template": "{x}", "vars": object{}}, "BINDING_PATH_UNRESOLVED"},
		{"templateVar", object{"template": "{x}", "vars": object{"x": "$item.title"}}, "BINDING_SCOPE_UNAVAILABLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := document(object{"type": "Text", "bindings": object{"text": tc.binding}})
			require(t, Validate(d, app(t), Options{}), tc.code)
		})
	}
	d := document(object{"type": "Text", "bindings": object{"text": "$content.title"}})
	d["content"] = object{"schema": "Product"}
	require(t, Validate(d, app(t), Options{}), "")
	d["content"] = object{"schema": "Missing"}
	require(t, Validate(d, app(t), Options{}), "BINDING_PATH_UNRESOLVED")
	d = document(object{"type": "Text", "bindings": object{"text": "$props.title"}})
	d["kind"] = "component"
	d["inputs"] = object{"title": typ("text")}
	require(t, Validate(d, app(t), Options{}), "")
	path := "/products/:slug"
	d = document(object{"type": "Text", "bindings": object{"text": "$context.route.params.slug"}})
	require(t, Validate(d, app(t), Options{Path: &path}), "")
	require(t, Validate(d, app(t), Options{}), "BINDING_PATH_UNRESOLVED")
}

// IR-020/042/050: scopes follow children and slots; paginated results have typed items.
func TestRepeatsDataAndProvides(t *testing.T) {
	a := app(t)
	d := document(object{"type": "Repeat", "bindings": object{"items": "$data.products.items"}, "props": object{"as": "outer", "key": "id"}, "children": []any{"n_inner"}, "slots": object{"empty": []any{"n_empty"}}})
	d["dataSources"] = object{"products": object{"source": "commerce.products.list", "params": object{"collection": "$local.t_title", "limit": object{"lit": 4.0}}}}
	nodes := obj(d["nodes"])
	nodes["n_inner"] = object{"id": "n_inner", "type": "Repeat", "bindings": object{"items": "$data.products.items"}, "children": []any{"n_text"}}
	nodes["n_text"] = object{"id": "n_text", "type": "Text", "bindings": object{"text": object{"template": "{x} {y}", "vars": object{"x": "$each.outer.title", "y": "$index"}}}, "when": object{"op": "gt", "args": []any{"$item.price", object{"lit": 0.0}}}}
	nodes["n_empty"] = object{"id": "n_empty", "type": "Text", "bindings": object{"text": "$local.t_title"}}
	require(t, Validate(d, a, Options{}), "")
	obj(nodes["n_empty"])["bindings"] = object{"text": "$item.title"}
	require(t, Validate(d, a, Options{}), "BINDING_SCOPE_UNAVAILABLE")
	delete(obj(nodes["n_empty"]), "bindings")
	obj(obj(d["dataSources"])["products"])["params"] = object{"collection": "$data.products.items"}
	require(t, Validate(d, a, Options{}), "BINDING_SCOPE_UNAVAILABLE")
	obj(obj(d["dataSources"])["products"])["source"] = "missing"
	require(t, Validate(d, a, Options{}), "BINDING_PATH_UNRESOLVED")
	d = document(object{"type": "ProductCard", "slots": object{"footer": []any{"n_text"}}})
	obj(d["nodes"])["n_text"] = object{"id": "n_text", "type": "Text", "bindings": object{"text": "$context.product.title"}}
	require(t, Validate(d, a, Options{}), "")
	obj(obj(d["nodes"])["n_root"])["bindings"] = object{"product": "$context.product"}
	require(t, Validate(d, a, Options{}), "BINDING_PATH_UNRESOLVED")
	obj(obj(d["nodes"])["n_root"])["type"] = "Missing"
	require(t, Validate(d, a, Options{}), "BINDING_PATH_UNRESOLVED")
}

// IR-052/053: exact action contracts, target checks and pending capabilities.
func TestActions(t *testing.T) {
	for _, tc := range []struct {
		name string
		args object
		code string
	}{
		{"track", object{"event": object{"lit": "view"}, "props": object{"lit": object{"count": 4.0}}}, ""},
		{"missing", object{}, "ACTION_UNKNOWN"}, {"pending:request", object{}, "ACTION_PENDING_CAPABILITY"},
		{"track", object{}, "ACTION_ARGS_INVALID"}, {"track", object{"event": object{"lit": 4.0}}, "ACTION_ARGS_INVALID"},
		{"track", object{"event": "$context.viewer.authenticated"}, "BINDING_TYPE_MISMATCH"},
		{"track", object{"event": object{"lit": "x"}, "props": object{"lit": 4.0}}, "ACTION_ARGS_INVALID"},
		{"track", object{"event": object{"lit": "x"}, "props": "$context.viewer"}, ""},
		{"openModal", object{"target": object{"lit": "n_root"}}, "ACTION_ARGS_INVALID"},
		{"scrollTo", object{"target": object{"lit": "n_root"}}, ""},
		{"navigate", object{"to": object{"kind": "anchor", "node": "n_root"}}, ""},
		{"navigate", object{"to": object{"kind": "anchor", "node": "n_missing"}}, "ACTION_ARGS_INVALID"},
		{"navigate", object{"to": object{"kind": "url", "url": "https://example.com"}}, ""},
		{"navigate", object{"to": object{"kind": "url", "url": "/relative"}}, ""},
		{"navigate", object{"to": object{"kind": "url", "url": "javascript:alert(1)"}}, "ACTION_ARGS_INVALID"},
		{"navigate", object{"to": object{"lit": object{"kind": "url", "url": "not-a-url"}}}, "ACTION_ARGS_INVALID"},
		{"navigate", object{"to": object{"kind": "page", "page": "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11"}}, "ACTION_ARGS_INVALID"},
		{"data.loadMore", object{"source": object{"lit": "missing"}}, "ACTION_ARGS_INVALID"},
		{"commerce.addToCart", object{"product": object{"lit": object{"entityId": "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11"}}, "quantity": object{"lit": 2.0}}, ""},
		{"commerce.addToCart", object{"quantity": object{"lit": 100.0}}, "ACTION_ARGS_INVALID"},
	} {
		t.Run(tc.name+tc.code, func(t *testing.T) {
			d := document(object{"type": "Button", "on": object{"click": object{"action": tc.name, "args": tc.args}}})
			require(t, Validate(d, app(t), Options{}), tc.code)
		})
	}
	d := document(object{"type": "Button", "on": object{"click": []any{object{"action": "navigate", "args": object{"to": object{"kind": "page", "page": "0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11", "params": object{"slug": "$local.t_title"}}}}, object{"action": "data.loadMore", "args": object{"source": object{"lit": "products"}}}}}})
	d["dataSources"] = object{"products": object{"source": "commerce.products.list", "params": object{"collection": "$local.t_title"}}}
	path := "/products/:slug"
	opts := Options{ResolvePage: func(string) (*string, bool) { return &path, true }}
	require(t, Validate(d, app(t), opts), "")
	opts.ResolvePage = func(string) (*string, bool) { return nil, false }
	require(t, Validate(d, app(t), opts), "ACTION_ARGS_INVALID")
}

// IR-042: compatibility preserves input schema identity, enum sets and list/object shapes.
func TestCompatibility(t *testing.T) {
	for _, tc := range []struct {
		from, to object
		want     bool
	}{
		{typ("number"), typ("text"), true}, {typ("richText"), typ("text"), false}, {typ("url"), typ("link"), true},
		{object{"type": "number"}, typ("number"), false}, {object{"type": "number", "default": 0}, typ("number"), true},
		{object{"type": "reference", "schema": "A"}, object{"type": "reference", "schema": "B"}, false},
		{object{"type": "asset", "assetKind": "image"}, object{"type": "asset", "assetKind": "video"}, false},
		{object{"type": "nodeRef", "nodeType": "Modal"}, object{"type": "nodeRef", "nodeType": "Modal"}, true},
		{object{"type": "enum", "values": []any{"a"}}, object{"type": "enum", "values": []any{"a", "b"}}, true},
		{object{"type": "enum", "enumSource": "icons"}, object{"type": "enum", "enumSource": "icons"}, true},
		{object{"type": "enum", "enumSource": "icons"}, object{"type": "enum", "enumSource": "colors"}, false},
		{object{"type": "enum", "values": []any{"a", "c"}}, object{"type": "enum", "values": []any{"a", "b"}}, false},
		{object{"type": "list", "of": typ("text")}, object{"type": "list", "of": typ("number")}, false},
		{object{"type": "list", "of": typ("text")}, object{"type": "list"}, true},
		{fields(object{"x": typ("string")}), fields(object{"x": typ("string"), "y": object{"type": "number"}}), true},
		{fields(object{}), fields(object{"x": typ("string")}), false},
		{fields(object{}), fields(object{"x": object{"type": "string", "required": true, "default": "fallback"}}), true},
		{object{"type": "number", "integer": true, "min": 2.0, "max": 4.0}, object{"type": "number", "integer": true, "min": 1.0, "max": 5.0}, true},
		{typ("number"), object{"type": "number", "integer": true}, false},
		{typ("number"), object{"type": "number", "min": 1.0}, false},
		{object{"type": "number", "max": 6.0}, object{"type": "number", "max": 5.0}, false},
		{nil, typ("text"), false}, {typ("text"), nil, false},
	} {
		if got := compatible(tc.from, tc.to, 0); got != tc.want {
			t.Fatalf("%v → %v: %v", tc.from, tc.to, got)
		}
	}
	if compatible(typ("text"), typ("text"), 33) {
		t.Fatal("unbounded types")
	}
}

// IR-043: reference dereferences stop at three; unknown list fields are rejected.
func TestPathsAndBounds(t *testing.T) {
	c := checker{app: object{"schemas": object{"A": object{"fields": object{"next": object{"type": "reference", "schema": "A"}, "name": typ("text")}}}}}
	s := scopes{"content": fields(object{"ref": object{"type": "reference", "schema": "A"}, "list": object{"type": "list", "of": typ("text")}})}
	if c.path("$content.ref.next.next.name", "", "", s) == nil {
		t.Fatal(c.out)
	}
	c.path("$content.ref.next.next.next.name", "", "", s)
	if !has(ir.Result{Diagnostics: c.out}, "BINDING_DEPTH_EXCEEDED") {
		t.Fatal(c.out)
	}
	if c.path("$content.list.0", "", "", s) == nil {
		t.Fatal(c.out)
	}
	c.path("$content.list.nope", "", "", s)
	c.binding(object{}, typ("text"), "", "", s, 33)
	c.predicate(object{"op": "not"}, "", "", s, 9)
	if c.binding("$content.ref", nil, "", "", s, 0) != nil {
		t.Fatal("unknown prop")
	}
	for _, fn := range []string{"uppercase", "lowercase", "date", "number", "currency", "truncate"} {
		if builtinFormatter(fn) == nil {
			t.Fatal(fn)
		}
	}
	if builtinFormatter("missing") != nil || builtinAction("missing") != nil {
		t.Fatal("unknown builtin")
	}
	for _, v := range []any{nil, true, []any{"x"}, object{"x": true}} {
		infer(v)
	}
}

// IR-040/042: predicates and expression-vs-literal arguments are checked, including templates.
func TestPredicatesAndComponents(t *testing.T) {
	a := app(t)
	d := document(object{"type": "Composed", "ref": object{}, "bindings": object{"count": "$local.t_title"}})
	opts := Options{ResolveComponent: func(object) (object, bool) { return object{"inputs": object{"count": typ("number")}}, true }}
	require(t, Validate(d, a, opts), "BINDING_TYPE_MISMATCH")
	opts.ResolveComponent = func(object) (object, bool) { return nil, false }
	require(t, Validate(d, a, opts), "")
	for _, pred := range []object{
		{"op": "and", "args": []any{object{"op": "exists", "arg": "$item"}}},
		{"op": "not", "arg": object{"op": "empty", "arg": "$props"}},
		{"op": "eq", "args": []any{"$local.t_title", object{"lit": true}}},
		{"op": "gt", "args": []any{"$local.t_title", object{"lit": "x"}}},
		{"op": "in", "args": []any{"$local.t_title", object{"lit": []any{true}}}},
		{"op": "in", "args": []any{"$local.t_title", object{"lit": []any{"ok", true}}}},
	} {
		d = document(object{"type": "Box", "when": pred})
		if Validate(d, a, Options{}).Valid {
			t.Fatal(pred)
		}
	}
	for _, pred := range []object{{"op": "eq", "args": []any{"$local.t_title", object{"lit": "x"}}}, {"op": "eq", "args": []any{"$local.t_num", object{"lit": 4.0}}}, {"op": "in", "args": []any{"$local.t_title", object{"lit": []any{"x"}}}}} {
		d = document(object{"type": "Box", "when": pred})
		require(t, Validate(d, a, Options{}), "")
	}
	d = document(object{"type": "Image", "bindings": object{"src": object{"template": "{x}", "vars": object{"x": "$local.t_title"}}}})
	require(t, Validate(d, a, Options{}), "BINDING_TYPE_MISMATCH")
	// Required Composed input needs a fallback when its source is optional.
	d = document(object{"type": "Composed", "bindings": object{"title": object{"expr": "$props.optional", "default": "ok"}}})
	d["kind"] = "component"
	d["inputs"] = object{"optional": object{"type": "text"}}
	opts.ResolveComponent = func(object) (object, bool) { return object{"inputs": object{"title": typ("text")}}, true }
	require(t, Validate(d, a, opts), "")
	if !sameType(object{"type": "enum"}, typ("string")) {
		t.Fatal("enum equality")
	}
	if sameType(typ("asset"), typ("number")) {
		t.Fatal("different types")
	}
}

// IR-042: arbitrary JSON never panics; malformed IR is rejected by the preceding L1 stage.
func FuzzValidate(f *testing.F) {
	f.Add([]byte(`{"nodes":{},"root":"missing"}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"root":"x","nodes":{"x":{"type":"Repeat","children":["x"]}}}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		var d any
		if json.Unmarshal(raw, &d) != nil {
			return
		}
		Validate(d, object{}, Options{})
	})
}

func TestDiagnosticsPointers(t *testing.T) {
	d := document(object{"type": "Button", "on": object{"click": []any{object{"action": "pending:x"}}}})
	r := Validate(d, app(t), Options{})
	require(t, r, "ACTION_PENDING_CAPABILITY")
	if !strings.Contains(r.Diagnostics[0].Pointer, "/on/click/0/action") {
		t.Fatal(r)
	}
}

// IR-052: Link arguments to native actions use the same environment target resolver.
func TestNativeLinkArguments(t *testing.T) {
	a := app(t)
	obj(a["actions"])["custom.link"] = object{"args": object{"to": typ("link")}}
	for _, v := range []any{object{"kind": "url", "url": "/relative"}, object{"lit": object{"kind": "url", "url": "https://example.com"}}} {
		d := document(object{"type": "Button", "on": object{"click": object{"action": "custom.link", "args": object{"to": v}}}})
		require(t, Validate(d, a, Options{}), "")
	}
	d := document(object{"type": "Button", "on": object{"click": object{"action": "custom.link", "args": object{"to": object{"kind": "anchor", "node": "n_missing"}}}}})
	require(t, Validate(d, a, Options{}), "ACTION_ARGS_INVALID")
	c := checker{doc: d, app: a}
	c.link(object{"kind": "url", "url": "/x", "params": object{"x": object{"lit": "x"}}}, "", "", scopes{})
	if len(c.out) != 1 {
		t.Fatal(c.out)
	}
}

// IR-042: manifest.ParseJSON preserves JSON numbers; bounds cannot be bypassed.
func TestJSONNumberContracts(t *testing.T) {
	from := object{"type": "number", "min": json.Number("0"), "max": json.Number("9")}
	to := object{"type": "number", "min": json.Number("1"), "max": json.Number("5")}
	if compatible(from, to, 0) {
		t.Fatal("wider numeric range accepted")
	}
	if compatible(object{"type": "number", "min": json.Number("invalid")}, to, 0) {
		t.Fatal("invalid numeric range")
	}
	if infer(json.Number("2"))["integer"] != true || len(infer(json.Number("bad"))) != 0 {
		t.Fatal("number inference")
	}
	d := document(object{"type": "Text", "bindings": object{"text": object{"expr": "$local.t_num", "format": object{"fn": "number", "args": object{"minFraction": -1.0}}}}})
	require(t, Validate(d, app(t), Options{}), "BINDING_TYPE_MISMATCH")
}

// IR-042: date literals retain their declared type; equality with null is meaningful.
func TestDateAndNullPredicates(t *testing.T) {
	for _, pred := range []object{
		{"op": "eq", "args": []any{"$local.t_title", object{"lit": nil}}},
		{"op": "gt", "args": []any{"$local.t_date", object{"lit": "2026-01-01"}}},
		{"op": "lt", "args": []any{object{"lit": "2026-01-01"}, "$local.t_date"}},
		{"op": "in", "args": []any{"$local.t_date", object{"lit": []any{"2026-01-01", "2026-02-01"}}}},
	} {
		d := document(object{"type": "Box", "when": pred})
		obj(d["localContent"])["t_date"] = object{"type": "date", "value": object{"ru": "2026-01-01"}}
		require(t, Validate(d, app(t), Options{}), "")
	}
	c := checker{}
	if c.literalType("bad", typ("date"))["type"] != "string" {
		t.Fatal("invalid date accepted")
	}
}
