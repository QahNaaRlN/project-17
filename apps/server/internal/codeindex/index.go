// Package codeindex validates metadata only. It never opens a repository path.
package codeindex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
)

const MaxBytes = 1 << 20

var ErrInvalid = errors.New("CODE_INDEX_INVALID")
var commitPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
var HashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type Ref struct {
	File   string `json:"file"`
	Export string `json:"export,omitempty"`
	Line   *int   `json:"line,omitempty"`
}
type Definition struct {
	File string `json:"file"`
	Line int    `json:"line"`
}
type Element struct {
	Definition     Definition `json:"definition"`
	Implementation Ref        `json:"implementation"`
	Dependencies   []string   `json:"dependencies"`
	Tests          []string   `json:"tests"`
}
type Index struct {
	Repo struct {
		Root   string `json:"root"`
		Commit string `json:"commit"`
	} `json:"repo"`
	Elements    map[string]Element `json:"elements"`
	Conventions struct {
		Registry        string `json:"registry"`
		TestCommand     string `json:"testCommand"`
		ManifestCommand string `json:"manifestCommand"`
	} `json:"conventions"`
}

func Text(value string, max int) bool {
	if value == "" || len([]rune(value)) > max {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func Path(value string) bool {
	if !Text(value, 512) || strings.ContainsAny(value, `\:`) {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
		switch strings.ToLower(part) {
		case "node_modules", "dist", "generated", ".git", ".ssh", ".aws", ".codex":
			return false
		}
		if strings.HasPrefix(strings.ToLower(part), ".env") {
			return false
		}
	}
	return true
}
func paths(values []string) bool {
	if values == nil || len(values) > 1000 {
		return false
	}
	seen := map[string]bool{}
	for _, path := range values {
		if !Path(path) || seen[path] {
			return false
		}
		seen[path] = true
	}
	return true
}
func nonNull(value any) bool {
	if value == nil {
		return false
	}
	switch value := value.(type) {
	case map[string]any:
		for _, child := range value {
			if !nonNull(child) {
				return false
			}
		}
	case []any:
		for _, child := range value {
			if !nonNull(child) {
				return false
			}
		}
	}
	return true
}
func Parse(raw []byte) (Index, []byte, error) {
	var index Index
	if len(raw) > 2*MaxBytes {
		return index, nil, ErrInvalid
	}
	value, err := manifest.ParseJSON(raw)
	if err != nil || !nonNull(value) {
		return index, nil, ErrInvalid
	}
	canonical, err := manifest.CanonicalJSON(value)
	if err != nil || len(canonical) > MaxBytes {
		return index, nil, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&index) != nil || index.Repo.Root != "." || !commitPattern.MatchString(index.Repo.Commit) || index.Elements == nil || len(index.Elements) > 1000 ||
		!Path(index.Conventions.Registry) || !Text(index.Conventions.TestCommand, 512) || !Text(index.Conventions.ManifestCommand, 512) {
		return index, nil, ErrInvalid
	}
	for key, entry := range index.Elements {
		kind, name, ok := strings.Cut(key, ":")
		if !ok || !Text(name, 200) || strings.Contains(name, ":") || (kind != "component" && kind != "action" && kind != "dataSource") ||
			!Path(entry.Definition.File) || entry.Definition.Line < 1 || entry.Definition.Line > 10000000 ||
			!Path(entry.Implementation.File) || !Text(entry.Implementation.Export, 200) || (entry.Implementation.Line != nil && (*entry.Implementation.Line < 1 || *entry.Implementation.Line > 10000000)) ||
			!paths(entry.Dependencies) || !paths(entry.Tests) {
			return index, nil, ErrInvalid
		}
	}
	return index, canonical, nil
}

// Match binds supplied paths to the actual registered contract, not to a client label.
func Match(index Index, app map[string]any) bool {
	for key, entry := range index.Elements {
		kind, name, _ := strings.Cut(key, ":")
		group := map[string]string{"component": "components", "action": "actions", "dataSource": "dataSources"}[kind]
		elements, _ := app[group].(map[string]any)
		element, _ := elements[name].(map[string]any)
		source, _ := element["sourceRef"].(map[string]any)
		if source == nil || source["file"] != entry.Implementation.File || source["export"] != entry.Implementation.Export {
			return false
		}
		if line, ok := source["line"]; ok && (entry.Implementation.Line == nil || fmt.Sprint(line) != strconv.Itoa(*entry.Implementation.Line)) {
			return false
		}
	}
	return true
}
