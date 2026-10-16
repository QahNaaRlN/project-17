package bindingdoc

import (
	"encoding/json"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
	"os"
	"reflect"
	"testing"
)

// STU-240/242/243/244: Go/TS conformance, canonical payloads and inverse effects.
func TestBehaviorFixtureSTU240244(t *testing.T) {
	raw, err := os.ReadFile("../../../../../packages/ir/fixtures/studio-behavior.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Builtins      map[string]object
		Compatibility []struct {
			Name     string
			From, To object
			Valid    bool
		}
		Operations []struct {
			Type, Field string
			Payload     json.RawMessage
			Expected    any
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for name, fields := range fixture.Builtins {
		if !reflect.DeepEqual(builtinAction(name), fields) {
			t.Fatal(name, builtinAction(name), fields)
		}
	}
	for _, tc := range fixture.Compatibility {
		if SimpleContentCompatible(tc.From, tc.To) != tc.Valid {
			t.Fatal(tc.Name)
		}
	}
	d := document(object{"type": "Button"})
	delete(d, "localContent")
	for _, step := range fixture.Operations {
		before := ops.Clone(d)
		result, err := ops.Apply(d, ops.Op{Type: step.Type, Payload: step.Payload}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := obj(obj(d["nodes"])["n_root"])[step.Field]; !reflect.DeepEqual(got, step.Expected) {
			t.Fatal(step.Type, got, step.Expected)
		}
		if _, err := ops.Apply(d, result.Inverse, nil); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, d) {
			t.Fatal("inverse", d, before)
		}
		if _, err := ops.Apply(d, ops.Op{Type: step.Type, Payload: step.Payload}, nil); err != nil {
			t.Fatal(err)
		}
	}
}
