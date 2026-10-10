package manifest_test

import (
	"encoding/json"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"os"
	"pgregory.net/rapid"
	"strings"
	"testing"
)

// MF-001: the Go/TS portable grammar and matching use identical fixtures.
func TestPortablePatterns(t *testing.T) {
	data, err := os.ReadFile("../../../../../packages/manifest/fixtures/patterns.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Pattern string
		Valid   bool
		Matches [][]any
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Pattern, func(t *testing.T) {
			re, err := manifest.CompilePattern(c.Pattern)
			if (err == nil) != c.Valid {
				t.Fatalf("valid=%v err=%v", c.Valid, err)
			}
			for _, match := range c.Matches {
				if got := re.MatchString(match[0].(string)); got != match[1].(bool) {
					t.Errorf("%q: got %v", match[0], got)
				}
			}
			src, err := os.ReadFile("../../../../../packages/manifest/fixtures/valid/store.json")
			if err != nil {
				t.Fatal(err)
			}
			m := decode(t, string(src)).(map[string]any)
			props := m["components"].(map[string]any)["ProductCard"].(map[string]any)["props"].(map[string]any)
			props["test"] = map[string]any{"type": "string", "pattern": c.Pattern}
			result := manifest.Validate(m)
			// Invalid Unicode and NUL are rejected at the common manifest boundary.
			if result.Valid != c.Valid {
				t.Fatalf("valid=%v diagnostics=%v", result.Valid, result.Diagnostics)
			}
			if !c.Valid && c.Pattern != "\x00" && len([]rune(c.Pattern)) <= 500 {
				d := result.Diagnostics[0]
				if d.Code != manifest.CodeModifierInvalid || d.Pointer != "/components/ProductCard/props/test/pattern" || d.Params["reason"] != "unsupported_pattern" {
					t.Fatalf("%+v", d)
				}
			}
		})
	}
}

func TestPatternDefault(t *testing.T) {
	for _, extra := range []string{"", `,"minLength":2`} {
		r := manifest.ValidateJSON([]byte(`{"manifestVersion":"1.0","app":{"id":"test","version":"1.0.0","framework":"react"},"irVersions":["1.0"],"tokens":{},"breakpoints":{},"components":{"Test":{"props":{"value":{"type":"string","pattern":"^x$","default":"y"` + extra + `}}}}}`))
		if len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != manifest.CodeDefaultInvalid || r.Diagnostics[0].Pointer != "/components/Test/props/value/default" {
			t.Fatalf("%+v", r)
		}
	}
}

func TestPatternProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		value := strings.Join(rapid.SliceOfN(rapid.SampledFrom([]string{"a", "я", "😀", "é"}), 0, 40).Draw(t, "scalars"), "")
		re, err := manifest.CompilePattern("^" + value + "$")
		if err != nil || !re.MatchString(value) {
			t.Fatalf("%q: %v", value, err)
		}
		if _, err := manifest.CompilePattern("(?=" + value + ")"); err == nil {
			t.Fatal("special group accepted")
		}
	})
}

func FuzzCompilePattern(f *testing.F) {
	for _, s := range []string{"^x$", "[", "(?=x)", "😀", "\\d+"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { _, _ = manifest.CompilePattern(s) })
}
