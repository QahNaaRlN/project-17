package schemadiag_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
)

var update = flag.Bool("update", false, "перезаписать testdata/golden.json")

// TestGolden прогоняет invalid-фикстуры IR и manifest через настоящие валидаторы и
// сверяет полный список диагностик (с текстами) со снимком. Conformance-тесты проверяют
// лишь наличие ожидаемых диагностик; снимок ловит и лишние, и изменённые тексты.
func TestGolden(t *testing.T) {
	got := map[string]any{}
	add := func(prefix, pattern string, validate func([]byte) any) {
		files, err := filepath.Glob(pattern)
		if err != nil || len(files) == 0 {
			t.Fatalf("нет фикстур %s", pattern)
		}
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			got[prefix+filepath.Base(f)] = validate(data)
		}
	}
	add("ir/", "../../../../../packages/ir/fixtures/invalid/*.json", func(data []byte) any {
		var fx struct{ Document json.RawMessage }
		if err := json.Unmarshal(data, &fx); err != nil {
			t.Fatal(err)
		}
		return ir.ValidateJSON(fx.Document).Diagnostics
	})
	add("manifest/", "../../../../../packages/manifest/fixtures/invalid/*.json", func(data []byte) any {
		var fx struct{ Manifest json.RawMessage }
		if err := json.Unmarshal(data, &fx); err != nil {
			t.Fatal(err)
		}
		return manifest.ValidateJSON(fx.Manifest).Diagnostics
	})

	actual, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	actual = append(actual, '\n')
	const golden = "testdata/golden.json"
	if *update {
		if err := os.WriteFile(golden, actual, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(actual) {
		t.Errorf("диагностики отличаются от %s (go test -run TestGolden -update):\n%s", golden, actual)
	}
}
