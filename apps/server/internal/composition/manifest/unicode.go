package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ErrInvalidUnicode запрещает потерю исходных символов при разборе и хэшировании (MF-002).
var ErrInvalidUnicode = errors.New("manifest содержит некорректный Unicode")

// ParseJSON проверяет Unicode до JSON decoder, который заменяет одиночные суррогаты на U+FFFD.
// Внешний JSON manifest нужно разбирать этим методом, затем передавать в Validate и Hash.
// Проверка UTF-8 и пар суррогатов не является проверкой схемы manifest.
func ParseJSON(data []byte) (any, error) {
	if json.Valid(data) && !validJSONUnicode(data) {
		return nil, ErrInvalidUnicode
	}
	return jsonschema.UnmarshalJSON(bytes.NewReader(data))
}

// validJSONUnicode вызывается только для синтаксически корректного JSON.
// Поэтому позиции четырёх hex-цифр и закрывающей кавычки уже проверены json.Valid.
func validJSONUnicode(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	for i := 0; i < len(data); i++ {
		if data[i] != '"' {
			continue
		}
		for i++; data[i] != '"'; i++ {
			if data[i] != '\\' {
				continue
			}
			i++
			if data[i] != 'u' {
				continue
			}
			code, _ := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
			i += 4
			if code >= 0xdc00 && code <= 0xdfff {
				return false
			}
			if code < 0xd800 || code > 0xdbff {
				continue
			}
			// Высокий суррогат должен немедленно сопровождаться низким.
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}

func validUnicodeValue(value any) bool {
	switch v := value.(type) {
	case string:
		return utf8.ValidString(v)
	case []any:
		for _, item := range v {
			if !validUnicodeValue(item) {
				return false
			}
		}
	case map[string]any:
		for key, item := range v {
			if !utf8.ValidString(key) || !validUnicodeValue(item) {
				return false
			}
		}
	}
	return true
}

func unicodeResult() Result {
	return result([]Diagnostic{{Code: CodeSchemaViolation, Severity: "error", Pointer: "",
		Message: ErrInvalidUnicode.Error(), Params: map[string]any{"keyword": "unicode"}}})
}
