package ir

import (
	"os"
	"testing"
)

// FuzzValidateJSON: валидатор не паникует и согласован на любых байтах.
// Затравка — все фикстуры. Длительный прогон: go test -fuzz=FuzzValidateJSON -fuzztime=5m.
func FuzzValidateJSON(f *testing.F) {
	for _, kind := range []string{"valid", "invalid"} {
		for _, path := range fixtureFilesF(f, kind) {
			data, err := os.ReadFile(path)
			if err != nil {
				f.Fatal(err)
			}
			f.Add(data)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		res := ValidateJSON(data)
		if res.Valid && len(res.Diagnostics) != 0 {
			t.Fatalf("валидный документ с диагностиками: %s", mustJSON(res.Diagnostics))
		}
	})
}
