package manifest

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"encoding/json"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/schemadiag"
)

//go:embed schema_gen.json
var schemaJSON []byte

const schemaURL = "https://cms.dev/schemas/manifest-1.0/manifest.json"

var compiledSchema = sync.OnceValue(func() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		panic(fmt.Sprintf("manifest: embedded schema is not valid JSON: %v", err))
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemaURL, doc); err != nil {
		panic(fmt.Sprintf("manifest: add schema: %v", err))
	}
	return c.MustCompile(schemaURL)
})

// Result — итог валидации.
type Result struct {
	Valid       bool         `json:"valid"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// ValidateJSON разбирает JSON и валидирует manifest (см. Validate).
func ValidateJSON(data []byte) Result {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return result([]Diagnostic{{Code: CodeSchemaViolation, Severity: "error", Pointer: "",
			Message: "manifest не является корректным JSON", Params: map[string]any{"error": err.Error()}}})
	}
	return Validate(doc)
}

// Validate проверяет manifest: JSON Schema, затем — для структурно корректного manifest —
// семантику (имена, ссылки между разделами, значения по умолчанию, диапазоны, CNT-001).
// doc — результат jsonschema.UnmarshalJSON (числа — json.Number). Диагностики отсортированы
// по указателю (кодовые единицы UTF-16) и коду — как в @cms/manifest.
func Validate(doc any) Result {
	var out []Diagnostic
	if err := compiledSchema().Validate(doc); err != nil {
		var verr *jsonschema.ValidationError
		if !errors.As(err, &verr) {
			panic(fmt.Sprintf("manifest: unexpected schema validation error: %v", err))
		}
		for _, e := range schemadiag.Collect(doc, verr) {
			out = append(out, Diagnostic{Code: CodeSchemaViolation, Severity: "error", Pointer: e.Pointer,
				Message: e.Message, Params: map[string]any{"keyword": e.Keyword}})
		}
	} else {
		c := newChecker(doc.(map[string]any))
		c.run()
		out = c.out
	}
	slices.SortStableFunc(out, func(a, b Diagnostic) int {
		if c := compareUTF16(a.Pointer, b.Pointer); c != 0 {
			return c
		}
		return strings.Compare(a.Code, b.Code)
	})
	return result(out)
}

func result(diags []Diagnostic) Result {
	if diags == nil {
		diags = []Diagnostic{}
	}
	valid := true
	for _, d := range diags {
		if d.Severity == "error" {
			valid = false
		}
	}
	return Result{Valid: valid, Diagnostics: diags}
}

// --- Семантика ---------------------------------------------------------------------
// Порядок и правила проверок — как класс Checker в @cms/manifest (validate.ts).

type obj = map[string]any

func object(v any) obj {
	o, _ := v.(obj)
	return o
}

func sortedKeys(o obj) []string {
	keys := make([]string, 0, len(o))
	for k := range o {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, compareUTF16)
	return keys
}

// number — значение числа из JSON (json.Number после jsonschema.UnmarshalJSON).
func number(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := n.Float64()
	return f, err == nil
}

func strs(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, it := range items {
		s, _ := it.(string)
		out = append(out, s)
	}
	return out
}

type checker struct {
	m            obj
	out          []Diagnostic
	types        map[string]bool
	schemas      obj
	capabilities obj
	colors       obj
}

func newChecker(m obj) *checker {
	c := &checker{m: m, types: map[string]bool{}, schemas: object(m["schemas"]),
		capabilities: object(m["capabilities"]), colors: object(object(m["tokens"])["colors"])}
	for _, p := range BuiltinPrimitives {
		c.types[p] = true
	}
	for _, section := range []string{"primitives", "components"} {
		for name := range object(m[section]) {
			c.types[name] = true
		}
	}
	return c
}

func (c *checker) add(code, ptr, message string, params map[string]any) {
	c.out = append(c.out, Diagnostic{Code: code, Severity: "error", Pointer: ptr, Message: message, Params: params})
}

func (c *checker) run() {
	m := c.m
	supported := false
	for _, v := range strs(m["irVersions"]) {
		if slices.Contains(SupportedIRVersions, v) {
			supported = true
		}
	}
	if !supported {
		c.add(CodeIrVersionUnsupported, "/irVersions", fmt.Sprintf(
			"manifest не поддерживает ни одну версию IR сервера (%s)", strings.Join(SupportedIRVersions, ", ")), nil)
	}
	breakpoints := object(m["breakpoints"])
	typography := object(object(m["tokens"])["typography"])
	for _, name := range sortedKeys(typography) {
		for _, bp := range sortedKeys(object(object(typography[name])["fontSize"])) {
			if _, ok := breakpoints[bp]; bp != "base" && !ok {
				c.add(CodeUnknownBreakpoint, Pointer("tokens", "typography", name, "fontSize", bp),
					fmt.Sprintf("breakpoint %s не объявлен в breakpoints", bp), map[string]any{"breakpoint": bp})
			}
		}
	}
	primitives := object(m["primitives"])
	for _, section := range []string{"primitives", "components"} {
		record := object(m[section])
		for _, name := range sortedKeys(record) {
			ptr := Pointer(section, name)
			if slices.Contains(BuiltinPrimitives, name) {
				c.add(CodeNameReserved, ptr, fmt.Sprintf("имя %s занято встроенным примитивом", name), map[string]any{"name": name})
			}
			if _, dup := primitives[name]; section == "components" && dup {
				c.add(CodeNameDuplicate, ptr, fmt.Sprintf("%s объявлен и как примитив, и как компонент", name), map[string]any{"name": name})
			}
			c.component(ptr, object(record[name]))
		}
	}
	actions := object(m["actions"])
	for _, name := range sortedKeys(actions) {
		ptr := Pointer("actions", name)
		if slices.Contains(BuiltinActions, name) {
			c.add(CodeNameReserved, ptr, fmt.Sprintf("имя %s занято встроенным действием", name), map[string]any{"name": name})
		}
		action := object(actions[name])
		c.fields(ptr+"/args", object(action["args"]), false)
		c.capabilityRefs(ptr, action["capabilities"])
	}
	sources := object(m["dataSources"])
	for _, name := range sortedKeys(sources) {
		ptr := Pointer("dataSources", name)
		source := object(sources[name])
		c.fields(ptr+"/params", object(source["params"]), false)
		c.typ(ptr+"/result", object(source["result"]), false)
		c.capabilityRefs(ptr, source["capabilities"])
	}
	formatters := object(m["formatters"])
	for _, name := range sortedKeys(formatters) {
		c.fields(Pointer("formatters", name)+"/args", object(object(formatters[name])["args"]), false)
	}
	for _, name := range sortedKeys(c.schemas) {
		c.schema(Pointer("schemas", name), object(c.schemas[name]))
	}
}

func (c *checker) schema(ptr string, s obj) {
	fields := object(s["fields"])
	for _, field := range sortedKeys(fields) {
		if slices.Contains(ReservedFields, field) {
			c.add(CodeFieldReserved, ptr+"/fields"+Pointer(field),
				fmt.Sprintf("имя поля %s зарезервировано (CNT-001)", field), map[string]any{"field": field})
		}
	}
	c.fields(ptr+"/fields", fields, true)
	display := object(s["display"])
	for _, key := range []string{"titleField", "previewField"} {
		if field, ok := display[key].(string); ok {
			if _, exists := fields[field]; !exists {
				c.add(CodeUnknownField, ptr+"/display/"+key, fmt.Sprintf("поля %s нет в схеме", field), map[string]any{"field": field})
			}
		}
	}
	version, _ := number(s["version"])
	migrations := object(s["migrateFrom"])
	for _, from := range sortedKeys(migrations) {
		f, _ := strconv.ParseFloat(from, 64)
		if f >= version {
			c.add(CodeMigrationInvalid, ptr+"/migrateFrom"+Pointer(from),
				fmt.Sprintf("миграция с версии %s должна быть с версии меньше текущей %v", from, version),
				map[string]any{"from": f, "version": version})
		}
	}
}

func (c *checker) component(ptr string, comp obj) {
	c.fields(ptr+"/props", object(comp["props"]), false)
	slots := object(comp["slots"])
	for _, name := range sortedKeys(slots) {
		slotPtr := ptr + "/slots" + Pointer(name)
		slot := object(slots[name])
		for i, t := range strs(slot["allowedTypes"]) {
			if !c.types[t] {
				c.add(CodeUnknownType, fmt.Sprintf("%s/allowedTypes/%d", slotPtr, i),
					fmt.Sprintf("тип узла %s не объявлен", t), map[string]any{"type": t})
			}
		}
		c.rangeCheck(slotPtr, slot["min"], slot["max"])
	}
	events := object(comp["events"])
	for _, name := range sortedKeys(events) {
		c.fields(ptr+"/events"+Pointer(name)+"/payload", object(object(events[name])["payload"]), false)
	}
	c.fields(ptr+"/provides", object(comp["provides"]), false)
	c.capabilityRefs(ptr, comp["capabilities"])
}

func (c *checker) capabilityRefs(ptr string, refs any) {
	for i, cap := range strs(refs) {
		if _, ok := c.capabilities[cap]; !ok {
			c.add(CodeUnknownCapability, fmt.Sprintf("%s/capabilities/%d", ptr, i),
				fmt.Sprintf("capability %s не объявлена в capabilities", cap), map[string]any{"capability": cap})
		}
	}
}

func (c *checker) fields(ptr string, fields obj, schemaField bool) {
	for _, name := range sortedKeys(fields) {
		c.typ(ptr+Pointer(name), object(fields[name]), schemaField)
	}
}

func (c *checker) rangeCheck(ptr string, minV, maxV any) {
	lo, okMin := number(minV)
	hi, okMax := number(maxV)
	if okMin && okMax && lo > hi {
		c.add(CodeRangeInvalid, ptr, fmt.Sprintf("минимум %v больше максимума %v", lo, hi), map[string]any{"min": lo, "max": hi})
	}
}

func (c *checker) defaultInvalid(ptr, message string) {
	c.add(CodeDefaultInvalid, ptr+"/default", message, nil)
}

// typ проверяет тип. schemaField — поле схемы контента верхнего уровня: только там допустимы
// localized и unique.
func (c *checker) typ(ptr string, t obj, schemaField bool) {
	kind, _ := t["type"].(string)
	if t["content"] == true && !slices.Contains(contentTypes, kind) {
		c.add(CodeModifierInvalid, ptr+"/content", "content неприменим к типу "+kind, nil)
	}
	if t["localized"] == true && !schemaField {
		c.add(CodeModifierInvalid, ptr+"/localized", "localized допустим только у полей схем контента", nil)
	}
	if t["unique"] == true && (!schemaField || !slices.Contains(uniqueTypes, kind)) {
		c.add(CodeModifierInvalid, ptr+"/unique", "unique допустим только у полей схем контента типов string, text, number", nil)
	}
	def, hasDefault := t["default"]
	switch kind {
	case "string":
		c.rangeCheck(ptr, t["minLength"], t["maxLength"])
		if s, ok := def.(string); hasDefault && ok && !lengthFits(s, t["minLength"], t["maxLength"]) {
			c.defaultInvalid(ptr, "значение по умолчанию не укладывается в ограничения длины")
		}
	case "text":
		if s, ok := def.(string); hasDefault && ok && !lengthFits(s, nil, t["maxLength"]) {
			c.defaultInvalid(ptr, "значение по умолчанию длиннее maxLength")
		}
	case "number":
		c.rangeCheck(ptr, t["min"], t["max"])
		if d, ok := number(def); hasDefault && ok {
			lo, okMin := number(t["min"])
			hi, okMax := number(t["max"])
			if (okMin && d < lo) || (okMax && d > hi) || (t["integer"] == true && d != math.Trunc(d)) {
				c.defaultInvalid(ptr, "значение по умолчанию вне допустимого диапазона")
			}
		}
	case "enum":
		if s, ok := def.(string); hasDefault && ok && !slices.Contains(strs(t["values"]), s) {
			c.defaultInvalid(ptr, fmt.Sprintf("значение по умолчанию %s не входит в values", s))
		}
	case "reference":
		schema, _ := t["schema"].(string)
		if _, ok := c.schemas[schema]; !ok {
			c.add(CodeUnknownSchema, ptr+"/schema", fmt.Sprintf("схема %s не объявлена в schemas", schema), map[string]any{"schema": schema})
		}
	case "list":
		c.rangeCheck(ptr, t["min"], t["max"])
		c.typ(ptr+"/of", object(t["of"]), false)
	case "object":
		c.fields(ptr+"/fields", object(t["fields"]), false)
	case "color":
		if s, ok := def.(string); hasDefault && ok {
			if _, known := c.colors[s]; !known {
				c.defaultInvalid(ptr, fmt.Sprintf("цветового токена %s нет в tokens.colors", s))
			}
		}
	case "nodeRef":
		if nt, ok := t["nodeType"].(string); ok && !c.types[nt] {
			c.add(CodeUnknownType, ptr+"/nodeType", fmt.Sprintf("тип узла %s не объявлен", nt), map[string]any{"type": nt})
		}
	}
}

// lengthFits — длина в кодовых точках Unicode, как [...value].length в @cms/manifest.
func lengthFits(s string, minV, maxV any) bool {
	n := float64(utf8.RuneCountInString(s))
	lo, okMin := number(minV)
	hi, okMax := number(maxV)
	return (!okMin || n >= lo) && (!okMax || n <= hi)
}
