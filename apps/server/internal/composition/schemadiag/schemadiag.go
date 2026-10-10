// Package schemadiag сводит дерево ошибок JSON Schema (santhosh-tekuri/jsonschema v6) к одной,
// самой конкретной ошибке на место в документе — так же, как это делают реализации на ajv
// (@cms/ir, @cms/manifest). Общий для валидаторов IR и manifest.
package schemadiag

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// Error — ошибка схемы для диагностики.
type Error struct {
	Pointer string // JSON Pointer места в документе
	Message string // текст на русском
	Keyword string // ключевое слово JSON Schema (params.keyword)
}

// Collect сводит дерево ошибок к одной, самой конкретной ошибке на место в документе и
// убирает расплывчатые ошибки мест, у потомков которых есть свои ошибки. Порядок —
// порядок обхода дерева ошибок.
func Collect(instance any, root *jsonschema.ValidationError) []Error {
	var leaves []schemaError
	collectLeaves(instance, nil, root, &leaves)

	byPointer := map[string]schemaError{}
	var order []string
	for _, e := range leaves {
		cur, seen := byPointer[e.pointer]
		if !seen {
			order = append(order, e.pointer)
		}
		if !seen || rank(e.kind) < rank(cur.kind) {
			byPointer[e.pointer] = e
		}
	}

	hasDescendant := func(ptr string) bool {
		for _, p := range order {
			if p != ptr && strings.HasPrefix(p, ptr+"/") {
				return true
			}
		}
		return false
	}

	var out []Error
	for _, ptr := range order {
		e := byPointer[ptr]
		if isVague(e.kind) && hasDescendant(ptr) {
			continue
		}
		out = append(out, Error{Pointer: ptr, Message: errorMessage(e), Keyword: keyword(e.kind)})
	}
	return out
}

