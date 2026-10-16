package policydoc

import "testing"

func TestCapabilityNodeModeRespectsAncestorsSlotsAndCeilings(t *testing.T) {
	doc := parse(`{"root":"n_root","nodes":{"n_root":{"id":"n_root","type":"Box","children":["n_child"],"slots":{"footer":["n_slot"]}},"n_child":{"id":"n_child","type":"Box","zone":{"mode":"FREE"}},"n_slot":{"id":"n_slot","type":"Button","zone":{"mode":"CODE"}}}}`)
	project := map[string]any{"defaultMode": "SYSTEM", "maxMode": "CODE"}
	for _, tc := range []struct {
		id    string
		mode  Mode
		found bool
	}{{"n_root", System, true}, {"n_child", Free, true}, {"n_slot", Code, true}, {"absent", Strict, false}} {
		mode, found := NodeMode(doc, tc.id, project)
		if mode != tc.mode || found != tc.found {
			t.Fatal(tc, mode, found)
		}
	}
	if _, found := NodeMode(doc, "n_slot", map[string]any{"maxMode": "FREE"}); found {
		t.Fatal("ceiling bypass")
	}
	nodes := obj(doc["nodes"])
	obj(nodes["n_root"])["children"] = []any{"n_root", "absent"}
	if _, found := NodeMode(doc, "unknown", project); found {
		t.Fatal("cycle found unknown node")
	}
}
