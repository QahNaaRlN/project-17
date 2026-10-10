package manifest_test

import (
	"encoding/json"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"os"
	"reflect"
	"testing"
)

// MF-001, DS catalogue: Go is generated from the same source as TypeScript.
func TestBuiltinContract(t *testing.T) {
	b, err := os.ReadFile("../../../../../packages/manifest/schema/builtin-catalogue.json")
	if err != nil {
		t.Fatal(err)
	}
	var source map[string]any
	if err = json.Unmarshal(b, &source); err != nil {
		t.Fatal(err)
	}
	if len(source) != len(manifest.BuiltinPrimitives) {
		t.Fatal("catalogue count")
	}
	for _, name := range manifest.BuiltinPrimitives {
		if !reflect.DeepEqual(source[name], manifest.BuiltinContract(name)) {
			t.Fatal(name)
		}
	}
	if manifest.BuiltinContract("Unknown") != nil {
		t.Fatal("unknown type")
	}
	button := manifest.BuiltinContract("Button")
	button["props"].(map[string]any)["variant"] = nil
	if manifest.BuiltinContract("Button")["props"].(map[string]any)["variant"] == nil {
		t.Fatal("catalogue mutated")
	}
}
