package policydoc

import (
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
	"testing"
)

// STU-251: behavior, inherited locks, ancestor visibility, both snapshots.
func TestLockedBehaviorSTU251(t *testing.T) {
	before := parse(`{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{
    "n_root":{"id":"n_root","type":"Stack","children":["n_lock","n_free"]},
    "n_lock":{"id":"n_lock","type":"Box","locked":true,"children":["n_child"]},
    "n_child":{"id":"n_child","type":"Button"},"n_free":{"id":"n_free","type":"Button"}}}`)
	for _, id := range []string{"n_lock", "n_child", "n_root", "n_free"} {
		for _, field := range []string{"on", "when", "props"} {
			after := ops.Clone(before)
			obj(obj(after["nodes"])[id])[field] = parse(`{"changed":true}`)
			want := field != "props" && (id == "n_lock" || id == "n_child" || id == "n_root" && field == "when")
			if got := CheckEdit(before, after, false); got.Valid == want {
				t.Fatalf("%s %s %+v", id, field, got)
			}
			if !CheckEdit(before, after, true).Valid {
				t.Fatal("manager denied")
			}
			if got := CheckEdit(after, before, false); got.Valid == want {
				t.Fatalf("reverse %s %s %+v", id, field, got)
			}
		}
	}
	after := ops.Clone(before)
	delete(obj(obj(after["nodes"])["n_lock"]), "locked")
	obj(obj(after["nodes"])["n_child"])["on"] = parse(`{"click":{"action":"scrollTo"}}`)
	if CheckEdit(before, after, false).Valid {
		t.Fatal("unlock bypass")
	}
}

func TestLockedBehaviorDependenciesSTU241251(t *testing.T) {
	before := parse(`{"root":"n_root","dataSources":{"items":{"source":"test.items"}},"content":{"schema":"Article"},"nodes":{
    "n_root":{"id":"n_root","type":"Stack","when":{"op":"exists","arg":"$data.items"},"children":["n_lock"]},
    "n_lock":{"id":"n_lock","type":"Box","locked":true}}}`)
	after := ops.Clone(before)
	delete(after, "dataSources")
	if CheckEdit(before, after, false).Valid {
		t.Fatal("ancestor when source bypass")
	}
	obj(obj(before["nodes"])["n_root"])["when"] = parse(`{"op":"exists","arg":"$content.title"}`)
	after = ops.Clone(before)
	delete(after, "content")
	if CheckEdit(before, after, false).Valid {
		t.Fatal("ancestor primary bypass")
	}
	delete(obj(obj(before["nodes"])["n_root"]), "when")
	obj(obj(before["nodes"])["n_lock"])["on"] = parse(`{"click":{"action":"test.use","args":{"value":"$content.title"}}}`)
	after = ops.Clone(before)
	delete(after, "content")
	if CheckEdit(before, after, false).Valid {
		t.Fatal("locked primary action bypass")
	}
	obj(obj(before["nodes"])["n_lock"])["on"] = parse(`{"click":{"action":"test.use","args":{"value":{"lit":"$content.title"}}}}`)
	after = ops.Clone(before)
	delete(after, "content")
	if !CheckEdit(before, after, false).Valid {
		t.Fatal("literal counted as dependency")
	}
	delete(obj(obj(before["nodes"])["n_lock"]), "on")
	obj(obj(before["nodes"])["n_lock"])["bindings"] = parse(`{"text":"$content.title"}`)
	after = ops.Clone(before)
	delete(after, "content")
	if !CheckEdit(before, after, false).Valid {
		t.Fatal("ordinary content protection expanded")
	}
}
