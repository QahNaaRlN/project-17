package capabilities

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

func TestContextSelectionIsBoundedSafeAndDeterministic(t *testing.T) {
	f := setup(t)
	r := f.create()
	ctx := context.Background()
	s, err := loadContext(ctx, f.e.Q, f.a, r, 1, f.hash, true)
	if err != nil {
		t.Fatal(err)
	}
	index := object{"repo": object{"root": ".", "commit": strings.Repeat("a", 40)}, "elements": object{}, "conventions": object{"registry": "src/registry.ts", "testCommand": "pnpm test", "manifestCommand": "cms build"}}
	actions := object{}
	elements := obj(index["elements"])
	for i := 0; i < 110; i++ {
		name := fmt.Sprintf("store.action%03d", i)
		path := fmt.Sprintf("src/actions/a%03d.ts", i)
		actions[name] = object{"args": object{"value": object{"type": "string", "default": "private-default"}}, "sourceRef": object{"file": path, "export": "run"}}
		elements["action:"+name] = object{"definition": object{"file": "src/registry.ts", "line": i + 1}, "implementation": object{"file": path, "export": "run"}, "dependencies": []string{fmt.Sprintf("src/dependencies/a%03d.ts", i)}, "tests": []string{fmt.Sprintf("test/a%03d.test.ts", i)}}
	}
	s.app["actions"] = actions
	// Build a wide/deep structural snapshot without copying any props/loader data.
	nodes := obj(s.doc["nodes"])
	node := obj(nodes[r.NodeID])
	node["type"] = "ContainerAction"
	s.app["components"] = object{"ContainerAction": object{"container": true, "events": object{"click": object{}}}}
	children := []any{}
	for i := 0; i < 105; i++ {
		id := fmt.Sprintf("n_child%03d", i)
		nodes[id] = object{"id": id, "type": "Button", "props": object{"label": "private-literal"}, "on": object{"click": []any{object{"action": "store.action001"}}}}
		children = append(children, id)
	}
	node["children"] = children
	s.doc["localContent"] = object{"secret": object{"type": "text", "value": object{"ru": "private-local"}}}
	s.doc["dataSources"] = object{"items": object{"source": "store.items", "params": object{"token": object{"lit": "private-loader"}}}}
	s.app["dataSources"] = object{"store.items": object{"params": object{}, "result": object{"type": "string"}}}
	rawIndex := raw(index)
	parsed, err := manifest.ParseJSON(rawIndex)
	if err != nil {
		t.Fatal(err)
	}
	indexHash, _ := manifest.Hash(parsed)
	id := uuid.New()
	key := id.String()
	s.m.CodeIndexKey = &key
	if err = f.e.Q.InsertCodeIndexUpload(ctx, store.InsertCodeIndexUploadParams{ID: id, ProjectID: r.ProjectID, ManifestHash: s.m.Hash, IndexHash: indexHash, RepoCommit: strings.Repeat("a", 40), Body: rawIndex, CreatedBy: f.e.Admin.ID}); err != nil {
		t.Fatal(err)
	}
	p, err := buildPack(ctx, f.e.Q, r, s)
	if err != nil {
		t.Fatal(err)
	}
	again, err := buildPack(ctx, f.e.Q, r, s)
	if err != nil || p.Hash != again.Hash || string(p.Body) != string(again.Body) {
		t.Fatal("nondeterministic pack", err)
	}
	value := parse(p.Body)
	if len(arr(value["files"])) != 40 || len(arr(value["contracts"])) != 100 || obj(value["anchor"])["truncated"] != true || obj(value["fileSelection"])["truncated"] != true || obj(value["contractSelection"])["truncated"] != true {
		t.Fatal("limits", value["fileSelection"], value["contractSelection"])
	}
	if strings.Contains(string(p.Body), "private-") {
		t.Fatal("secret defaults/content escaped projection")
	}
	files := arr(value["files"])
	if obj(files[0])["path"] != "src/actions/a000.ts" {
		t.Fatal("unstable priority", files[0])
	}
	// Return selected implementation exports/lines only for included paths.
	if len(obj(value["codeElements"])) == 0 {
		t.Fatal("metadata absent")
	}
	// Oversize contracts fail before saving any immutable pack.
	for _, contract := range actions {
		obj(contract)["description"] = strings.Repeat("x", 8000)
	}
	_, err = buildPack(ctx, f.e.Q, r, s)
	expectCode(t, err, "CONTEXT_PACK_LIMIT_EXCEEDED")
	// Explicit schema references must exist in the active manifest.
	r.ProposedContract = raw(object{"args": object{"article": object{"type": "reference", "schema": "Absent"}}})
	_, err = loadContext(ctx, f.e.Q, f.a, r, 1, f.hash, true)
	expectCode(t, err, "CAPABILITY_SCHEMA_MISSING")
}

