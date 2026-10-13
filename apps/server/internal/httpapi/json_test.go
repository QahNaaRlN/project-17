package httpapi_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

// STU-091/093/094: TS diff fixture runs on the public Command API, real PostgreSQL and history.
func TestStudioJSONDiffContract(t *testing.T) {
	raw, err := os.ReadFile("../../../studio/test/fixtures/json-diff.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Before, After map[string]any
		Operations    []map[string]any
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	nested, err := ir.ToNested(fixture.Before)
	if err != nil {
		t.Fatal(err)
	}
	e := setup(t)
	contract := cmstest.Env{T: t, Pool: e.pool, Q: store.New(e.pool), Admin: workspaceActor(t, e)}
	contract.ActivateManifest("staging", []byte(strings.Replace(cmstest.DefaultManifest, `"tokens":{}`, `"tokens":{"spacing":{"sm":"12px"}}`, 1)))
	cs := e.command(t, "create-changeset", `{"title":"JSON","targets":["staging"]}`)["id"].(string)
	payload, _ := json.Marshal(map[string]any{"changesetId": cs, "expectedSeq": 0, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": "/json", "root": nested["root"]}}}})
	created := e.command(t, "apply-operations", string(payload))
	doc := created["operations"].([]any)[0].(map[string]any)["target"].(string)
	for _, op := range fixture.Operations {
		op["target"] = doc
	}
	read := func() map[string]any {
		r, data := e.do(t, "GET", "/api/v1/documents/"+doc+"?changesetId="+cs, "", e.authed(nil))
		if r.StatusCode != 200 {
			t.Fatal(data)
		}
		return data["body"].(map[string]any)
	}
	original := read()
	payload, _ = json.Marshal(map[string]any{"payload": map[string]any{"changesetId": cs, "expectedSeq": 1, "group": true, "operations": fixture.Operations}, "reason": "Studio JSON diff"})
	headers := e.authed(map[string]string{"Idempotency-Key": "json-exact"})
	for i := 0; i < 2; i++ {
		r, data := e.do(t, "POST", "/api/v1/commands/apply-operations", string(payload), headers)
		if r.StatusCode != 200 {
			t.Fatal(data)
		}
		if i == 1 && r.Header.Get("Idempotent-Replayed") != "true" {
			t.Fatal("not replayed")
		}
	}
	if actual := read(); !reflect.DeepEqual(actual, fixture.After) {
		a, _ := json.Marshal(actual)
		t.Fatalf("JSON result %s", a)
	}
	_, history := e.do(t, "GET", "/api/v1/changesets/"+cs+"/history", "", e.authed(nil))
	if len(history["items"].([]any)) != 2 || len(history["items"].([]any)[0].(map[string]any)["operations"].([]any)) != len(fixture.Operations) {
		t.Fatal(history)
	}
	count := len(fixture.Operations)
	seq := 1 + count
	p, _ := json.Marshal(map[string]any{"changesetId": cs, "expectedSeq": seq})
	e.command(t, "undo", string(p))
	if !reflect.DeepEqual(read(), original) {
		t.Fatal("undo did not restore whole JSON action")
	}
	seq += count
	p, _ = json.Marshal(map[string]any{"changesetId": cs, "expectedSeq": seq})
	e.command(t, "redo", string(p))
	if !reflect.DeepEqual(read(), fixture.After) {
		t.Fatal("redo did not restore whole JSON action")
	}
	// A rejected second operation rolls back a preceding valid rename in the same group.
	seq += count
	p, _ = json.Marshal(map[string]any{"payload": map[string]any{"changesetId": cs, "expectedSeq": seq, "group": true, "operations": []any{map[string]any{"type": "node.rename", "target": doc, "payload": map[string]any{"nodeId": "n_heading", "name": "must roll back"}}, map[string]any{"type": "node.move", "target": doc, "payload": map[string]any{"nodeId": "n_root", "parentId": "n_tail"}}}}})
	r, _ := e.do(t, "POST", "/api/v1/commands/apply-operations", string(p), e.authed(map[string]string{"Idempotency-Key": "json-invalid"}))
	if r.StatusCode < 400 || !reflect.DeepEqual(read(), fixture.After) {
		t.Fatal("batch not atomic")
	}
	var actualSeq int
	if err := e.pool.QueryRow(context.Background(), "SELECT seq FROM changesets WHERE id=$1", cs).Scan(&actualSeq); err != nil || actualSeq != seq {
		t.Fatal(actualSeq, err)
	}
}
