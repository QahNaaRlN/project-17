package ir

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// Сгенерированные типы описывают все поля валидных документов: строгий разбор фикстур
// ловит расхождение types_gen.go со схемой (кроме фикстуры с намеренно неизвестными полями).
func TestGeneratedTypesDecodeValidFixtures(t *testing.T) {
	for _, path := range fixtureFiles(t, "valid") {
		name := fixtureName(path)
		if name == "unknown-fields-ignored" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.DisallowUnknownFields()
			var doc Document
			if err := dec.Decode(&doc); err != nil {
				t.Fatal(err)
			}
			if doc.Root == "" || len(doc.Nodes) == 0 {
				t.Fatalf("пустой документ после разбора: %+v", doc)
			}
		})
	}
}
