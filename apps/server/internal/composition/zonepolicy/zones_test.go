package zonepolicy

import (
	"encoding/json"
	"slices"
	"testing"
)

func tree() map[string]any {
	return map[string]any{"root": "n_root", "nodes": map[string]any{"n_root": map[string]any{"type": "Box", "children": []any{"n_zone", "n_other"}}, "n_zone": map[string]any{"type": "Box", "zone": map[string]any{"requiredApprovalRole": "designer", "mode": "STRICT"}, "children": []any{"n_text"}}, "n_text": map[string]any{"type": "Text"}, "n_other": map[string]any{"type": "Box"}}}
}
func clone(d map[string]any) map[string]any {
	b, _ := json.Marshal(d)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

// PUB-001: requirements survive removals, movements, parent and live dependency changes.
func TestAffected(t *testing.T) {
	cases := []struct {
		name   string
		change func(map[string]any)
		want   bool
	}{
		{"unchanged", func(map[string]any) {}, false},
		{"other", func(d map[string]any) { obj(obj(d["nodes"])["n_other"])["props"] = map[string]any{"text": "x"} }, false},
		{"child", func(d map[string]any) { obj(obj(d["nodes"])["n_text"])["props"] = map[string]any{"text": "x"} }, true},
		{"remove zone", func(d map[string]any) {
			delete(obj(d["nodes"]), "n_zone")
			delete(obj(d["nodes"]), "n_text")
			obj(obj(d["nodes"])["n_root"])["children"] = []any{"n_other"}
		}, true},
		{"remove role", func(d map[string]any) { delete(obj(obj(d["nodes"])["n_zone"]), "zone") }, true},
		{"move zone", func(d map[string]any) {
			obj(obj(d["nodes"])["n_root"])["children"] = []any{"n_other"}
			obj(obj(d["nodes"])["n_other"])["children"] = []any{"n_zone"}
		}, true},
		{"parent design", func(d map[string]any) { obj(obj(d["nodes"])["n_root"])["design"] = map[string]any{"color": "x"} }, true},
		{"document meta", func(d map[string]any) { d["meta"] = map[string]any{"title": "x"} }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := tree()
			b := clone(a)
			tc.change(b)
			r := Affected(a, b, "SYSTEM", nil, false)
			if slices.Contains(r.Roles, "designer") != tc.want || r.Strict != tc.want {
				t.Fatal(r)
			}
		})
	}
	d := tree()
	obj(obj(d["nodes"])["n_text"])["type"] = "Composed"
	obj(obj(d["nodes"])["n_text"])["ref"] = map[string]any{"version": "live"}
	if r := Affected(d, d, "SYSTEM", func(map[string]any) bool { return true }, false); len(r.Roles) != 1 || !r.Strict {
		t.Fatal(r)
	}
	d = tree()
	obj(obj(d["nodes"])["n_root"])["zone"] = map[string]any{"requiredApprovalRole": "owner"}
	d["policy"] = map[string]any{"mode": "STRICT"}
	r := Affected(d, d, "SYSTEM", nil, true)
	if !slices.Equal(r.Roles, []string{"designer", "owner"}) || !r.Strict {
		t.Fatal(r)
	}
	// Invalid cycles terminate; structural validation reports their errors separately.
	obj(obj(d["nodes"])["n_other"])["slots"] = map[string]any{"body": []any{"n_root"}}
	Affected(d, d, "SYSTEM", nil, true)
	Affected(nil, nil, "SYSTEM", nil, false)
}

// PUB-001: arbitrary decoded tree data terminates and produces sorted requirements.
func FuzzAffected(f *testing.F) {
	f.Add(`{}`)
	f.Add(`{"root":"n_root","nodes":{"n_root":{"children":["n_root"]}}}`)
	f.Fuzz(func(t *testing.T, raw string) {
		var d map[string]any
		if json.Unmarshal([]byte(raw), &d) != nil {
			return
		}
		r := Affected(nil, d, "SYSTEM", nil, true)
		if !slices.IsSorted(r.Roles) {
			t.Fatal(r)
		}
	})
}