// PointerSegment экранирует сегмент JSON Pointer (RFC 6901).
func PointerSegment(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

type schemaError struct {
	pointer string
	kind    jsonschema.ErrorKind
	detail  string // уточнение для propertyNames
}

// rank — чем меньше, тем конкретнее ошибка. Совпадает с логикой @cms/ir:
// расплывчатые ошибки (тип, oneOf …) уступают остальным, ошибки «чужой» ветки oneOf
// (лишнее или отсутствующее поле, константа) — конкретным ограничениям.
func rank(k jsonschema.ErrorKind) int {
	switch k.(type) {
	case *kind.Type, *kind.OneOf, *kind.AnyOf, *kind.AllOf, *kind.Not, *kind.Group, *kind.Schema, *kind.Reference:
		return 2
	case *kind.AdditionalProperties, *kind.Required, *kind.Const:
		return 1
	}
	return 0
}

func isVague(k jsonschema.ErrorKind) bool { return rank(k) == 2 }

// collectLeaves собирает конечные ошибки дерева. trusted — достоверный путь ближайшей
// ошибки-предка (см. propertyNamesLocation).
func collectLeaves(instance any, trusted []string, e *jsonschema.ValidationError, out *[]schemaError) {
	ptr := instancePointer(e.InstanceLocation)
	switch k := e.ErrorKind.(type) {
	case *kind.PropertyNames:
		// Указываем на само свойство; вложенные ошибки дают пояснение.
		detail := ""
		if len(e.Causes) > 0 {
			detail = errorMessage(firstLeaf(e.Causes[0]))
		}
		loc := propertyNamesLocation(instance, trusted, len(e.InstanceLocation), k.Property)
		*out = append(*out, schemaError{pointer: instancePointer(loc) + "/" + PointerSegment(k.Property), kind: k, detail: detail})
		return
	case *kind.AdditionalProperties:
		for _, p := range k.Properties {
			*out = append(*out, schemaError{pointer: ptr + "/" + PointerSegment(p), kind: k})
		}
		return
	case *kind.OneOf:
		// Аналог discriminator: если в части веток не совпал «тег» (const/enum прямого
		// свойства объекта), эти ветки заведомо чужие — отчитываемся только по остальным.
		causes := e.Causes
		var matching []*jsonschema.ValidationError
		for _, c := range causes {
			if !hasTagMismatch(c, ptr) {
				matching = append(matching, c)
			}
		}
		if len(matching) > 0 && len(matching) < len(causes) {
			causes = matching
		}
		if len(causes) == 0 {
			*out = append(*out, schemaError{pointer: ptr, kind: k})
		}
		for _, c := range causes {
			collectLeaves(instance, e.InstanceLocation, c, out)
		}
		return
	}
	if len(e.Causes) == 0 {
		*out = append(*out, schemaError{pointer: ptr, kind: e.ErrorKind})
		return
	}
	for _, c := range e.Causes {
		collectLeaves(instance, e.InstanceLocation, c, out)
	}
}

// propertyNamesLocation восстанавливает путь объекта, имя свойства которого не прошло
// propertyNames.
//
// Обход ошибки jsonschema v6.0.3: для propertyNames библиотека сохраняет в
// ValidationError.InstanceLocation внутренний срез без копирования (validator.go,
// `verr.InstanceLocation = vd.vloc`), и его элементы затем перезаписываются при проверке
// других частей документа. Длина среза остаётся верной. Поэтому ищем в документе объект
// на этой глубине под достоверным путём предка, содержащий свойство name; при нескольких
// кандидатах берём первый в лексикографическом порядке пути.
func propertyNamesLocation(instance any, trusted []string, depth int, name string) []string {
	prefix := trusted
	if len(prefix) > depth {
		prefix = prefix[:depth]
	}
	node := instance
	for _, seg := range prefix {
		node = child(node, seg)
	}
	var found []string
	var walk func(v any, path []string) bool
	walk = func(v any, path []string) bool {
		if len(path) == depth {
			if obj, ok := v.(map[string]any); ok {
				if _, has := obj[name]; has {
					found = slices.Clone(path)
					return true
				}
			}
			return false
		}
		switch t := v.(type) {
		case map[string]any:
			for _, k := range sortedKeys(t) {
				if walk(t[k], append(path, k)) {
					return true
				}
			}
		case []any:
			for i, item := range t {
				if walk(item, append(path, strconv.Itoa(i))) {
					return true
				}
			}
		}
		return false
	}
	if walk(node, slices.Clone(prefix)) {
		return found
	}
	return prefix // не нашли — указываем на ближайший достоверный путь
}

func child(v any, seg string) any {
	switch t := v.(type) {
	case map[string]any:
		return t[seg]
	case []any:
		if i, err := strconv.Atoi(seg); err == nil && i >= 0 && i < len(t) {
			return t[i]
		}
	}
	return nil
}

func hasTagMismatch(e *jsonschema.ValidationError, objectPtr string) bool {
	switch e.ErrorKind.(type) {
	case *kind.Const, *kind.Enum:
		// Тег — прямое свойство объекта, на котором стоит oneOf.
		loc := instancePointer(e.InstanceLocation)
		if i := strings.LastIndex(loc, "/"); i >= 0 && loc[:i] == objectPtr {
			return true
		}
	}
	for _, c := range e.Causes {
		if hasTagMismatch(c, objectPtr) {
			return true
		}
	}
	return false
}

func firstLeaf(e *jsonschema.ValidationError) schemaError {
	for len(e.Causes) > 0 {
		e = e.Causes[0]
	}
	return schemaError{pointer: instancePointer(e.InstanceLocation), kind: e.ErrorKind}
}

func instancePointer(loc []string) string {
	var b strings.Builder
	for _, s := range loc {
		b.WriteByte('/')
		b.WriteString(PointerSegment(s))
	}
	return b.String()
}

var englishPrinter = message.NewPrinter(language.English)

// errorMessage — текст ошибки схемы на русском (как ajv-i18n в @cms/ir); для редких видов —
// английский текст библиотеки.
func errorMessage(e schemaError) string {
	switch k := e.kind.(type) {
	case *kind.PropertyNames:
		if e.detail != "" {
			return fmt.Sprintf("недопустимое имя свойства %s: %s", k.Property, e.detail)
		}
		return "недопустимое имя свойства " + k.Property
	case *kind.FalseSchema:
		return "поле недопустимо в этом контексте"
	case *kind.Required:
		return "должно иметь обязательное поле " + strings.Join(k.Missing, ", ")
	case *kind.AdditionalProperties:
		return "не должно иметь дополнительных полей"
	case *kind.Pattern:
		return fmt.Sprintf("должно соответствовать образцу %q", k.Want)
	case *kind.Enum:
		return "должно быть равно одному из разрешенных значений"
	case *kind.Const:
		return "должно быть равно заданному значению"
	case *kind.Type:
		return "должно быть " + strings.Join(k.Want, " или ")
	case *kind.MinItems:
		return fmt.Sprintf("должно иметь не менее, чем %d элементов", k.Want)
	case *kind.MaxItems:
		return fmt.Sprintf("должно иметь не более, чем %d элементов", k.Want)
	case *kind.MaxProperties:
		return fmt.Sprintf("должно иметь не более, чем %d полей", k.Want)
	case *kind.MinProperties:
		return fmt.Sprintf("должно иметь не менее, чем %d полей", k.Want)
	case *kind.MaxLength:
		return fmt.Sprintf("должно иметь не более, чем %d символов", k.Want)
	case *kind.MinLength:
		return fmt.Sprintf("должно иметь не менее, чем %d символов", k.Want)
	case *kind.UniqueItems:
		return "не должно иметь повторяющихся элементов"
	case *kind.Not:
		return `должно не соответствовать схеме в "not"`
	}
	return e.kind.LocalizedString(englishPrinter)
}

// keyword — имя ключевого слова JSON Schema, как params.keyword в @cms/ir.
func keyword(k jsonschema.ErrorKind) string {
	if _, ok := k.(*kind.FalseSchema); ok {
		return "false schema"
	}
	path := k.KeywordPath()
	if len(path) == 0 {
		return ""
	}
	return path[0]
}