func TestPackIncludesScopedSlotsAndSelectedSourceMetadata(t *testing.T) {
	f := setup(t)
	r := f.create()
	ctx := context.Background()
	s, err := loadContext(ctx, f.e.Q, f.a, r, 1, f.hash, true)
	if err != nil {
		t.Fatal(err)
	}
	nodes := obj(s.doc["nodes"])
	obj(nodes[r.NodeID])["slots"] = object{"body": []any{"n_named", "n_absent"}}
	nodes["n_named"] = object{"id": "n_named", "name": "Named child", "type": "Native", "children": []any{"n_named"}, "on": object{"click": object{"action": "analytics.track"}}}
	s.app["components"] = object{"Native": object{"props": object{"text": object{"type": "text"}}}}
	s.app["actions"] = object{"analytics.track": object{"args": object{}}}
	s.app["schemas"] = object{"Article": object{"fields": object{"title": object{"type": "text"}}}}
	r.ProposedContract = raw(object{"args": object{"article": object{"type": "reference", "schema": "Article"}}})
	index := object{"repo": object{"root": ".", "commit": strings.Repeat("a", 40)}, "elements": object{"component:Native": object{
		"definition": object{"file": "src/registry.ts", "line": 1}, "implementation": object{"file": "src/native.ts", "export": "Native"},
		"tests": []any{"test/native.test.ts"}, "dependencies": []any{"src/helper.ts"},
	}}, "conventions": object{"registry": "src/registry.ts", "testCommand": "pnpm test", "manifestCommand": "cms build"}}
	indexValue, err := manifest.ParseJSON(raw(index))
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := manifest.Hash(indexValue)
	id := uuid.New()
	key := id.String()
	s.m.CodeIndexKey = &key
	if err = f.e.Q.InsertCodeIndexUpload(ctx, store.InsertCodeIndexUploadParams{ID: id, ProjectID: r.ProjectID, ManifestHash: s.m.Hash, IndexHash: hash, RepoCommit: strings.Repeat("a", 40), Body: raw(index), CreatedBy: f.e.Admin.ID}); err != nil {
		t.Fatal(err)
	}
	p, err := buildPack(ctx, f.e.Q, r, s)
	if err != nil {
		t.Fatal(err)
	}
	value := parse(p.Body)
	if len(arr(obj(value["anchor"])["subtree"])) != 2 || !strings.Contains(string(p.Body), "Named child") {
		t.Fatal("slot structure missing", string(p.Body))
	}
	metadata := obj(obj(value["codeElements"])["component:Native"])
	if len(arr(metadata["tests"])) != 1 || len(arr(metadata["dependencies"])) != 1 {
		t.Fatal(metadata)
	}
	if !strings.Contains(string(p.Body), `"key":"Article"`) {
		t.Fatal("referenced schema absent")
	}
	if !strings.Contains(string(p.Body), `"key":"analytics.track"`) {
		t.Fatal("single handler's contract absent")
	}
	badKey := "invalid"
	s.m.CodeIndexKey = &badKey
	_, err = buildPack(ctx, f.e.Q, r, s)
	expectCode(t, err, "CAPABILITY_INDEX_UNAVAILABLE")
	missing := uuid.NewString()
	s.m.CodeIndexKey = &missing
	_, err = buildPack(ctx, f.e.Q, r, s)
	expectCode(t, err, "CAPABILITY_INDEX_UNAVAILABLE")
	s.m.CodeIndexKey = &key
	f.e.Exec(`UPDATE code_index_uploads SET body='{}' WHERE id=$1`, id)
	if _, err = buildPack(ctx, f.e.Q, r, s); err == nil {
		t.Fatal("corrupt stored index accepted")
	}
}

func TestNestedContractProjectionExcludesDefaultsAtEveryLevel(t *testing.T) {
	input := object{"type": "object", "fields": object{"default": object{"type": "text", "default": "private-field"}},
		"of": object{"type": "text", "example": "private-of"}, "result": object{"type": "text", "examples": []any{"private-result"}},
		"extension": object{"nested": object{"default": "private-extension", "description": "safe"}},
	}
	projected := typeProjection(input, 0)
	if strings.Contains(string(raw(projected)), "private-") || obj(obj(projected["fields"])["default"])["type"] != "text" || obj(projected["of"])["type"] != "text" {
		t.Fatal(projected)
	}
	if len(typeProjection(input, 9)) != 0 {
		t.Fatal("depth limit ignored")
	}
}
