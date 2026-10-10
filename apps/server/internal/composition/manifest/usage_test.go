package manifest_test

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"pgregory.net/rapid"
)

// MF-020: извлекаются ссылки IR, но не произвольные строки с теми же именами.
func TestDocumentUses(t *testing.T) {
	active := decode(t, `{"components":{"Card":{"props":{"product":{"type":"reference","schema":"Product"},"tone":{"type":"color","responsive":true},"size":{"type":"number","responsive":true}},"provides":{"value":{"type":"object","fields":{"items":{"type":"list","of":{"type":"reference","schema":"Product"}}}}},"events":{"select":{"payload":{"p":{"type":"reference","schema":"Product"}}}},"capabilities":["commerce"]}},"primitives":{"Custom":{"args":{"x":{"type":"reference","schema":"Product"}}}},"schemas":{"Article":{"fields":{"author":{"type":"reference","schema":"Person"}}},"Person":{"fields":{"self":{"type":"reference","schema":"Person"}}},"Product":{"fields":{"self":{"type":"reference","schema":"Product"}}}},"dataSources":{"shop.list":{"params":{},"result":{"type":"list","of":{"type":"reference","schema":"Product"}}}},"actions":{"buy":{"args":{"p":{"type":"reference","schema":"Product"}}}},"formatters":{"money":{"args":{}}}}`)
	doc := decode(t, `{"irVersion":"1.0","content":{"schema":"Article"},"inputs":{"p":{"type":"reference","schema":"Product"}},"dataSources":{"products":{"source":"shop.list","params":{"limit":{"expr":"$local.x","format":{"fn":"money"}}}}},"nodes":{"n_card":{"type":"Card","name":"Unused","props":{"tone":{"base":"red","md":"blue"},"size":{"base":1,"lg":2}},"bindings":{"price":{"expr":"$data.products.0.price","format":{"fn":"money"}},"text":{"template":"Unused","vars":{"a":{"expr":"$local.x","format":{"fn":"uppercase"}}},"default":{"format":{"fn":"fake"}}}},"when":{"op":"eq","args":[{"lit":{"format":{"fn":"fake"}}},"$local.x"]},"on":{"select":[{"action":"buy","args":{"p":{"expr":"$local.x","format":{"fn":"money"}}}},{"action":"track","args":{"event":{"lit":"fake"}}}]},"design":{"gap":{"base":"sm","md":"lg"},"background":{"raw":"#ff0000"},"paddingTop":"md","opacity":0.5,"states":{"hover":{"color":"blue"}}}},"n_custom":{"type":"Custom"},"n_icon":{"type":"Icon","props":{"name":"cart"}},"n_box":{"type":"Box"}},"localContent":{"t_title":{"type":"text","value":{"ru":"Card buy fake Product"}}}}`)
	beforeDoc, _ := json.Marshal(doc)
	beforeManifest, _ := json.Marshal(active)
	got := manifest.DocumentUses(doc, active)
	required := []manifest.Usage{
		{ManifestPointer: "/components/Card", DocumentPointer: "/nodes/n_card/type"},
		{ManifestPointer: "/primitives/Custom", DocumentPointer: "/nodes/n_custom/type"},
		{ManifestPointer: "/schemas/Article", DocumentPointer: "/content/schema"},
		{ManifestPointer: "/schemas/Person", DocumentPointer: "/content/schema", Indirect: true},
		{ManifestPointer: "/schemas/Product", DocumentPointer: "/nodes/n_card/type", Indirect: true},
		{ManifestPointer: "/schemas/Product", DocumentPointer: "/inputs/p"},
		{ManifestPointer: "/dataSources/shop.list", DocumentPointer: "/dataSources/products/source"},
		{ManifestPointer: "/schemas/Product", DocumentPointer: "/dataSources/products/source", Indirect: true},
		{ManifestPointer: "/actions/buy", DocumentPointer: "/nodes/n_card/on/select/0/action"},
		{ManifestPointer: "/schemas/Product", DocumentPointer: "/nodes/n_card/on/select/0/action", Indirect: true},
		{ManifestPointer: "/capabilities/commerce", DocumentPointer: "/nodes/n_card/type", Indirect: true},
		{ManifestPointer: "/formatters/money", DocumentPointer: "/nodes/n_card/bindings/price/format/fn"},
		{ManifestPointer: "/formatters/money", DocumentPointer: "/nodes/n_card/on/select/0/args/p/format/fn"},
		{ManifestPointer: "/tokens/colors/red", DocumentPointer: "/nodes/n_card/props/tone/base"},
		{ManifestPointer: "/tokens/spacing/sm", DocumentPointer: "/nodes/n_card/design/gap/base"},
		{ManifestPointer: "/tokens/spacing/md", DocumentPointer: "/nodes/n_card/design/paddingTop"},
		{ManifestPointer: "/tokens/colors/blue", DocumentPointer: "/nodes/n_card/design/states/hover/color"},
		{ManifestPointer: "/breakpoints/md", DocumentPointer: "/nodes/n_card/design/gap/md"},
		{ManifestPointer: "/breakpoints/lg", DocumentPointer: "/nodes/n_card/props/size/lg"},
		{ManifestPointer: "/icons", DocumentPointer: "/nodes/n_icon/props/name"},
		{ManifestPointer: "/irVersions", DocumentPointer: "/irVersion"},
	}
	for _, want := range required {
		if !slices.Contains(got, want) {
			t.Errorf("missing %+v in %+v", want, got)
		}
	}
	for _, u := range got {
		if u.ManifestPointer == "/components/Box" || u.ManifestPointer == "/formatters/fake" || u.ManifestPointer == "/tokens/colors/#ff0000" {
			t.Errorf("literal interpreted as reference: %+v", u)
		}
	}
	afterDoc, _ := json.Marshal(doc)
	afterManifest, _ := json.Marshal(active)
	if string(beforeDoc) != string(afterDoc) || string(beforeManifest) != string(afterManifest) {
		t.Fatal("input mutated")
	}
	if !reflect.DeepEqual(got, manifest.DocumentUses(doc, active)) {
		t.Fatal("nondeterministic order")
	}
}

