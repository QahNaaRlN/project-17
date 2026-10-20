package ops

import (
	"encoding/json"
	"os"
	"testing"
)

const extractFixture = "../../../../../packages/ir/fixtures/component-extract.json"

// STU-412: общая Go/TS фикстура — страница до, канонический payload, страница после и компонент.
// CMS_UPDATE_FIXTURES=1 перезаписывает файл результатом Go.
func TestExtractFixtureSTU412(t *testing.T) {
	page := parse(t, showcase)
	before := Clone(page)
	r, err := Extract(page, showcasePayload(), builtinEnv())
	if err != nil {
		t.Fatal(err)
	}
	actual := map[string]any{"before": before, "payload": r.Payload, "page": page, "component": r.Component}
	if os.Getenv("CMS_UPDATE_FIXTURES") == "1" {
		raw, _ := json.MarshalIndent(actual, "", "  ")
		if err := os.WriteFile(extractFixture, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(extractFixture)
	if err != nil {
		t.Fatal(err)
	}
	var expected map[string]any
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	var normalized map[string]any
	_ = json.Unmarshal(mustRaw(actual), &normalized)
	if string(mustRaw(normalized)) != string(mustRaw(expected)) {
		t.Fatalf("fixture differs from Go result; run with CMS_UPDATE_FIXTURES=1\n%s", mustRaw(normalized))
	}
}
