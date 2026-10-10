package manifest_test

import (
	"bytes"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"pgregory.net/rapid"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
)

func decode(t *testing.T, src string) any {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func canonical(t *testing.T, v any) string {
	t.Helper()
	out, err := manifest.CanonicalJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestCanonicalJSON(t *testing.T) {
	for src, want := range map[string]string{
		`{"b":1,"a":[true,null,"x"],"":{"z":0,"y":-1.5}}`: `{"":{"y":-1.5,"z":0},"a":[true,null,"x"],"b":1}`,
		`"\u2028<&>\"\\\n\u0001\b\f\r\t😀"`:                "\"\u2028<&>\\\"\\\\\\n\\u0001\\b\\f\\r\\t😀\"",
		`[1e21, 0.1, -0, 1.0, 100, 1e-7]`:                 `[1e+21,0.1,0,1,100,1e-7]`,
		`{"\uffff":1,"😀":2,"b":3}`:                        "{\"b\":3,\"😀\":2,\"\uffff\":1}",
	} {
		if got := canonical(t, decode(t, src)); got != want {
			t.Errorf("%s:\n получено %s\nожидалось %s", src, got, want)
		}
	}
	if got, _ := manifest.CanonicalJSON(map[string]any{"a": 1.5, "b": false}); string(got) != `{"a":1.5,"b":false}` {
		t.Errorf("float64: %s", got)
	}
	if _, err := manifest.CanonicalJSON(map[string]any{"x": 1}); err == nil {
		t.Error("int — не значение из JSON")
	}
	if _, err := manifest.CanonicalJSON([]any{json.Number("x")}); err == nil {
		t.Error("некорректное число")
	}
	if _, err := manifest.CanonicalJSON(math.Inf(1)); err == nil {
		t.Error("бесконечность")
	}
	if _, err := manifest.Hash(struct{}{}); err == nil {
		t.Error("Hash: значение не из JSON")
	}
	if h, _ := manifest.Hash(map[string]any{}); h != "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a" {
		t.Errorf("Hash({}): %s", h)
	}
}

// jsonValue — случайное значение JSON (как fc.jsonValue в тестах @cms/manifest).
func jsonValue(depth int) *rapid.Generator[any] {
	return rapid.Custom(func(t *rapid.T) any {
		kinds := 5
		if depth > 0 {
			kinds = 7
		}
		switch rapid.IntRange(0, kinds-1).Draw(t, "kind") {
		case 0:
			return nil
		case 1:
			return rapid.Bool().Draw(t, "bool")
		case 2:
			return rapid.String().Draw(t, "string")
		case 3:
			return json.Number(strconv.FormatInt(rapid.Int64().Draw(t, "int"), 10))
		case 4:
			return json.Number(strconv.FormatFloat(rapid.Float64Range(-1e6, 1e6).Draw(t, "f"), 'g', -1, 64))
		case 5:
			return rapid.SliceOfN(jsonValue(depth-1), 0, 4).Draw(t, "list")
		default:
			return rapid.MapOfN(rapid.String(), jsonValue(depth-1), 0, 4).Draw(t, "map")
		}
	})
}

// Канонический JSON — корректный JSON того же значения; повторная канонизация — тождество.
func TestPropertyCanonicalRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		v := jsonValue(3).Draw(t, "value")
		first, err := manifest.CanonicalJSON(v)
		if err != nil {
			t.Fatal(err)
		}
		reparsed, err := jsonschema.UnmarshalJSON(bytes.NewReader(first))
		if err != nil {
			t.Fatalf("не JSON: %s", first)
		}
		second, _ := manifest.CanonicalJSON(reparsed)
		if !bytes.Equal(first, second) {
			t.Fatalf("канонизация не идемпотентна:\n%s\n%s", first, second)
		}
	})
}

func TestValidateJSON(t *testing.T) {
	r := manifest.ValidateJSON([]byte("{"))
	if r.Valid || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != manifest.CodeSchemaViolation {
		t.Errorf("%+v", r)
	}
	r = manifest.ValidateJSON([]byte(`42`))
	if r.Valid || r.Diagnostics[0].Pointer != "" {
		t.Errorf("не объект: %+v", r)
	}
	ok := manifest.ValidateJSON([]byte(`{"manifestVersion":"1.0","app":{"id":"a","version":"1","framework":"react"},"irVersions":["0.9","1.0"],"breakpoints":{},"tokens":{}}`))
	if !ok.Valid || len(ok.Diagnostics) != 0 {
		t.Errorf("одной поддерживаемой версии IR достаточно: %+v", ok)
	}
}

