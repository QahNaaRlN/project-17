package manifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// CanonicalJSON — канонический JSON (MF-002), байт в байт как canonicalJson в @cms/manifest:
// ключи объектов по возрастанию кодовых единиц UTF-16, без пробелов, строки и числа — как у
// JSON.stringify. v — результат разбора JSON (map[string]any, []any, string, json.Number или
// float64, bool, nil).
func CanonicalJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := writeCanonical(&b, v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Hash — manifestHash: "sha256:" + SHA-256 канонического JSON в hex.
func Hash(v any) (string, error) {
	c, err := CanonicalJSON(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(c)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func writeCanonical(b *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(t))
	case string:
		if !utf8.ValidString(t) {
			return ErrInvalidUnicode
		}
		writeString(b, t)
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return err
		}
		return writeNumber(b, f)
	case float64:
		return writeNumber(b, t)
	case []any:
		b.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonical(b, item); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			if !utf8.ValidString(k) {
				return ErrInvalidUnicode
			}
			keys = append(keys, k)
		}
		slices.SortFunc(keys, compareUTF16)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeString(b, k)
			b.WriteByte(':')
			if err := writeCanonical(b, t[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("manifest: значение %T не из JSON", v)
	}
	return nil
}

// writeNumber — как JSON.stringify: encoding/json форматирует float64 по правилам ES6;
// -0 в JS выводится как 0.
func writeNumber(b *bytes.Buffer, f float64) error {
	if f == 0 {
		f = 0
	}
	out, err := json.Marshal(f)
	if err != nil {
		return err // NaN и бесконечности в JSON невозможны
	}
	b.Write(out)
	return nil
}

const hexDigits = "0123456789abcdef"

// writeString экранирует строку как JSON.stringify: только кавычку, обратную косую черту и
// управляющие символы; HTML-символы и U+2028/U+2029 — как есть.
func writeString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte(hexDigits[r>>4])
				b.WriteByte(hexDigits[r&0xf])
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// compareUTF16 сравнивает строки по кодовым единицам UTF-16 — как операторы < и > в JS.
func compareUTF16(a, b string) int {
	return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
}