func TestUsedChanges(t *testing.T) {
	changes := []manifest.Change{
		{Pointer: "/components/Card2", Class: manifest.Breaking},
		{Pointer: "/components/Card/props/new", Class: manifest.Breaking, Added: true},
		{Pointer: "/tokens", Class: manifest.Breaking, Removed: true},
		{Pointer: "/tokens/colors/red", Class: manifest.Visual},
		{Pointer: "/components/Card", Class: manifest.Compatible},
	}
	uses := []manifest.Usage{{ManifestPointer: "/components/Card"}, {ManifestPointer: "/tokens/colors/red"}}
	got := manifest.UsedChanges(changes, uses)
	if len(got) != 4 || got[0].Pointer != "/components/Card" || got[3].Pointer != "/tokens/colors/red" {
		t.Fatalf("matched: %+v", got)
	}
	if changes[0].Pointer != "/components/Card2" {
		t.Fatal("sorted caller slice")
	}
	if len(manifest.UsedChanges(changes, nil)) != 0 {
		t.Fatal("empty usage matched")
	}
}

func TestUsageEscapedNamesAndInvalidShapes(t *testing.T) {
	doc := decode(t, `{"nodes":{"n/a~b":{"type":"Card/a~b"},"empty":42},"dataSources":{"bad":null}}`)
	got := manifest.DocumentUses(doc, nil)
	if !slices.Contains(got, manifest.Usage{ManifestPointer: "/components/Card~1a~0b", DocumentPointer: "/nodes/n~1a~0b/type"}) {
		t.Fatalf("pointer escape: %+v", got)
	}
	for _, value := range []any{nil, true, 42, "x", []any{nil}, map[string]any{}} {
		if len(manifest.DocumentUses(value, value)) != 0 {
			t.Fatalf("malformed root: %#v", value)
		}
	}
}

