package ir

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

//go:embed schema_gen.json
var schemaJSON []byte

const schemaURL = "https://cms.dev/schemas/ir/1.0/document.json"

var compiledSchema = sync.OnceValue(func() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		panic(fmt.Sprintf("ir: embedded schema is not valid JSON: %v", err))
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemaURL, doc); err != nil {
		panic(fmt.Sprintf("ir: add schema: %v", err))
	}
	return c.MustCompile(schemaURL)
})

// Result — итог валидации.
type Result struct {
	Valid       bool         `json:"valid"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// ValidateJSON разбирает JSON и валидирует документ (см. ValidateDocument).
func ValidateJSON(data []byte) Result {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return Result{Diagnostics: []Diagnostic{{
			Code:     CodeSchemaViolation,
			Severity: SeverityError,
			Pointer:  "",
			Message:  "документ не является корректным JSON",
			Params:   map[string]any{"error": err.Error()},
		}}}
	}
	return ValidateDocument(doc)
}

// ValidateDocument — структурная валидация документа IR (уровень L1, 02-ir.md §11.1):
// JSON Schema, инварианты дерева IR-010…014, IR-020 и ограничения размера.
// doc — результат разбора JSON (map[string]any, []any, string, json.Number, bool, nil);
// для разбора используйте ValidateJSON или jsonschema.UnmarshalJSON.
func ValidateDocument(doc any) Result {
	var out []Diagnostic
	obj, isObj := doc.(map[string]any)

	if version, ok := obj["irVersion"].(string); isObj && ok && !slices.Contains(SupportedVersions, version) {
		out = append(out, Diagnostic{
			Code:     CodeIrVersionUnsupported,
			Severity: SeverityError,
			Pointer:  "/irVersion",
			Message: fmt.Sprintf("Версия IR %s не поддерживается; поддерживаются: %s",
				version, strings.Join(SupportedVersions, ", ")),
			Params: map[string]any{"version": version, "supported": slices.Clone(SupportedVersions)},
		})
		return result(out)
	}

	if size := encodedSize(doc); size > MaxBodyBytes {
		out = append(out, Diagnostic{
			Code:     CodeLimitExceeded,
			Severity: SeverityError,
			Pointer:  "",
			Message:  fmt.Sprintf("Размер документа %d байт превышает %d", size, MaxBodyBytes),
			Params:   map[string]any{"limit": "bodyBytes", "max": MaxBodyBytes, "actual": size},
		})
	}

	if err := compiledSchema().Validate(doc); err != nil {
		var verr *jsonschema.ValidationError
		if !errors.As(err, &verr) {
			panic(fmt.Sprintf("ir: unexpected schema validation error: %v", err))
		}
		out = append(out, schemaDiagnostics(doc, verr)...)
	}

	if nodes, ok := obj["nodes"].(map[string]any); isObj && ok {
		out = append(out, treeDiagnostics(obj, nodes)...)
		out = append(out, nodeDiagnostics(nodes)...)
	}
	return result(out)
}

func result(diags []Diagnostic) Result {
	valid := true
	for _, d := range diags {
		if d.Severity == SeverityError {
			valid = false
		}
	}
	if diags == nil {
		diags = []Diagnostic{}
	}
	return Result{Valid: valid, Diagnostics: diags}
}

// encodedSize — размер документа в компактном JSON, как JSON.stringify в @cms/ir.
func encodedSize(doc any) int {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return 0 // значение не из JSON — его отвергнет схема
	}
	return buf.Len() - 1 // Encode добавляет перевод строки
}

// --- JSON Schema ---------------------------------------------------------------

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

// schemaDiagnostics сводит дерево ошибок к одной, самой конкретной диагностике на место
// в документе и убирает расплывчатые ошибки мест, у потомков которых есть свои ошибки.
func schemaDiagnostics(instance any, root *jsonschema.ValidationError) []Diagnostic {
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

	var out []Diagnostic
	for _, ptr := range order {
		e := byPointer[ptr]
		if isVague(e.kind) && hasDescendant(ptr) {
			continue
		}
		d := Diagnostic{
			Code:     CodeSchemaViolation,
			Severity: SeverityError,
			Pointer:  ptr,
			Message:  errorMessage(e),
			Params:   map[string]any{"keyword": keyword(e.kind)},
		}
		if id, ok := NodeIDFromPointer(ptr); ok {
			d.NodeID = id
		}
		out = append(out, d)
	}
	return out
}

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

// --- Инварианты дерева -----------------------------------------------------------

type childRef struct {
	id  string
	ptr string
}

// childRefs — ссылки узла на детей в каноническом порядке (02-ir.md §2.3):
// children по порядку, затем слоты в лексикографическом порядке имён.
func childRefs(id string, node any) []childRef {
	n, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	var refs []childRef
	if children, ok := n["children"].([]any); ok {
		for i, c := range children {
			if s, ok := c.(string); ok {
				refs = append(refs, childRef{id: s, ptr: Pointer("nodes", id, "children", i)})
			}
		}
	}
	if slots, ok := n["slots"].(map[string]any); ok {
		for _, slot := range sortedKeys(slots) {
			list, ok := slots[slot].([]any)
			if !ok {
				continue
			}
			for i, c := range list {
				if s, ok := c.(string); ok {
					refs = append(refs, childRef{id: s, ptr: Pointer("nodes", id, "slots", slot, i)})
				}
			}
		}
	}
	return refs
}

func treeDiagnostics(doc map[string]any, nodes map[string]any) []Diagnostic {
	var out []Diagnostic
	ids := sortedKeys(nodes)

	root, rootIsString := doc["root"].(string)
	_, rootExists := nodes[root]
	rootExists = rootIsString && rootExists
	if !rootExists {
		out = append(out, Diagnostic{
			Code:     CodeNodeNotFound,
			Severity: SeverityError,
			Pointer:  "/root",
			Message:  fmt.Sprintf("Корневой узел %v отсутствует в nodes", doc["root"]),
			Params:   map[string]any{"nodeId": doc["root"]},
		})
	}

	for _, id := range ids {
		n, ok := nodes[id].(map[string]any)
		if ok && n["id"] != id {
			out = append(out, Diagnostic{
				Code:     CodeNodeIDMismatch,
				Severity: SeverityError,
				Pointer:  Pointer("nodes", id, "id"),
				NodeID:   id,
				Message:  fmt.Sprintf("Поле id узла (%v) не совпадает с ключом %s", n["id"], id),
				Params:   map[string]any{"key": id, "id": n["id"]},
			})
		}
	}

	parentOf := map[string]string{}
	visited := map[string]bool{}
	depthReported := false

	// traverse обходит поддерево в прямом порядке и закрепляет за каждым узлом первого
	// родителя, через которого он достигнут; повторные ссылки — диагностики.
	type frame struct {
		id    string
		depth int
	}
	traverse := func(start string, checkDepth bool) {
		stack := []frame{{start, 1}}
		for len(stack) > 0 {
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if visited[f.id] {
				continue
			}
			visited[f.id] = true
			if checkDepth && f.depth > MaxDepth && !depthReported {
				depthReported = true
				out = append(out, Diagnostic{
					Code:     CodeLimitExceeded,
					Severity: SeverityError,
					Pointer:  Pointer("nodes", f.id),
					NodeID:   f.id,
					Message:  fmt.Sprintf("Глубина вложенности превышает %d", MaxDepth),
					Params:   map[string]any{"limit": "depth", "max": MaxDepth},
				})
			}
			var kids []string
			for _, ref := range childRefs(f.id, nodes[f.id]) {
				switch _, exists := nodes[ref.id]; {
				case !exists:
					out = append(out, Diagnostic{
						Code:     CodeNodeNotFound,
						Severity: SeverityError,
						Pointer:  ref.ptr,
						NodeID:   f.id,
						Message:  fmt.Sprintf("Дочерний узел %s отсутствует в nodes", ref.id),
						Params:   map[string]any{"childId": ref.id},
					})
				case ref.id == root: // при отсутствии корня ссылка на него уже отсеяна выше
					out = append(out, Diagnostic{
						Code:     CodeNodeCycle,
						Severity: SeverityError,
						Pointer:  ref.ptr,
						NodeID:   f.id,
						Message:  fmt.Sprintf("Корневой узел %s указан как дочерний узла %s", ref.id, f.id),
						Params:   map[string]any{"cycle": []string{ref.id}},
					})
				default:
					if parent, claimed := parentOf[ref.id]; claimed {
						out = append(out, Diagnostic{
							Code:     CodeNodeMultipleParents,
							Severity: SeverityError,
							Pointer:  ref.ptr,
							NodeID:   ref.id,
							Message: fmt.Sprintf("Узел %s уже вложен в %s; узел может иметь только одного родителя",
								ref.id, parent),
							Params: map[string]any{"parents": []string{parent, f.id}},
						})
						continue
					}
					parentOf[ref.id] = f.id
					kids = append(kids, ref.id)
				}
			}
			for i := len(kids) - 1; i >= 0; i-- {
				stack = append(stack, frame{kids[i], f.depth + 1})
			}
		}
	}

	if rootExists {
		traverse(root, true)
	}
	reachable := make(map[string]bool, len(visited))
	for id := range visited {
		reachable[id] = true
	}
	for _, id := range ids {
		traverse(id, false)
	}

	// Без корня достижимость не определена: каждый узел оказался бы «сиротой».
	if !rootExists {
		return out
	}

	// Недостижимые узлы: либо вершина «сиротского» поддерева, либо часть цикла.
	settled := reachable
	for _, id := range ids {
		if settled[id] {
			continue
		}
		var chain []string
		inChain := map[string]bool{}
		current, hasCurrent := id, true
		for hasCurrent && !settled[current] && !inChain[current] {
			chain = append(chain, current)
			inChain[current] = true
			current, hasCurrent = parentOf[current]
		}
		switch {
		case hasCurrent && inChain[current]:
			cycle := chain[slices.Index(chain, current):]
			first := slices.Min(cycle)
			out = append(out, Diagnostic{
				Code:     CodeNodeCycle,
				Severity: SeverityError,
				Pointer:  Pointer("nodes", first),
				NodeID:   first,
				Message:  "Узлы образуют цикл: " + strings.Join(cycle, " → "),
				Params:   map[string]any{"cycle": slices.Clone(cycle)},
			})
		case !hasCurrent:
			top := chain[len(chain)-1]
			out = append(out, Diagnostic{
				Code:     CodeNodeOrphan,
				Severity: SeverityError,
				Pointer:  Pointer("nodes", top),
				NodeID:   top,
				Message:  fmt.Sprintf("Узел %s не связан с корнем документа", top),
				Params:   map[string]any{},
			})
		}
		for _, n := range chain {
			settled[n] = true
		}
	}
	return out
}

// --- Проверки отдельных узлов --------------------------------------------------------

func nodeDiagnostics(nodes map[string]any) []Diagnostic {
	var out []Diagnostic
	for _, id := range sortedKeys(nodes) {
		n, ok := nodes[id].(map[string]any)
		if !ok {
			continue
		}
		props, okProps := n["props"].(map[string]any)
		bindings, okBindings := n["bindings"].(map[string]any)
		if okProps && okBindings {
			for _, key := range sortedKeys(props) {
				if _, bound := bindings[key]; !bound {
					continue
				}
				out = append(out, Diagnostic{
					Code:     CodePropBothStaticAndBound,
					Severity: SeverityError,
					Pointer:  Pointer("nodes", id, "props", key),
					NodeID:   id,
					Message:  fmt.Sprintf("Свойство %s задано и в props, и в bindings", key),
					Params:   map[string]any{"prop": key},
				})
			}
		}
		if when, ok := n["when"].(map[string]any); ok {
			if depth := predicateDepth(when); depth > MaxPredicateDepth {
				out = append(out, Diagnostic{
					Code:     CodeLimitExceeded,
					Severity: SeverityError,
					Pointer:  Pointer("nodes", id, "when"),
					NodeID:   id,
					Message:  fmt.Sprintf("Глубина условия %d превышает %d", depth, MaxPredicateDepth),
					Params:   map[string]any{"limit": "predicateDepth", "max": MaxPredicateDepth, "actual": depth},
				})
			}
		}
	}
	return out
}

func predicateDepth(p any) int {
	obj, ok := p.(map[string]any)
	if !ok {
		return 0
	}
	var nested []any
	switch obj["op"] {
	case "not":
		nested = append(nested, obj["arg"])
	case "and", "or":
		if args, ok := obj["args"].([]any); ok {
			nested = append(nested, args...)
		}
	}
	deepest := 0
	for _, n := range nested {
		deepest = max(deepest, predicateDepth(n))
	}
	return 1 + deepest
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
