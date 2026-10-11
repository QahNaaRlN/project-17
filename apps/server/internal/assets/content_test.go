package assets

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

func cs(e *cmstest.Env) uuid.UUID {
	var c changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "asset files", "targets": []string{"staging"}}, &c)
	return c.ID
}
func operation(e *cmstest.Env, c uuid.UUID, kind string, id *uuid.UUID, p any) (changes.ApplyResult, error) {
	var seq int
	e.Pool.QueryRow(context.Background(), `SELECT seq FROM changesets WHERE id=$1`, c).Scan(&seq)
	op := map[string]any{"type": kind, "payload": p}
	if id != nil {
		op["target"] = id
	}
	var r changes.ApplyResult
	err := e.Do(e.Admin, "apply-operations", map[string]any{"changesetId": c, "expectedSeq": seq, "operations": []any{op}}, &r)
	return r, err
}
func submit(e *cmstest.Env, c uuid.UUID) workflow.Review {
	var seq int
	e.Pool.QueryRow(context.Background(), `SELECT seq FROM changesets WHERE id=$1`, c).Scan(&seq)
	var r workflow.Review
	e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": c, "expectedSeq": seq, "targets": []string{"staging"}}, &r)
	return r
}
func publish(e *cmstest.Env, c uuid.UUID) publishing.Publication {
	r := submit(e, c)
	if r.Changeset.State != "approved" {
		e.T.Fatalf("submit %+v", r)
	}
	var p publishing.Publication
	e.Must(e.Admin, "publish", map[string]any{"changesetId": c, "environment": "staging"}, &p)
	return p
}
func read(e *cmstest.Env, id uuid.UUID, c *uuid.UUID) changes.Content {
	v, err := changes.GetContent(context.Background(), e.Q, e.Admin.ProjectID, id, "staging", c)
	if err != nil {
		e.T.Fatal(err)
	}
	return v
}
func ready(t *testing.T, e *cmstest.Env, s *Service, m *memoryStorage, raw []byte) Upload {
	u := begin(t, e, s, m, raw, "image/png")
	e.Must(e.Admin, "complete-asset-upload", Ref{u.AssetID}, nil)
	if err := s.Process(context.Background(), ProcessArgs{u.AssetID, e.Admin.ProjectID}); err != nil {
		t.Fatal(err)
	}
	u, err := s.Get(context.Background(), e.Admin, u.AssetID)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// CNT-040/043/CHG-010/035: worker cannot publish, file replacement versions undo/rebase.
func TestAssetFileVersions(t *testing.T) {
	e := cmstest.New(t, workflow.Register, publishing.Register)
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 0, "high": 0}, nil)
	m := &memoryStorage{data: map[string][]byte{}}
	s := &Service{Pool: e.Pool, Storage: m}
	s.Register(e.Bus)
	ctx := context.Background()
	u := ready(t, e, s, m, pngBytes(t, 10, 20))
	initial := cs(e)
	for _, p := range []any{map[string]any{}, map[string]any{"uploadId": uuid.New()}, map[string]any{"uploadId": u.AssetID, "extra": true}} {
		if _, err := operation(e, initial, "asset.create", nil, p); err == nil {
			t.Fatal("bad creation")
		}
	}
	r, err := operation(e, initial, "asset.create", nil, map[string]any{"uploadId": u.AssetID})
	if err != nil {
		t.Fatal(err)
	}
	id := r.Operations[0].Target
	if _, err := changes.GetContent(ctx, e.Q, e.Admin.ProjectID, id, "staging", nil); err == nil {
		t.Fatal("draft exposed")
	}
	if !strings.Contains(string(read(e, id, &initial).Data), *u.FileHash) {
		t.Fatal("file")
	}
	if _, err := operation(e, initial, "asset.create", nil, map[string]any{"uploadId": u.AssetID}); err == nil {
		t.Fatal("duplicate asset")
	}
	if _, err := operation(e, initial, "asset.updateMeta", &id, map[string]any{"set": map[string]any{"title": "keep"}}); err != nil {
		t.Fatal(err)
	}
	publish(e, initial)
	original := read(e, id, nil)
	replacement := ready(t, e, s, m, pngBytes(t, 20, 10))
	edit := cs(e)
	for _, p := range []any{map[string]any{}, map[string]any{"set": map[string]any{"fileHash": "xx"}}, map[string]any{"set": map[string]any{"fileHash": strings.Repeat("0", 64)}}, map[string]any{"set": map[string]any{"fileHash": *replacement.FileHash, "title": "forge"}}, map[string]any{"set": map[string]any{"fileHash": *replacement.FileHash}, "unset": []string{"title"}}} {
		if _, err := operation(e, edit, "asset.replaceFile", &id, p); err == nil {
			t.Fatal("bad replacement")
		}
	}
	r, err = operation(e, edit, "asset.replaceFile", &id, map[string]any{"set": map[string]any{"fileHash": *replacement.FileHash}})
	if err != nil {
		t.Fatal(err)
	}
	data := read(e, id, &edit)
	if !strings.Contains(string(data.Data), "keep") || !strings.Contains(string(data.Data), *replacement.FileHash) {
		t.Fatal(string(data.Data))
	}
	e.Must(e.Admin, "undo", map[string]any{"changesetId": edit, "expectedSeq": r.Changeset.Seq}, nil)
	if string(read(e, id, &edit).Data) != string(original.Data) {
		t.Fatal("undo changed metadata")
	}
	_, err = operation(e, edit, "asset.replaceFile", &id, map[string]any{"set": map[string]any{"fileHash": *replacement.FileHash}})
	if err != nil {
		t.Fatal(err)
	}
	concurrent := cs(e)
	if _, err := operation(e, concurrent, "asset.updateMeta", &id, map[string]any{"set": map[string]any{"title": "concurrent"}}); err != nil {
		t.Fatal(err)
	}
	publish(e, concurrent)
	var seq int
	if err := e.Pool.QueryRow(ctx, `SELECT seq FROM changesets WHERE id=$1`, edit).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	e.Must(e.Admin, "rebase-changeset", map[string]any{"changesetId": edit, "expectedSeq": seq}, nil)
	if !strings.Contains(string(read(e, id, &edit).Data), "concurrent") {
		t.Fatal("rebase lost metadata")
	}
	publish(e, edit)
	if !strings.Contains(string(read(e, id, nil).Data), *replacement.FileHash) {
		t.Fatal("replacement missing")
	}
}

