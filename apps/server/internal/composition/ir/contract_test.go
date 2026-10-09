package ir

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Контракт диагностик, общий с @cms/ir (packages/ir/test/contract.test.ts).

func validate(t *testing.T, src string) Result {
	t.Helper()
	return ValidateDocument(decode(t, src))
}

func only(t *testing.T, res Result, code Code) Diagnostic {
	t.Helper()
	var found []Diagnostic
	for _, d := range res.Diagnostics {
		if d.Code == code {
			found = append(found, d)
		}
	}
	if len(found) != 1 {
		t.Fatalf("ожидалась одна диагностика %s, получено: %s", code, mustJSON(res.Diagnostics))
	}
	return found[0]
}

// params через JSON — чтобы сравнивать так же, как их увидит клиент API.
func paramsJSON(t *testing.T, d Diagnostic) string {
	t.Helper()
	b, err := json.Marshal(d.Params)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func doc(nodeList string, extra ...string) string {
	tail := ""
	if len(extra) > 0 {
		tail = "," + strings.Join(extra, ",")
	}
	return fmt.Sprintf(`{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{%s}%s}`, nodeList, tail)
}

func nodes(parts ...string) string { return strings.Join(parts, ",") }

func box(id string, children ...string) string {
	if len(children) == 0 {
		return fmt.Sprintf(`%q:{"id":%q,"type":"Box"}`, id, id)
	}
	b, _ := json.Marshal(children)
	return fmt.Sprintf(`%q:{"id":%q,"type":"Box","children":%s}`, id, id, b)
}

func TestDiagnosticParams(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		code   Code
		nodeID string
		params string
	}{
		{"версия", `{"irVersion":"9.0","kind":"page","root":"n_root","nodes":{}}`, CodeIrVersionUnsupported, "",
			`{"supported":["1.0"],"version":"9.0"}`},
		{"нет корня", doc(box("n_aaaa")), CodeNodeNotFound, "", `{"nodeId":"n_root"}`},
		{"нет ребёнка", doc(box("n_root", "n_gone")), CodeNodeNotFound, "n_root", `{"childId":"n_gone"}`},
		{"id не совпадает", doc(`"n_root":{"id":"n_other","type":"Box"}`), CodeNodeIDMismatch, "n_root",
			`{"id":"n_other","key":"n_root"}`},
		{"второй родитель", doc(nodes(box("n_root", "n_aaaa", "n_bbbb"), box("n_aaaa", "n_cccc"), box("n_bbbb", "n_cccc"), box("n_cccc"))),
			CodeNodeMultipleParents, "n_cccc", `{"parents":["n_aaaa","n_bbbb"]}`},
		{"корень как ребёнок", doc(nodes(box("n_root", "n_aaaa"), box("n_aaaa", "n_root"))), CodeNodeCycle, "n_aaaa",
			`{"cycle":["n_root"]}`},
		{"свойство в props и bindings",
			doc(`"n_root":{"id":"n_root","type":"Text","props":{"text":"x","as":"p"},"bindings":{"text":"$content.a"}}`),
			CodePropBothStaticAndBound, "n_root", `{"prop":"text"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := only(t, validate(t, tc.src), tc.code)
			if d.NodeID != tc.nodeID {
				t.Errorf("nodeId = %q, ожидалось %q", d.NodeID, tc.nodeID)
			}
			if got := paramsJSON(t, d); got != tc.params {
				t.Errorf("params = %s, ожидалось %s", got, tc.params)
			}
		})
	}
}

func TestCycleReportedOnceFromSmallestID(t *testing.T) {
	d := only(t, validate(t, doc(nodes(box("n_root"), box("n_zzzz", "n_mmmm"), box("n_mmmm", "n_aaaa"), box("n_aaaa", "n_zzzz")))), CodeNodeCycle)
	if d.NodeID != "n_aaaa" {
		t.Errorf("nodeId = %s", d.NodeID)
	}
	cycle := slices.Clone(d.Params["cycle"].([]string))
	slices.Sort(cycle)
	if !reflect.DeepEqual(cycle, []string{"n_aaaa", "n_mmmm", "n_zzzz"}) {
		t.Errorf("cycle = %v", cycle)
	}
}

func TestOrphanReportedOnTopOfSubtreeOnly(t *testing.T) {
	res := validate(t, doc(nodes(box("n_root"), box("n_top1", "n_kid1"), box("n_kid1"))))
	d := only(t, res, CodeNodeOrphan)
	if d.NodeID != "n_top1" {
		t.Errorf("nodeId = %s", d.NodeID)
	}
}

func TestDepthReportedOnce(t *testing.T) {
	var parts []string
	n := MaxDepth + 10
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("n_%04d", i)
		if i == 0 {
			id = "n_root"
		}
		if i+1 < n {
			parts = append(parts, box(id, fmt.Sprintf("n_%04d", i+1)))
		} else {
			parts = append(parts, box(id))
		}
	}
	d := only(t, validate(t, doc(strings.Join(parts, ","))), CodeLimitExceeded)
	if got := paramsJSON(t, d); got != fmt.Sprintf(`{"limit":"depth","max":%d}`, MaxDepth) {
		t.Errorf("params = %s", got)
	}
}

func TestPredicateDepth(t *testing.T) {
	nest := func(op string, depth int) string {
		p := `{"op":"exists","arg":"$content.a"}`
		for i := 1; i < depth; i++ {
			if op == "not" {
				p = fmt.Sprintf(`{"op":"not","arg":%s}`, p)
			} else {
				p = fmt.Sprintf(`{"op":%q,"args":[{"op":"exists","arg":"$content.b"},%s]}`, op, p)
			}
		}
		return p
	}
	for _, op := range []string{"and", "or", "not"} {
		t.Run(op, func(t *testing.T) {
			at := func(depth int) []Code {
				res := validate(t, doc(fmt.Sprintf(`"n_root":{"id":"n_root","type":"Box","when":%s}`, nest(op, depth))))
				var codes []Code
				for _, d := range res.Diagnostics {
					codes = append(codes, d.Code)
				}
				return codes
			}
			if got := at(MaxPredicateDepth); len(got) != 0 {
				t.Errorf("глубина %d: %v", MaxPredicateDepth, got)
			}
			if got := at(MaxPredicateDepth + 1); !reflect.DeepEqual(got, []Code{CodeLimitExceeded}) {
				t.Errorf("глубина %d: %v", MaxPredicateDepth+1, got)
			}
		})
	}
}

func TestSizeLimit(t *testing.T) {
	big := strings.Repeat("x", MaxBodyBytes)
	res := validate(t, doc(box("n_root"), fmt.Sprintf(`"meta":{"description":%q}`, big)))
	found := false
	for _, d := range res.Diagnostics {
		if d.Code == CodeLimitExceeded && d.Params["limit"] == "bodyBytes" {
			found = true
		}
	}
	if !found {
		t.Errorf("нет LIMIT_EXCEEDED bodyBytes: %s", mustJSON(res.Diagnostics))
	}
}

func TestSchemaDiagnosticShape(t *testing.T) {
	d := only(t, validate(t, doc(`"n_root":{"id":"n_root","type":"Box","zone":{"id":"z","mode":"SYSTEM","extra":1}}`)), CodeSchemaViolation)
	if d.NodeID != "n_root" || d.Pointer != "/nodes/n_root/zone/extra" || d.Params["keyword"] != "additionalProperties" {
		t.Errorf("диагностика: %s", mustJSON(d))
	}
	if d.Message != "не должно иметь дополнительных полей" {
		t.Errorf("сообщение: %s", d.Message)
	}
}

func TestSchemaMessages(t *testing.T) {
	names := validate(t, doc(box("n_root"), `"localContent":{"bad":{"type":"text","value":{"ru":"x"}}}`))
	if msg := names.Diagnostics[0].Message; !strings.HasPrefix(msg, "недопустимое имя свойства bad: должно соответствовать образцу") {
		t.Errorf("propertyNames: %s", msg)
	}
	forbidden := validate(t, doc(box("n_root"), `"inputs":{}`))
	if msg := forbidden.Diagnostics[0].Message; msg != "поле недопустимо в этом контексте" {
		t.Errorf("false schema: %s", msg)
	}
}

// Точный набор диагностик — сведение ошибок схемы не должно давать шума.
// Совпадает с ожиданиями packages/ir/test/validate.test.ts.
func TestExactDiagnosticsOnCuratedFixtures(t *testing.T) {
	cases := map[string][][2]string{
		"link-javascript-url":     {{"IR_SCHEMA_VIOLATION", "/nodes/n_link/on/click/args/to/url"}},
		"responsive-without-base": {{"IR_SCHEMA_VIOLATION", "/nodes/n_root/design/gap"}},
		"ref-on-non-composed":     {{"IR_SCHEMA_VIOLATION", "/nodes/n_text/ref"}},
		"too-many-actions":        {{"IR_SCHEMA_VIOLATION", "/nodes/n_btn/on/click"}},
		"root-missing":            {{"NODE_NOT_FOUND", "/root"}},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(fixturesDir + "/invalid/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var fx invalidFixture
			if err := json.Unmarshal(data, &fx); err != nil {
				t.Fatal(err)
			}
			var got [][2]string
			for _, d := range ValidateJSON(fx.Document).Diagnostics {
				got = append(got, [2]string{string(d.Code), d.Pointer})
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("получено %v, ожидалось %v", got, want)
			}
		})
	}
}

func TestMessagesForSchemaShapes(t *testing.T) {
	cases := []struct {
		name, node, pointer, keyword string
	}{
		{"неполная зона", `"zone":{"id":"z"}`, "/nodes/n_root/zone", "required"},
		{"лишнее поле объектной привязки", `"bindings":{"text":{"expr":"$content.a","junk":1}}`, "/nodes/n_root/bindings/text/junk", "additionalProperties"},
		{"неверное число аргументов условия", `"when":{"op":"eq","args":["$content.a"]}`, "/nodes/n_root/when/args", "minItems"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := validate(t, doc(fmt.Sprintf(`"n_root":{"id":"n_root","type":"Box",%s}`, tc.node)))
			if len(res.Diagnostics) != 1 || res.Diagnostics[0].Pointer != tc.pointer || res.Diagnostics[0].Params["keyword"] != tc.keyword {
				t.Errorf("получено %s", mustJSON(res.Diagnostics))
			}
		})
	}
}

func TestInvalidJSON(t *testing.T) {
	res := ValidateJSON([]byte(`{"irVersion":`))
	if res.Valid || len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != CodeSchemaViolation || res.Diagnostics[0].Pointer != "" {
		t.Errorf("получено %s", mustJSON(res))
	}
}

func TestNonObjectDocuments(t *testing.T) {
	for _, src := range []string{`null`, `42`, `"x"`, `[]`} {
		res := ValidateJSON([]byte(src))
		if res.Valid || res.Diagnostics[0].Code != CodeSchemaViolation {
			t.Errorf("%s: %s", src, mustJSON(res))
		}
	}
}

func TestPointerHelpers(t *testing.T) {
	if got := Pointer("nodes", "a/b", "c~d", 0); got != "/nodes/a~1b/c~0d/0" {
		t.Errorf("Pointer = %s", got)
	}
	for ptr, want := range map[string]string{"/nodes/a~1b/props": "a/b", "/nodes/n_x": "n_x"} {
		if got, ok := NodeIDFromPointer(ptr); !ok || got != want {
			t.Errorf("NodeIDFromPointer(%s) = %s, %v", ptr, got, ok)
		}
	}
	for _, ptr := range []string{"/meta/nodes/n_x", "/nodes", ""} {
		if _, ok := NodeIDFromPointer(ptr); ok {
			t.Errorf("NodeIDFromPointer(%s) должен вернуть false", ptr)
		}
	}
}

func TestPointerPanicsOnUnsupportedSegment(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("ожидалась паника")
		}
	}()
	Pointer(1.5)
}

func TestNodeIDs(t *testing.T) {
	re := regexp.MustCompile(`^n_[a-z0-9]{8}$`)
	taken := map[string]bool{}
	for i := 0; i < 5000; i++ {
		id := GenerateNodeID(taken)
		if !re.MatchString(id) || !IsNodeID(id) || taken[id] {
			t.Fatalf("плохой или повторный ID %q", id)
		}
		taken[id] = true
	}
	for s, want := range map[string]bool{"n_root": true, "abc": false, "n_with space": false, strings.Repeat("x", 33): false} {
		if IsNodeID(s) != want {
			t.Errorf("IsNodeID(%q) != %v", s, want)
		}
	}
}
