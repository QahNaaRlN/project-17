package manifest

import (
	"encoding/json"
	"reflect"
	"testing"

	"pgregory.net/rapid"
)

func parseDiff(t *testing.T, raw string) any {
	t.Helper()
	v, err := ParseJSON([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// MF-020, MF-021, MF-025: compatibility and schema agreement are independent.
func TestDiffCompatibility(t *testing.T) {
	tests := []struct {
		name, before, after, pointer string
		class                        ChangeClass
	}{
		{"добавление компонента с обязательным свойством", `{}`, `{"components":{"Card":{"props":{"title":{"type":"text","required":true}}}}}`, "/components/Card", Compatible},
		{"удаление компонента", `{"components":{"Card":{}}}`, `{}`, "/components/Card", Breaking},
		{"удаление примитива", `{"primitives":{"Tile":{}}}`, `{}`, "/primitives/Tile", Breaking},
		{"добавление действия", `{}`, `{"actions":{"shop.buy":{}}}`, "/actions/shop.buy", Compatible},
		{"удаление действия", `{"actions":{"shop.buy":{}}}`, `{}`, "/actions/shop.buy", Breaking},
		{"удаление источника", `{"dataSources":{"shop.list":{}}}`, `{}`, "/dataSources/shop.list", Breaking},
		{"удаление форматтера", `{"formatters":{"money":{}}}`, `{}`, "/formatters/money", Breaking},
		{"удаление capability", `{"capabilities":{"shop.buy":{"description":"Buy"}}}`, `{}`, "/capabilities/shop.buy", Breaking},
		{"необязательное свойство", `{"components":{"Card":{}}}`, `{"components":{"Card":{"props":{"title":{"type":"text"}}}}}`, "/components/Card/props/title", Compatible},
		{"обязательное свойство", `{"components":{"Card":{}}}`, `{"components":{"Card":{"props":{"title":{"type":"text","required":true}}}}}`, "/components/Card/props/title", Breaking},
		{"удаление свойства с именем description", `{"components":{"Card":{"props":{"description":{"type":"text"}}}}}`, `{"components":{"Card":{}}}`, "/components/Card/props/description", Breaking},
		{"обязательное свойство с именем default", `{"components":{"Card":{}}}`, `{"components":{"Card":{"props":{"default":{"type":"text","required":true}}}}}`, "/components/Card/props/default", Breaking},
		{"поле схемы с именем version", `{"schemas":{"Product":{"fields":{"version":{"type":"text"}}}}}`, `{"schemas":{"Product":{}}}`, "/schemas/Product/fields/version", Breaking},
		{"версия схемы", `{"schemas":{"Product":{"version":1}}}`, `{"schemas":{"Product":{"version":2}}}`, "/schemas/Product/version", Compatible},
		{"новая схема", `{}`, `{"schemas":{"Product":{"version":1}}}`, "/schemas/Product", Compatible},
		{"удаление схемы", `{"schemas":{"Product":{"version":1}}}`, `{}`, "/schemas/Product", Breaking},
		{"добавление слота", `{"components":{"Card":{}}}`, `{"components":{"Card":{"slots":{"footer":{}}}}}`, "/components/Card/slots/footer", Compatible},
		{"добавление обязательного слота", `{"components":{"Card":{}}}`, `{"components":{"Card":{"slots":{"footer":{"min":1}}}}}`, "/components/Card/slots/footer", Breaking},
		{"добавление необязательного слота", `{"components":{"Card":{}}}`, `{"components":{"Card":{"slots":{"footer":{"min":0}}}}}`, "/components/Card/slots/footer", Compatible},
		{"удаление события", `{"components":{"Card":{"events":{"select":{}}}}}`, `{"components":{"Card":{}}}`, "/components/Card/events/select", Breaking},
		{"добавление события", `{"components":{"Card":{}}}`, `{"components":{"Card":{"events":{"select":{}}}}}`, "/components/Card/events/select", Compatible},
		{"описание события", `{"components":{"Card":{"events":{"select":{"description":"a"}}}}}`, `{"components":{"Card":{"events":{"select":{"description":"b"}}}}}`, "/components/Card/events/select/description", Compatible},
		{"удаление поля события", `{"components":{"Card":{"events":{"select":{"payload":{"title":{"type":"text"}}}}}}}`, `{"components":{"Card":{"events":{"select":{"payload":{}}}}}}`, "/components/Card/events/select/payload/title", Breaking},
		{"добавление поля события", `{"components":{"Card":{"events":{"select":{"payload":{}}}}}}`, `{"components":{"Card":{"events":{"select":{"payload":{"title":{"type":"text","required":true}}}}}}}`, "/components/Card/events/select/payload/title", Compatible},
		{"расширение диапазона поля события", `{"components":{"Card":{"events":{"select":{"payload":{"quantity":{"type":"number","max":1}}}}}}}`, `{"components":{"Card":{"events":{"select":{"payload":{"quantity":{"type":"number","max":2}}}}}}}`, "/components/Card/events/select/payload/quantity/max", Compatible},
		{"группа примитива", `{"primitives":{"Tile":{"group":"Layout"}}}`, `{"primitives":{"Tile":{"group":"Media"}}}`, "/primitives/Tile/group", Compatible},
		{"описание компонента", `{"components":{"Card":{"description":"old"}}}`, `{"components":{"Card":{"description":"new"}}}`, "/components/Card/description", Compatible},
		{"deprecated", `{"components":{"Card":{}}}`, `{"components":{"Card":{"deprecated":{"since":"2.0","message":"Use Tile"}}}}`, "/components/Card/deprecated", Compatible},
		{"sourceRef", `{"actions":{"shop.buy":{"sourceRef":{"file":"old.ts"}}}}`, `{"actions":{"shop.buy":{"sourceRef":{"file":"new.ts"}}}}`, "/actions/shop.buy/sourceRef", Compatible},
		{"app", `{"app":{"version":"1"}}`, `{"app":{"version":"2"}}`, "/app", Compatible},
		{"codeIndex", `{}`, `{"codeIndex":{"uploaded":false}}`, "/codeIndex", Compatible},
		{"manifestVersion", `{"manifestVersion":"1.0"}`, `{"manifestVersion":"2.0"}`, "/manifestVersion", Compatible},
		{"удаление значения по умолчанию", `{"components":{"Card":{"props":{"title":{"type":"text","default":"a"}}}}}`, `{"components":{"Card":{"props":{"title":{"type":"text"}}}}}`, "/components/Card/props/title/default", Breaking},
		{"поле с именем sourceRef", `{"components":{"Card":{"props":{"sourceRef":{"type":"text"}}}}}`, `{"components":{"Card":{}}}`, "/components/Card/props/sourceRef", Breaking},
		{"описание слота", `{"components":{"Card":{"slots":{"footer":{"description":"a"}}}}}`, `{"components":{"Card":{"slots":{"footer":{"description":"b"}}}}}`, "/components/Card/slots/footer/description", Compatible},
		{"метаданные capability", `{"capabilities":{"shop.buy":{"description":"a"}}}`, `{"capabilities":{"shop.buy":{"description":"b"}}}`, "/capabilities/shop.buy/description", Compatible},
		{"сигнатура форматтера", `{"formatters":{"money":{"input":["number"]}}}`, `{"formatters":{"money":{"input":["text"]}}}`, "/formatters/money/input", Breaking},
		{"расширение входов форматтера", `{"formatters":{"money":{"input":["number"]}}}`, `{"formatters":{"money":{"input":["number","text"]}}}`, "/formatters/money/input", Compatible},
		{"удаление container false", `{"components":{"Card":{"container":false}}}`, `{"components":{"Card":{}}}`, "/components/Card/container", Compatible},
		{"удаление container true", `{"components":{"Card":{"container":true}}}`, `{"components":{"Card":{}}}`, "/components/Card/container", Breaking},
		{"удаление responsive false", `{"components":{"Card":{"props":{"title":{"type":"text","responsive":false}}}}}`, `{"components":{"Card":{"props":{"title":{"type":"text"}}}}}`, "/components/Card/props/title/responsive", Compatible},
		{"удаление responsive true", `{"components":{"Card":{"props":{"title":{"type":"text","responsive":true}}}}}`, `{"components":{"Card":{"props":{"title":{"type":"text"}}}}}`, "/components/Card/props/title/responsive", Breaking},
		{"добавление токена", `{}`, `{"tokens":{"spacing":{"sm":"4px"}}}`, "/tokens/spacing", Compatible},
		{"значение токена", `{"tokens":{"spacing":{"sm":"4px"}}}`, `{"tokens":{"spacing":{"sm":"8px"}}}`, "/tokens/spacing/sm", Visual},
		{"удаление токена", `{"tokens":{"spacing":{"sm":"4px"}}}`, `{"tokens":{"spacing":{}}}`, "/tokens/spacing/sm", Breaking},
		{"добавление breakpoint", `{}`, `{"breakpoints":{"sm":640}}`, "/breakpoints/sm", Compatible},
		{"изменение breakpoint", `{"breakpoints":{"sm":640}}`, `{"breakpoints":{"sm":720}}`, "/breakpoints/sm", Visual},
		{"удаление breakpoint", `{"breakpoints":{"sm":640}}`, `{}`, "/breakpoints/sm", Breaking},
		{"удаление версии IR", `{"irVersions":["1.0","2.0"]}`, `{"irVersions":["2.0"]}`, "/irVersions", Breaking},
		{"добавление версии IR", `{"irVersions":["1.0"]}`, `{"irVersions":["1.0","2.0"]}`, "/irVersions", Compatible},
		{"удаление иконки", `{"icons":["cart"]}`, `{}`, "/icons", Breaking},
		{"добавление иконки", `{}`, `{"icons":["cart"]}`, "/icons", Compatible},
		{"добавление поля результата", `{"dataSources":{"shop.list":{"result":{"type":"object","fields":{}}}}}`, `{"dataSources":{"shop.list":{"result":{"type":"object","fields":{"title":{"type":"text","required":true}}}}}}`, "/dataSources/shop.list/result/fields/title", Compatible},
		{"удаление поля результата", `{"dataSources":{"shop.list":{"result":{"type":"object","fields":{"title":{"type":"text"}}}}}}`, `{"dataSources":{"shop.list":{"result":{"type":"object","fields":{}}}}}`, "/dataSources/shop.list/result/fields/title", Breaking},
		{"новый обязательный аргумент", `{"actions":{"shop.buy":{}}}`, `{"actions":{"shop.buy":{"args":{"quantity":{"type":"number","required":true}}}}}`, "/actions/shop.buy/args/quantity", Breaking},
		{"обязательный параметр с именем result", `{"dataSources":{"shop.list":{}}}`, `{"dataSources":{"shop.list":{"params":{"result":{"type":"number","required":true}}}}}`, "/dataSources/shop.list/params/result", Breaking},
		{"добавленный контекст", `{"components":{"Card":{}}}`, `{"components":{"Card":{"provides":{"title":{"type":"text","required":true}}}}}`, "/components/Card/provides/title", Compatible},
		{"удаление контекста", `{"components":{"Card":{"provides":{"title":{"type":"text"}}}}}`, `{"components":{"Card":{}}}`, "/components/Card/provides/title", Breaking},
		{"контейнер сужается", `{"components":{"Card":{"container":true}}}`, `{"components":{"Card":{"container":false}}}`, "/components/Card/container", Breaking},
		{"контейнер расширяется", `{"components":{"Card":{"container":false}}}`, `{"components":{"Card":{"container":true}}}`, "/components/Card/container", Compatible},
		{"pointer escaping", `{"components":{"a/b~c":{}}}`, `{}`, "/components/a~1b~0c", Breaking},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Diff(parseDiff(t, tc.before), parseDiff(t, tc.after))
			if len(got) != 1 || got[0].Pointer != tc.pointer || got[0].Class != tc.class {
				t.Fatalf("diff: %+v", got)
			}
			if SchemasChanged(got) != (got[0].Schema) {
				t.Fatal("schema decision lost")
			}
			if len(BreakingChanges(got)) != boolInt(tc.class == Breaking) {
				t.Fatal("breaking filter")
			}
		})
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// MF-020: widening and narrowing are tested in both directions and at equality.
func TestDiffTypeConstraints(t *testing.T) {
	tests := []struct {
		field, before, after string
		class                ChangeClass
	}{
		{"type", `"text"`, `"number"`, Breaking},
		{"schema", `"Product"`, `"Category"`, Breaking},
		{"min", `1`, `2`, Breaking}, {"min", `2`, `1`, Compatible},
		{"max", `2`, `1`, Breaking}, {"max", `1`, `2`, Compatible},
		{"minLength", `1`, `2`, Breaking}, {"maxLength", `1`, `2`, Compatible},
		{"required", `false`, `true`, Breaking}, {"required", `true`, `false`, Compatible},
		{"integer", `false`, `true`, Breaking}, {"integer", `true`, `false`, Compatible},
		{"unique", `false`, `true`, Breaking}, {"unique", `true`, `false`, Compatible},
		{"responsive", `true`, `false`, Breaking}, {"responsive", `false`, `true`, Compatible},
		{"values", `["a","b"]`, `["a"]`, Breaking}, {"values", `["a"]`, `["a","b"]`, Compatible},
		{"pattern", `"a"`, `"b"`, Breaking},
		{"description", `"a"`, `"b"`, Compatible},
		{"default", `"a"`, `"b"`, Compatible},
	}
	for _, tc := range tests {
		t.Run(tc.field+tc.before+tc.after, func(t *testing.T) {
			before := `{"components":{"Card":{"props":{"value":{"` + tc.field + `":` + tc.before + `}}}}}`
			after := `{"components":{"Card":{"props":{"value":{"` + tc.field + `":` + tc.after + `}}}}}`
			got := Diff(parseDiff(t, before), parseDiff(t, after))
			if len(got) != 1 || got[0].Class != tc.class {
				t.Fatalf("%+v", got)
			}
		})
	}
	for _, field := range []string{"min", "max", "minLength", "maxLength", "pattern", "integer", "required", "unique", "mimeTypes", "marks", "blocks", "schemes", "allowedTypes", "nodeType", "assetKind"} {
		t.Run("remove-"+field, func(t *testing.T) {
			a := `{"components":{"Card":{"props":{"value":{"type":"string","` + field + `":true}}}}}`
			b := `{"components":{"Card":{"props":{"value":{"type":"string"}}}}}`
			got := Diff(parseDiff(t, a), parseDiff(t, b))
			if len(got) != 1 || got[0].Class != Compatible || !got[0].Removed {
				t.Fatalf("%+v", got)
			}
		})
	}
	for _, field := range []string{"min", "max", "minLength", "maxLength", "pattern", "integer", "unique", "mimeTypes", "marks", "blocks", "schemes", "allowedTypes", "nodeType", "assetKind"} {
		t.Run("add-"+field, func(t *testing.T) {
			a := `{"components":{"Card":{"props":{"value":{"type":"string"}}}}}`
			b := `{"components":{"Card":{"props":{"value":{"type":"string","` + field + `":true}}}}}`
			got := Diff(parseDiff(t, a), parseDiff(t, b))
			if len(got) != 1 || got[0].Class != Breaking || !got[0].Added {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestDiffOptionalSectionsAndNumbers(t *testing.T) {
	empty := parseDiff(t, `{}`)
	explicit := parseDiff(t, `{"components":{},"primitives":{},"actions":{},"dataSources":{},"formatters":{},"schemas":{},"capabilities":{},"breakpoints":{},"tokens":{},"icons":[]}`)
	if got := Diff(empty, explicit); len(got) != 0 {
		t.Fatal(got)
	}
	if got := Diff(explicit, empty); len(got) != 0 {
		t.Fatal(got)
	}
	a := parseDiff(t, `{"breakpoints":{"sm":640}}`)
	b := map[string]any{"breakpoints": map[string]any{"sm": float64(640)}}
	if got := Diff(a, b); len(got) != 0 {
		t.Fatal(got)
	}
	if got := Diff(parseDiff(t, `{"x":null}`), parseDiff(t, `{}`)); len(got) != 1 || !got[0].Removed || got[0].Added {
		t.Fatal(got)
	}
}

func TestDiffIsDeterministicAndDoesNotMutate(t *testing.T) {
	// MF-002, MF-020: map insertion order and numeric lexical forms are irrelevant.
	rapid.Check(t, func(t *rapid.T) {
		keys := rapid.SliceOfDistinct(rapid.StringMatching(`[a-z]{1,12}`), func(v string) string { return v }).Draw(t, "keys")
		a := obj{}
		b := obj{}
		for _, k := range keys {
			a[k] = json.Number("1.0")
		}
		for i := len(keys) - 1; i >= 0; i-- {
			b[keys[i]] = json.Number("1")
		}
		left := obj{"components": a}
		right := obj{"components": b}
		snapshot, _ := CanonicalJSON(left)
		if got := Diff(left, right); len(got) != 0 {
			t.Fatalf("identity: %+v", got)
		}
		first := Diff(left, obj{})
		if !reflect.DeepEqual(first, Diff(left, obj{})) {
			t.Fatal("unstable")
		}
		for i := 1; i < len(first); i++ {
			if compareUTF16(first[i-1].Pointer, first[i].Pointer) >= 0 {
				t.Fatal("not sorted")
			}
		}
		unchanged, _ := CanonicalJSON(left)
		if string(snapshot) != string(unchanged) {
			t.Fatal("input mutated")
		}
	})
}

// MF-020, MF-025: the classifier also accepts complete manifests that both
// pass the existing format/semantic validator, including the agreed slot rule.
func TestDiffValidManifestContracts(t *testing.T) {
	makeManifest := func(fragment string) any {
		base := object(parseDiff(t, `{"manifestVersion":"1.0","app":{"id":"store-web","version":"1.0.0","framework":"react"},"irVersions":["1.0"],"breakpoints":{},"tokens":{}}`))
		for k, v := range object(parseDiff(t, fragment)) {
			base[k] = v
		}
		if r := Validate(base); !r.Valid {
			t.Fatalf("invalid fixture: %+v", r.Diagnostics)
		}
		return base
	}
	a := makeManifest(`{"components":{"Card":{"slots":{}}}}`)
	b := makeManifest(`{"components":{"Card":{"slots":{"footer":{"min":1,"allowedTypes":["Box"]}}}}}`)
	if got := Diff(a, b); len(got) != 1 || got[0].Class != Breaking {
		t.Fatal(got)
	}
	a = makeManifest(`{"schemas":{"Product":{"version":1,"fields":{"sku":{"type":"string"}}}}}`)
	b = makeManifest(`{"schemas":{"Product":{"version":1,"fields":{"sku":{"type":"string"},"title":{"type":"text"}}}}}`)
	got := Diff(a, b)
	if len(got) != 1 || got[0].Class != Compatible || !SchemasChanged(got) {
		t.Fatal(got)
	}
	a = makeManifest(`{"components":{"Card":{"props":{"items":{"type":"list","of":{"type":"text"}}}}}}`)
	b = makeManifest(`{"components":{"Card":{"props":{"items":{"type":"list","min":0,"of":{"type":"text"}}}}}}`)
	if got := Diff(a, b); len(got) != 1 || got[0].Class != Compatible {
		t.Fatal(got)
	}
	a = makeManifest(`{"components":{"Card":{"props":{"title":{"type":"text"}}}}}`)
	b = makeManifest(`{"components":{"Card":{"props":{"title":{"type":"text","maxLength":100}}}}}`)
	if got := Diff(a, b); len(got) != 1 || got[0].Class != Breaking {
		t.Fatal(got)
	}
}

func TestDiffSlotsAndNestedTypes(t *testing.T) {
	cases := []struct {
		a, b  string
		class ChangeClass
	}{
		{`{"min":0}`, `{"min":1}`, Breaking}, {`{"max":1}`, `{"max":2}`, Compatible},
		{`{}`, `{"min":0}`, Compatible}, {`{}`, `{"min":1}`, Breaking},
		{`{"allowedTypes":["Box"]}`, `{"allowedTypes":["Box","Text"]}`, Compatible},
	}
	for _, tc := range cases {
		a := parseDiff(t, `{"components":{"Card":{"slots":{"footer":`+tc.a+`}}}}`)
		b := parseDiff(t, `{"components":{"Card":{"slots":{"footer":`+tc.b+`}}}}`)
		got := Diff(a, b)
		if len(got) != 1 || got[0].Class != tc.class {
			t.Fatalf("%+v", got)
		}
	}
	a := parseDiff(t, `{"components":{"Card":{"props":{"items":{"type":"list","of":{"type":"object","fields":{"min":{"type":"text"}}}}}}}}`)
	b := parseDiff(t, `{"components":{"Card":{"props":{"items":{"type":"list","of":{"type":"object","fields":{}}}}}}}`)
	got := Diff(a, b)
	if len(got) != 1 || got[0].Class != Breaking || got[0].Pointer != "/components/Card/props/items/of/fields/min" {
		t.Fatal(got)
	}
}
