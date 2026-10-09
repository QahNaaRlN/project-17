package ir

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"regexp"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func decode(t *testing.T, src string) map[string]any {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	return v.(map[string]any)
}

const nestedSample = `{
  "irVersion": "1.0", "kind": "page",
  "root": {"type": "Container", "children": [
    {"type": "Heading", "props": {"level": 1}, "bindings": {"text": "$content.title"}},
    {"id": "n_modal1", "type": "Modal", "children": [{"type": "Text", "bindings": {"text": "$content.lead"}}]},
    {"type": "Button", "on": {"click": {"action": "openModal", "args": {"target": {"lit": "n_modal1"}}}}}
  ]}
}`

func TestNormalizeProducesValidDocument(t *testing.T) {
	doc, err := Normalize(decode(t, nestedSample), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res := ValidateDocument(doc); !res.Valid {
		t.Fatalf("диагностики: %s", mustJSON(res.Diagnostics))
	}
	nodes := doc["nodes"].(map[string]any)
	if len(nodes) != 5 {
		t.Fatalf("узлов %d, ожидалось 5", len(nodes))
	}
	idRe := regexp.MustCompile(`^(n_[a-z0-9]{8}|n_modal1)$`)
	for id := range nodes {
		if !idRe.MatchString(id) {
			t.Errorf("неожиданный ID %q", id)
		}
	}
	root := nodes[doc["root"].(string)].(map[string]any)
	if got := root["children"].([]any)[1]; got != "n_modal1" {
		t.Errorf("заданный ID не сохранён или нарушен порядок: %v", got)
	}
}

func TestNormalizePassesTakenIDsToGenerator(t *testing.T) {
	var seen []int
	i := 0
	gen := func(taken map[string]bool) string {
		seen = append(seen, len(taken))
		id := []string{"n_gen0", "n_gen1", "n_gen2"}[i]
		i++
		return id
	}
	doc, err := Normalize(decode(t, `{"irVersion":"1.0","kind":"page","root":{"type":"Box","children":[{"type":"Box"},{"type":"Box"}]}}`), gen)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, []int{0, 1, 2}) {
		t.Errorf("генератор видел занятых ID: %v", seen)
	}
	if doc["root"] != "n_gen0" {
		t.Errorf("root = %v", doc["root"])
	}
}

func TestNormalizeRejectsBadIDs(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want [][2]string // code, pointer
	}{
		{"дубликат и недопустимый ID",
			`{"root":{"id":"n_same","type":"Box","children":[{"id":"n_same","type":"Box"},{"id":"x","type":"Box"}]}}`,
			[][2]string{{"NODE_ID_DUPLICATE", "/root/children/0/id"}, {"IR_SCHEMA_VIOLATION", "/root/children/1/id"}}},
		{"дубликат внутри слота",
			`{"root":{"id":"n_card","type":"ProductCard","slots":{"footer":[{"id":"n_card","type":"Button"}]}}}`,
			[][2]string{{"NODE_ID_DUPLICATE", "/root/slots/footer/0/id"}}},
		{"ребёнок не узел", `{"root":{"type":"Box","children":[42]}}`,
			[][2]string{{"IR_SCHEMA_VIOLATION", "/root/children/0"}}},
		{"children не массив", `{"root":{"type":"Box","children":{}}}`,
			[][2]string{{"IR_SCHEMA_VIOLATION", "/root/children"}}},
		{"slots не объект", `{"root":{"type":"Box","slots":[]}}`,
			[][2]string{{"IR_SCHEMA_VIOLATION", "/root/slots"}}},
		{"корень не узел", `{"root":"n_root"}`,
			[][2]string{{"IR_SCHEMA_VIOLATION", "/root"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Normalize(decode(t, tc.src), nil)
			var nerr *NormalizeError
			if !errors.As(err, &nerr) {
				t.Fatalf("ожидалась NormalizeError, получено %v", err)
			}
			var got [][2]string
			for _, d := range nerr.Diagnostics {
				got = append(got, [2]string{string(d.Code), d.Pointer})
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("получено %v, ожидалось %v", got, tc.want)
			}
		})
	}
}

func TestNormalizeErrorMessage(t *testing.T) {
	_, err := Normalize(decode(t, `{"root":{"id":"bad","type":"Box"}}`), nil)
	if err == nil || err.Error() != "Недопустимый ID узла: bad" {
		t.Fatalf("сообщение: %v", err)
	}
}

func TestToNestedRoundTripOnFixtures(t *testing.T) {
	for _, path := range fixtureFiles(t, "valid") {
		t.Run(fixtureName(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			doc := decode(t, string(data))
			nested, err := ToNested(doc)
			if err != nil {
				t.Fatal(err)
			}
			back, err := Normalize(nested, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(back, doc) {
				t.Errorf("Normalize(ToNested(doc)) != doc\n%s\n%s", mustJSON(back), mustJSON(doc))
			}
		})
	}
}

func TestToNestedErrors(t *testing.T) {
	_, err := ToNested(decode(t, `{"root":"n_root","nodes":{"n_root":{"id":"n_root","type":"Box","children":["n_gone"]}}}`))
	var nerr *NormalizeError
	if !errors.As(err, &nerr) || nerr.Diagnostics[0].Code != CodeNodeNotFound || nerr.Diagnostics[0].Pointer != "/nodes/n_root/children/0" {
		t.Errorf("отсутствующий узел: %v", err)
	}
	_, err = ToNested(decode(t, `{"root":"n_root","nodes":{"n_root":{"id":"n_root","type":"Box","slots":{"s":["n_aaaa"]}},"n_aaaa":{"id":"n_aaaa","type":"Box","children":["n_root"]}}}`))
	if !errors.As(err, &nerr) || nerr.Diagnostics[0].Code != CodeNodeCycle {
		t.Errorf("цикл: %v", err)
	}
}

func TestToNestedSharedNodeIsNotACycle(t *testing.T) {
	nested, err := ToNested(decode(t, `{"root":"n_root","nodes":{
		"n_root":{"id":"n_root","type":"Box","children":["n_aaaa","n_bbbb"]},
		"n_aaaa":{"id":"n_aaaa","type":"Box","children":["n_cccc"]},
		"n_bbbb":{"id":"n_bbbb","type":"Box","children":["n_cccc"]},
		"n_cccc":{"id":"n_cccc","type":"Box"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(nested["root"].(map[string]any)["children"].([]any)); n != 2 {
		t.Errorf("детей корня: %d", n)
	}
}
