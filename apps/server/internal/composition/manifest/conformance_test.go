package manifest_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
)

const fixtures = "../../../../../packages/manifest/fixtures"

func fixtureFiles(t *testing.T, sub string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(fixtures, sub))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") && e.Name() != "hashes.json" {
			out = append(out, e.Name())
		}
	}
	return out
}

func load(t *testing.T, path string) any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtures, path))
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestConformanceValid(t *testing.T) {
	var hashes map[string]string
	data, _ := os.ReadFile(filepath.Join(fixtures, "valid", "hashes.json"))
	if err := json.Unmarshal(data, &hashes); err != nil {
		t.Fatal(err)
	}
	files := fixtureFiles(t, "valid")
	if len(files) != len(hashes) {
		t.Errorf("хэши: %d, фикстур: %d", len(hashes), len(files))
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			m := load(t, filepath.Join("valid", file))
			if r := manifest.Validate(m); !r.Valid || len(r.Diagnostics) != 0 {
				t.Errorf("диагностики: %+v", r.Diagnostics)
			}
			if got, err := manifest.Hash(m); err != nil || got != hashes[file] {
				t.Errorf("manifestHash %s, ожидался %s (%v)", got, hashes[file], err)
			}
		})
	}
}

func TestConformanceInvalid(t *testing.T) {
	seen := map[string]bool{}
	for _, file := range fixtureFiles(t, "invalid") {
		fixture := load(t, filepath.Join("invalid", file)).(map[string]any)
		t.Run(file, func(t *testing.T) {
			r := manifest.Validate(fixture["manifest"])
			if r.Valid {
				t.Fatal("manifest признан валидным")
			}
			for _, w := range fixture["expect"].([]any) {
				want := w.(map[string]any)
				seen[want["code"].(string)] = true
				if !slices.ContainsFunc(r.Diagnostics, func(d manifest.Diagnostic) bool {
					return d.Code == want["code"] && d.Pointer == want["pointer"]
				}) {
					t.Errorf("нет %v; получено %+v", want, r.Diagnostics)
				}
			}
		})
	}
	for _, code := range manifest.Codes {
		if !seen[code] {
			t.Errorf("код %s не покрыт фикстурой", code)
		}
	}
}
