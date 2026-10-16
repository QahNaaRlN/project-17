package ops

import (
	"encoding/json"
	"os"
	"testing"
)

func TestDataOperationConformanceSTU220(t *testing.T) {
	raw, err := os.ReadFile("../../../../../packages/ir/fixtures/studio-data-sources.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Operations []struct {
			Payload  json.RawMessage
			Expected any
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	doc := parse(t, base)
	for _, step := range fixture.Operations {
		if _, err := Apply(doc, Op{Type: DocumentSetDataSource, Payload: step.Payload}, nil); err != nil {
			t.Fatal(err)
		}
		actual, _ := json.Marshal(doc["dataSources"])
		expected, _ := json.Marshal(step.Expected)
		if string(actual) != string(expected) {
			t.Fatal("Go/TS declaration mismatch", doc["dataSources"], step.Expected)
		}
	}
}