// PUB-020: readiness is rechecked after approval; rejection leaves every publication pointer unchanged.
func TestAssetReadinessPublicationFailure(t *testing.T) {
	e := cmstest.New(t, workflow.Register, publishing.Register)
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 0, "high": 0}, nil)
	m := &memoryStorage{data: map[string][]byte{}}
	s := &Service{Pool: e.Pool, Storage: m}
	s.Register(e.Bus)
	u := ready(t, e, s, m, pngBytes(t, 2, 3))
	c := cs(e)
	r, err := operation(e, c, "asset.create", nil, map[string]any{"uploadId": u.AssetID})
	if err != nil {
		t.Fatal(err)
	}
	id := r.Operations[0].Target
	review := submit(e, c)
	if review.Changeset.State != "approved" {
		t.Fatal(review)
	}
	e.Exec(`UPDATE asset_files SET status='rejected' WHERE project_id=$1`, e.Admin.ProjectID)
	snapshot := func() string {
		var raw []byte
		err := e.Pool.QueryRow(context.Background(), `SELECT jsonb_build_object('objects',(SELECT jsonb_agg(o ORDER BY id) FROM objects o),'versions',(SELECT jsonb_agg(v ORDER BY id) FROM object_versions v),'cs',(SELECT jsonb_agg(c ORDER BY id) FROM changesets c),'pointers',(SELECT jsonb_agg(p ORDER BY object_id) FROM published_pointers p),'pubs',(SELECT jsonb_agg(p ORDER BY id) FROM publications p),'jobs',(SELECT jsonb_agg(j ORDER BY id) FROM river_job j),'checks',(SELECT jsonb_agg(c ORDER BY id) FROM checks c))`).Scan(&raw)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	before := snapshot()
	if err := e.Do(e.Admin, "publish", map[string]any{"changesetId": c, "environment": "staging"}, nil); err == nil {
		t.Fatal("not ready published")
	}
	if snapshot() != before {
		t.Fatal("failure mutated state")
	}
	if _, err := changes.GetContent(context.Background(), e.Q, e.Admin.ProjectID, id, "staging", nil); err == nil {
		t.Fatal("draft delivered")
	}
	// Invalid file identity remains a blocking draft diagnostic, rather than a panic.
	e.Must(e.Admin, "reopen-changeset", map[string]any{"changesetId": c}, nil)
	e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{fileHash}','"broken"') WHERE changeset_id=$1`, c)
	if submit(e, c).Changeset.State != "failed" {
		t.Fatal("hash not blocked")
	}
	var data map[string]any
	if err := json.Unmarshal(read(e, id, &c).Data, &data); err != nil {
		t.Fatal(err)
	}
}