func TestDiagnosticsSortedAndParams(t *testing.T) {
	m := load(t, "valid/store.json").(map[string]any)
	comps := m["components"].(map[string]any)
	comps["Image"] = map[string]any{}
	m["primitives"].(map[string]any)["Image"] = map[string]any{}
	limit := m["dataSources"].(map[string]any)["commerce.products.list"].(map[string]any)["params"].(map[string]any)["limit"].(map[string]any)
	limit["min"] = json.Number("50")
	r := manifest.Validate(m)
	ptrs := make([]string, len(r.Diagnostics))
	for i, d := range r.Diagnostics {
		ptrs[i] = d.Pointer
	}
	if !slices.IsSorted(ptrs) {
		t.Errorf("не отсортировано: %v", ptrs)
	}
	var codes []string
	for _, d := range r.Diagnostics {
		if d.Pointer == "/components/Image" {
			codes = append(codes, d.Code)
		}
		if d.Code == manifest.CodeRangeInvalid && (d.Params["min"] != 50.0 || d.Params["max"] != 48.0) {
			t.Errorf("параметры: %v", d.Params)
		}
	}
	if !slices.Equal(codes, []string{manifest.CodeNameDuplicate, manifest.CodeNameReserved}) {
		t.Errorf("коды одного места: %v", codes)
	}
}

func TestBoundariesAreValid(t *testing.T) {
	m := load(t, "valid/store.json").(map[string]any)
	get := func(path ...string) map[string]any {
		var cur any = m
		for _, p := range path {
			cur = cur.(map[string]any)[p]
		}
		return cur.(map[string]any)
	}
	get("actions", "commerce.addToCart", "args", "quantity")["default"] = json.Number("99")
	get("primitives", "PriceTag", "props", "currency")["default"] = "USD"
	get("components", "ProductCard", "props", "badge")["default"] = strings.Repeat("я", 40)
	get("components", "ProductCard", "slots", "footer")["min"] = json.Number("2")
	get("schemas", "Product", "migrateFrom")["2"] = []any{map[string]any{"op": "drop", "field": "x"}}
	params := get("dataSources", "commerce.products.list", "params", "limit")
	params["min"], params["default"] = json.Number("48"), json.Number("48")
	gallery := get("components", "Gallery", "props")
	gallery["caption"] = map[string]any{"type": "string", "default": "x"}
	gallery["size"] = map[string]any{"type": "number", "default": json.Number("-5")}
	gallery["title"] = map[string]any{"type": "text", "default": "длинный текст", "content": true}
	gallery["body"] = map[string]any{"type": "richText", "content": true}
	gallery["link"] = map[string]any{"type": "link", "content": true}
	fields := get("schemas", "Product", "fields")
	fields["sku"] = map[string]any{"type": "text", "unique": true}
	fields["code"] = map[string]any{"type": "number", "unique": true}
	if r := manifest.Validate(m); len(r.Diagnostics) != 0 {
		t.Errorf("%+v", r.Diagnostics)
	}
}

func TestNestedChecks(t *testing.T) {
	m := load(t, "valid/store.json").(map[string]any)
	card := m["components"].(map[string]any)["ProductCard"].(map[string]any)
	card["events"] = map[string]any{"select": map[string]any{"payload": map[string]any{
		"item": map[string]any{"type": "reference", "schema": "Nope"}}}}
	card["provides"].(map[string]any)["extra"] = map[string]any{"type": "enum", "values": []any{"a"}, "default": "b"}
	var got [][2]string
	for _, d := range manifest.Validate(m).Diagnostics {
		got = append(got, [2]string{d.Code, d.Pointer})
	}
	want := [][2]string{
		{manifest.CodeUnknownSchema, "/components/ProductCard/events/select/payload/item/schema"},
		{manifest.CodeDefaultInvalid, "/components/ProductCard/provides/extra/default"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("получено %v", got)
	}
}

func TestPointer(t *testing.T) {
	if got := manifest.Pointer("a/b", 2, "c~d"); got != "/a~1b/2/c~0d" {
		t.Error(got)
	}
}