// MF-020: любые JSON не вызывают панику, порядок обхода не влияет на результат.
func TestPropertyDocumentUses(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		doc := jsonValue(3).Draw(t, "document")
		active := jsonValue(3).Draw(t, "manifest")
		got := manifest.DocumentUses(doc, active)
		if !reflect.DeepEqual(got, manifest.DocumentUses(doc, active)) {
			t.Fatal("unstable")
		}
	})
}

func TestUsageDesignDependencies(t *testing.T) {
	active := decode(t, `{"tokens":{"typography":{"body":{"fontSize":{"base":"16px","md":"20px"}}}},"components":{"Card":{"props":{"options":{"type":"object","fields":{"base":{"type":"string"}}}}}}}`)
	doc := decode(t, `{"nodes":{"n_a":{"type":"Card","props":{"options":{"base":"a","fake":"b"}},"design":{"typography":"body","fontWeight":700,"maxWidth":"lg","marginLeft":"auto"}}}}`)
	got := manifest.DocumentUses(doc, active)
	for _, want := range []manifest.Usage{
		{ManifestPointer: "/breakpoints/md", DocumentPointer: "/nodes/n_a/design/typography", Indirect: true},
		{ManifestPointer: "/tokens/typography", DocumentPointer: "/nodes/n_a/design/fontWeight"},
		{ManifestPointer: "/tokens/container/lg", DocumentPointer: "/nodes/n_a/design/maxWidth"},
	} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %+v", want)
		}
	}
	for _, u := range got {
		if u.ManifestPointer == "/breakpoints/fake" || u.ManifestPointer == "/tokens/spacing/auto" {
			t.Fatalf("ordinary object/enum is not a reference: %+v", u)
		}
	}
}

func TestUsageRealFixtures(t *testing.T) {
	raw, err := os.ReadFile("../../../../../packages/ir/fixtures/valid/page-collection.json")
	if err != nil {
		t.Fatal(err)
	}
	got := manifest.DocumentUses(decode(t, string(raw)), load(t, "valid/store.json"))
	for _, p := range []string{"/components/ProductCard", "/actions/commerce.addToCart", "/dataSources/commerce.products.list", "/schemas/Product"} {
		if !slices.ContainsFunc(got, func(u manifest.Usage) bool { return u.ManifestPointer == p }) {
			t.Fatalf("real fixture missing %s: %+v", p, got)
		}
	}
}

func TestUsageNestedInputPointer(t *testing.T) {
	doc := decode(t, `{"inputs":{"nested":{"type":"object","fields":{"products":{"type":"list","of":{"type":"reference","schema":"Product"}}}}}}`)
	got := manifest.DocumentUses(doc, nil)
	if !slices.Contains(got, manifest.Usage{ManifestPointer: "/schemas/Product", DocumentPointer: "/inputs/nested/fields/products/of"}) {
		t.Fatalf("pointer must address actual input definition: %+v", got)
	}
}

// MF-020: builtin properties have dependencies even though their types are not in manifest.
func TestBuiltinPropertyImpact(t *testing.T) {
	doc := decode(t, `{"nodes":{"n_root":{"type":"Stack","props":{"direction":{"base":"vertical","md":"horizontal"}}},"n_size":{"type":"Container","props":{"size":"prose"}}}}`)
	active := decode(t, `{"breakpoints":{"md":768},"tokens":{"container":{"prose":"65ch"}}}`)
	got := manifest.DocumentUses(doc, active)
	for _, want := range []manifest.Usage{
		{ManifestPointer: "/breakpoints/md", DocumentPointer: "/nodes/n_root/props/direction/md"},
		{ManifestPointer: "/tokens/container/prose", DocumentPointer: "/nodes/n_size/props/size"},
	} {
		if !slices.Contains(got, want) {
			t.Fatalf("missing %+v in %+v", want, got)
		}
	}
}
