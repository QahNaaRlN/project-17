package bindingdoc

import "testing"

func TestDirectDataURLDoesNotBecomeLinkSTU224234(t *testing.T) {
	a := app(t)
	obj(a["dataSources"])["test.url"] = object{"result": object{"type": "url", "required": true}}
	d := document(object{"type": "Link", "bindings": object{"to": "$data.redirect"}})
	d["dataSources"] = object{"redirect": object{"source": "test.url"}}
	if !has(Validate(d, a, Options{}), "BINDING_TYPE_MISMATCH") {
		t.Fatal("URL binding passed publication validation as a Link object")
	}
}

func TestDataKeysAndScopesSTU223226227234(t *testing.T) {
	a := app(t)
	for _, key := range []string{"id", "$item.id"} {
		d := document(object{"type": "Repeat", "props": object{"key": key}, "bindings": object{"items": "$data.products.items"}, "children": []any{"n_item"}, "slots": object{"empty": []any{"n_empty"}}})
		d["dataSources"] = object{"products": object{"source": "commerce.products.list", "params": object{"collection": object{"lit": "summer"}}}}
		obj(d["nodes"])["n_item"] = object{"id": "n_item", "type": "Text", "bindings": object{"text": "$item.title"}}
		obj(d["nodes"])["n_empty"] = object{"id": "n_empty", "type": "Text"}
		require(t, Validate(d, a, Options{}), "")
		obj(obj(d["nodes"])["n_root"])["props"] = object{"key": "$item.image"}
		if !has(Validate(d, a, Options{}), "BINDING_TYPE_MISMATCH") {
			t.Fatal("asset key accepted")
		}
		obj(obj(d["nodes"])["n_empty"])["bindings"] = object{"text": "$item.title"}
		if !has(Validate(d, a, Options{}), "BINDING_SCOPE_UNAVAILABLE") {
			t.Fatal("empty inherited item")
		}
	}
	for _, key := range []string{"content", "constructor", "prototype", "__proto__"} {
		d := document(object{"type": "Text"})
		d["dataSources"] = object{key: object{"source": "commerce.products.list"}}
		if !has(Validate(d, a, Options{}), "BINDING_PATH_UNRESOLVED") {
			t.Fatal("reserved key", key)
		}
	}
}
