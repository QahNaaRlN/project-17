// Command manifestgen копирует packages/manifest/schema/manifest-1.0.schema.json — единственный
// источник истины формата manifest (MF-001) — в internal/composition/manifest/schema_gen.json
// для go:embed. Запуск из apps/server: go generate ./internal/composition/manifest (или pnpm gen).
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	schemaPath = "../../packages/manifest/schema/manifest-1.0.schema.json"
	outPath    = "internal/composition/manifest/schema_gen.json"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "manifestgen:", err)
		os.Exit(1)
	}
}

func run() error {
	gomod, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return err
	}
	if err := os.Chdir(filepath.Dir(string(bytes.TrimSpace(gomod)))); err != nil {
		return err
	}
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, schema, 0o644)
}
