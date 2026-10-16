package bindingdoc

import (
	"encoding/json"
	"os"
	"testing"
)

func TestDataCompatibilityConformanceSTU223224(t *testing.T) {
	raw, err := os.ReadFile("../../../../../packages/ir/fixtures/studio-data-sources.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Compatibility []struct {
			Name     string
			From, To map[string]any
			Valid    bool
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Compatibility {
		t.Run(tc.Name, func(t *testing.T) {
			if got := SimpleContentCompatible(tc.From, tc.To); got != tc.Valid {
				t.Fatalf("Go/TS compatibility: %v != %v", got, tc.Valid)
			}
		})
	}
}
