package manifest_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"testing"
)

// MF-002: исходные суррогаты нельзя проверять после decoding/json, который их теряет.
func TestUnicodeJSON(t *testing.T) {
	for _, value := range []string{`"\ud800"`, `"\udfff"`, `"x\ud800y"`, `"\ud800\ud800"`, `"\udfff\udfff"`, `"\ud800x"`, `"\ud800\n"`} {
		for _, raw := range []string{value, "[" + value + "]", `{"nested":{"value":` + value + `}}`, `{` + value + `:"ok"}`} {
			t.Run(raw, func(t *testing.T) {
				result := manifest.ValidateJSON([]byte(raw))
				if result.Valid || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != manifest.CodeSchemaViolation || result.Diagnostics[0].Pointer != "" || result.Diagnostics[0].Params["keyword"] != "unicode" {
					t.Fatalf("Unicode must be rejected: %+v", result)
				}
			})
		}
	}
}

func TestParseJSONUnicode(t *testing.T) {
	for raw, want := range map[string]string{
		`"\ud800\udc00"`: `"𐀀"`, `"\udbff\udfff"`: `"􏿿"`,
		`"\uD83D\uDE00"`: `"😀"`, `"�"`: `"�"`, `"\ufffd"`: `"�"`,
		`"\\ud800"`: `"\\ud800"`, `""`: `""`, `"abc"`: `"abc"`,
		`{"😀":["\u0000\n\t\b\f\r\"\\"]}`: `{"😀":["\u0000\n\t\b\f\r\"\\"]}`,
		`[null,true,1]`:                  `[null,true,1]`,
	} {
		t.Run(raw, func(t *testing.T) {
			parsed, err := manifest.ParseJSON([]byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			got, err := manifest.CanonicalJSON(parsed)
			if err != nil || string(got) != want {
				t.Fatalf("canonical %s: %v, want %s", got, err, want)
			}
		})
	}
	for _, raw := range []string{`"\ud800"`, `"\udfff"`, `"\ud800\u0041"`, `"\ud800\\uDC00"`} {
		if _, err := manifest.ParseJSON([]byte(raw)); !errors.Is(err, manifest.ErrInvalidUnicode) {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	invalidUTF8 := []byte{'"', 0xff, '"'}
	if _, err := manifest.ParseJSON(invalidUTF8); !errors.Is(err, manifest.ErrInvalidUnicode) {
		t.Fatalf("UTF8: %v", err)
	}
	for _, raw := range []string{`"\uZZZZ"`, `"unterminated`, `{`, `"\ud800`} {
		if _, err := manifest.ParseJSON([]byte(raw)); err == nil {
			t.Fatalf("syntax accepted: %s", raw)
		}
	}
}

func TestInvalidNativeUTF8(t *testing.T) {
	bad := string([]byte{0xff})
	for _, v := range []any{bad, []any{bad}, map[string]any{"x": bad}, map[string]any{bad: "ok"}, map[string]any{"x": []any{bad}}} {
		if manifest.Validate(v).Valid {
			t.Fatal("invalid UTF8 accepted")
		}
		if _, err := manifest.CanonicalJSON(v); !errors.Is(err, manifest.ErrInvalidUnicode) {
			t.Fatalf("canonical: %v", err)
		}
		if _, err := manifest.Hash(v); !errors.Is(err, manifest.ErrInvalidUnicode) {
			t.Fatalf("hash: %v", err)
		}
	}
}

// MF-002: внешний парсер не паникует и не заменяет исходный Unicode.
func FuzzParseJSONUnicode(f *testing.F) {
	for _, seed := range []string{`"\ud800"`, `"\udfff"`, `"\uD83D\uDE00"`, `{"a":[null,true,1,"x"]}`, `"\\ud800"`, `"\ud800\\uDC00"`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		value, err := manifest.ParseJSON(data)
		if err != nil {
			return
		}
		got, err := manifest.CanonicalJSON(value)
		if err != nil {
			return
		} // числа вне диапазона float64 не имеют канонического представления.
		if !json.Valid(got) {
			t.Fatalf("not JSON: %s", got)
		}
		again, err := manifest.ParseJSON(got)
		if err != nil {
			t.Fatal(err)
		}
		second, err := manifest.CanonicalJSON(again)
		if err != nil || !bytes.Equal(got, second) {
			t.Fatalf("not idempotent: %s / %s (%v)", got, second, err)
		}
	})
}
