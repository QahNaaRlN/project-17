package codeindex

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../../../packages/manifest/fixtures/code-index.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestMetadataContract(t *testing.T) {
	raw := fixture(t)
	index, canonical, err := Parse(raw)
	if err != nil || index.Repo.Root != "." || len(index.Elements) != 1 {
		t.Fatal(index, err)
	}
	_, again, err := Parse(canonical)
	if err != nil || string(again) != string(canonical) {
		t.Fatal(err)
	}
	app := map[string]any{"actions": map[string]any{"store.favorite": map[string]any{"sourceRef": map[string]any{"file": "src/actions.ts", "export": "favorite", "line": json.Number("12")}}}}
	if !Match(index, app) {
		t.Fatal("matching source ref rejected")
	}
	source := app["actions"].(map[string]any)["store.favorite"].(map[string]any)["sourceRef"].(map[string]any)
	for _, field := range []string{"file", "export", "line"} {
		old := source[field]
		source[field] = "wrong"
		if Match(index, app) {
			t.Fatal(field)
		}
		source[field] = old
	}
	delete(source, "line")
	if !Match(index, app) {
		t.Fatal("optional line")
	}
	delete(app, "actions")
	if Match(index, app) {
		t.Fatal("missing action")
	}
	for _, path := range []string{"", "/root", "../a.ts", "a/../b.ts", "a/./b", "a//b", "a/", "C:/repo/a.ts", `a\b.ts`, "node_modules/a.ts", "Dist/a", ".git/config", ".env", "a/.env.local", "a\n.ts", strings.Repeat("x", 513)} {
		if Path(path) {
			t.Fatal(path)
		}
	}
	for _, path := range []string{"src/a.ts", "src/русский.ts", "packages/@cms/index.ts"} {
		if !Path(path) {
			t.Fatal(path)
		}
	}
	mutations := map[string]any{
		"repo.root": "/", "repo.commit": "branch", "repo.content": "private", "repo": nil,
		"conventions.registry": "../secret", "conventions.testCommand": "", "conventions.manifestCommand": "x\ny", "elements": nil,
		"elements.action:store.favorite.definition.file": "/private", "elements.action:store.favorite.definition.line": 0,
		"elements.action:store.favorite.implementation.export": "", "elements.action:store.favorite.implementation.line": 0,
		"elements.action:store.favorite.implementation.line!": nil,
		"elements.action:store.favorite.dependencies":         []string{"src/a.ts", "src/a.ts"}, "elements.action:store.favorite.tests": []string{".env"},
		"elements.action:store.favorite.source": "private code", "extra": true,
	}
	for path, value := range mutations {
		var input map[string]any
		_ = json.Unmarshal(raw, &input)
		parts := strings.Split(path, ".")
		// Element names contain a dot; use the known entry as a nested mutation root.
		var target map[string]any = input
		if strings.HasPrefix(path, "elements.action:store.favorite.") {
			target = input["elements"].(map[string]any)["action:store.favorite"].(map[string]any)
			parts = strings.Split(strings.TrimPrefix(path, "elements.action:store.favorite."), ".")
		}
		for _, key := range parts[:len(parts)-1] {
			target = target[key].(map[string]any)
		}
		target[parts[len(parts)-1]] = value
		body, _ := json.Marshal(input)
		if _, _, err := Parse(body); err == nil {
			t.Fatal(path)
		}
	}
	for _, body := range []string{"{}", "null", "[]", `{"a":1,"a":2}`, string(raw) + "{}", strings.Repeat(" ", 2*MaxBytes+1)} {
		if _, _, err := Parse([]byte(body)); err == nil {
			t.Fatal("invalid accepted")
		}
	}
	var input map[string]any
	_ = json.Unmarshal(raw, &input)
	input["elements"] = map[string]any{"other:name": input["elements"].(map[string]any)["action:store.favorite"]}
	body, _ := json.Marshal(input)
	if _, _, err := Parse(body); err == nil {
		t.Fatal("unknown kind")
	}
	value, _ := manifest.ParseJSON(canonical)
	h, err := manifest.Hash(value)
	if err != nil || !HashPattern.MatchString(h) {
		t.Fatal(h, err)
	}
}
