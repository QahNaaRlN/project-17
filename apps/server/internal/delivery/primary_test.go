package delivery

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestPrimaryProjectionSTU210211216(t *testing.T) {
	field := func(kind string, localized bool) map[string]any {
		return map[string]any{"type": kind, "localized": localized}
	}
	schema := map[string]any{"fields": map[string]any{"title": field("text", true), "empty": field("text", false), "nil": field("text", false), "flag": field("boolean", false), "zero": field("number", false), "missing": field("text", true), "secret": field("text", false), "nested": map[string]any{"type": "object", "fields": map[string]any{"name": field("text", true)}}, "ref": map[string]any{"type": "reference"}}}
	data := map[string]any{"title": map[string]any{"ru": "Заголовок", "en": "Title"}, "empty": "", "nil": nil, "flag": false, "zero": 0, "missing": map[string]any{"en": "English only"}, "secret": "private", "nested": map[string]any{"name": map[string]any{"ru": "Имя"}}, "ref": map[string]any{"entityId": "private"}}
	bindings := map[string]any{}
	for _, path := range []string{"title", "empty", "nil", "flag", "zero", "missing", "nested.name", "ref.title"} {
		bindings[path] = "$content." + path
	}
	doc := map[string]any{"nodes": map[string]any{"n_root": map[string]any{"bindings": bindings, "props": map[string]any{"text": "$content.secret"}}}}
	got, ds, err := projectPrimary(doc, schema, data, "ru")
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]any{"title": "Заголовок", "empty": "", "nil": nil, "flag": false, "zero": 0, "nested": map[string]any{"name": "Имя"}}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("projection %#v", got)
	}
	if len(ds) != 2 || ds[0].Code != "BINDING_VALUE_MISSING" || ds[1].Code != "BINDING_PATH_UNRESOLVED" {
		t.Fatalf("diagnostics %#v", ds)
	}
	en, _, err := projectPrimary(doc, schema, data, "en")
	if err != nil || en["title"] != "Title" || en["missing"] != "English only" {
		t.Fatal(en, err)
	}
	if data["secret"] != "private" {
		t.Fatal("mutated source")
	}
}

func TestPrimaryProjectionLimitsSTU210(t *testing.T) {
	fields, bindings, data := map[string]any{}, map[string]any{}, map[string]any{}
	for i := 0; i < 201; i++ {
		key := fmt.Sprintf("f%d", i)
		fields[key] = map[string]any{"type": "text"}
		bindings[key] = "$content." + key
		data[key] = "x"
	}
	schema := map[string]any{"fields": fields}
	doc := map[string]any{"nodes": map[string]any{"n_root": map[string]any{"bindings": bindings}}}
	if _, _, err := projectPrimary(doc, schema, data, "ru"); err == nil {
		t.Fatal("201 paths accepted")
	}
	delete(bindings, "f200")
	if _, _, err := projectPrimary(doc, schema, data, "ru"); err != nil {
		t.Fatal(err)
	}
	data["f0"] = strings.Repeat("x", 1024*1024)
	if _, _, err := projectPrimary(doc, schema, data, "ru"); err == nil {
		t.Fatal("oversized projection accepted")
	}
}

func TestPrimaryDependenciesSTU209217(t *testing.T) {
	schema := map[string]any{"fields": map[string]any{"name": map[string]any{"type": "text"}}}
	doc := map[string]any{"nodes": map[string]any{"n_root": map[string]any{"bindings": map[string]any{"text": map[string]any{"template": "{a}", "vars": map[string]any{"a": "$content.name"}}}, "when": map[string]any{"eq": []any{"$content.name", map[string]any{"lit": "$content.secret"}}}}}, "dataSources": map[string]any{"test": map[string]any{"params": map[string]any{"title": "$content.name"}}}}
	got, ds, err := projectPrimary(doc, schema, map[string]any{"name": "Only name"}, "ru")
	if err != nil || len(ds) > 0 || len(got) != 1 || got["name"] != "Only name" {
		t.Fatal(got, ds, err)
	}
}
