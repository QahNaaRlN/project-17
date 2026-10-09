package ir

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixturesDir — общие conformance-фикстуры TS и Go (IR-005, packages/ir/fixtures/README.md).
const fixturesDir = "../../../../../packages/ir/fixtures"

func fixtureFiles(t *testing.T, kind string) []string {
	t.Helper()
	return fixtureFilesF(t, kind)
}

func fixtureFilesF(t testing.TB, kind string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(fixturesDir, kind, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("нет фикстур в %s/%s", fixturesDir, kind)
	}
	return files
}

func fixtureName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".json")
}

func TestConformanceValid(t *testing.T) {
	for _, path := range fixtureFiles(t, "valid") {
		t.Run(fixtureName(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			res := ValidateJSON(data)
			if !res.Valid || len(res.Diagnostics) != 0 {
				t.Fatalf("ожидался валидный документ, получено: %s", mustJSON(res.Diagnostics))
			}
		})
	}
}

type invalidFixture struct {
	Description string `json:"description"`
	Expect      []struct {
		Code    Code    `json:"code"`
		Pointer *string `json:"pointer"`
	} `json:"expect"`
	Document json.RawMessage `json:"document"`
}

func TestConformanceInvalid(t *testing.T) {
	for _, path := range fixtureFiles(t, "invalid") {
		t.Run(fixtureName(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var fx invalidFixture
			if err := json.Unmarshal(data, &fx); err != nil {
				t.Fatal(err)
			}
			res := ValidateJSON(fx.Document)
			if res.Valid {
				t.Fatalf("%s: документ признан валидным", fx.Description)
			}
			for _, want := range fx.Expect {
				found := false
				for _, d := range res.Diagnostics {
					if d.Code == want.Code && (want.Pointer == nil || d.Pointer == *want.Pointer) {
						found = true
						break
					}
				}
				if !found {
					ptr := "<любой>"
					if want.Pointer != nil {
						ptr = *want.Pointer
					}
					t.Errorf("%s: нет диагностики %s %s; получено: %s", fx.Description, want.Code, ptr, mustJSON(res.Diagnostics))
				}
			}
		})
	}
}

func mustJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(b)
}
