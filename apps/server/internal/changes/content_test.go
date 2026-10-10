package changes_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
	"time"
)

// CNT-040/043: metadata edits cannot replace the binary identity; only published assets resolve.
func TestContentAssetMetadata(t *testing.T) {
	e := cntSetup(t)
	asset := e.ContentFixture("asset", "", map[string]any{"assetKind": "image", "fileHash": "immutable", "title": "old"}, "staging")
	cs := cntCS(e)
	data := cntArticle("cover")
	data["cover"] = map[string]any{"assetId": asset.String()}
	cntCreate(e, cs, "Article", data)
	cntPublish(e, cs)
	meta := cntCS(e)
	for _, payload := range []any{map[string]any{"set": map[string]any{"fileHash": "replace"}}, map[string]any{"set": map[string]any{"alt": "wrong"}}, map[string]any{"set": map[string]any{"focalPoint": map[string]any{"x": 2, "y": 0}}}} {
		if _, err := cntOp(e, meta, "asset.updateMeta", &asset, payload); err == nil {
			t.Fatal(payload)
		}
	}
	r, err := cntOp(e, meta, "asset.updateMeta", &asset, map[string]any{"set": map[string]any{"title": "new", "alt.ru": "Фото", "alt.en": "Photo", "tags": []any{"hero"}, "focalPoint": map[string]any{"x": 0.5, "y": 1}}})
	if err != nil {
		t.Fatal(err)
	}
	e.Must(e.Admin, "undo", map[string]any{"changesetId": meta, "expectedSeq": cntSeq(e, meta)}, nil)
	if !strings.Contains(string(cntRead(e, asset, "staging", &meta).Data), `"title": "old"`) && !strings.Contains(string(cntRead(e, asset, "staging", &meta).Data), `"title":"old"`) {
		t.Fatal("metadata undo failed", r)
	}
	if _, err := cntOp(e, meta, "asset.updateMeta", &asset, map[string]any{"set": map[string]any{"title": "new"}}); err != nil {
		t.Fatal(err)
	}
	cntPublish(e, meta)
	if !strings.Contains(string(cntRead(e, asset, "staging", nil).Data), "immutable") {
		t.Fatal("file changed")
	}
	wrong := e.ContentFixture("asset", "", map[string]any{"assetKind": "file", "fileHash": "pdf"}, "staging")
	invalid := cntCS(e)
	data = cntArticle("wrong-cover")
	data["cover"] = map[string]any{"assetId": wrong.String()}
	if _, err := cntOp(e, invalid, "entity.create", nil, map[string]any{"schema": "Article", "environment": "staging", "data": data}); err == nil {
		t.Fatal("wrong asset kind accepted")
	}
	editor := e.Human("asset-editor", auth.AssetWrite)
	var own changes.Changeset
	e.Must(editor, "create-changeset", map[string]any{"title": "asset"}, &own)
	if cmstest.Code(e.Do(editor, "apply-operations", map[string]any{"changesetId": own.ID, "expectedSeq": 0, "operations": []any{map[string]any{"type": "entity.delete", "target": asset, "payload": map[string]any{}}}}, nil)) != "FORBIDDEN" {
		t.Fatal("delete rights bypassed")
	}
	e.Must(editor, "apply-operations", map[string]any{"changesetId": own.ID, "expectedSeq": 0, "operations": []any{map[string]any{"type": "asset.updateMeta", "target": asset, "payload": map[string]any{"set": map[string]any{"title": "editor"}}}}}, nil)
}

// CHG-040: disjoint field edits replay; overlapping paths need an explicit resolution.
func TestContentRebase(t *testing.T) {
	e := cntSetup(t)
	initial := cntCS(e)
	id := cntCreate(e, initial, "Article", cntArticle("initial"))
	cntPublish(e, initial)
	left, right := cntCS(e), cntCS(e)
	if _, err := cntOp(e, left, "entity.setFields", &id, map[string]any{"set": map[string]any{"slug": "left"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := cntOp(e, right, "entity.setFields", &id, map[string]any{"set": map[string]any{"title.ru": "right"}}); err != nil {
		t.Fatal(err)
	}
	cntPublish(e, right)
	var rebased workflow.RebaseOutcome
	e.Must(e.Admin, "rebase-changeset", map[string]any{"changesetId": left, "expectedSeq": cntSeq(e, left)}, &rebased)
	value := cntRead(e, id, "staging", &left)
	if !strings.Contains(string(value.Data), "left") || !strings.Contains(string(value.Data), "right") {
		t.Fatal(value, rebased)
	}
	cntPublish(e, left)
	mine, theirs := cntCS(e), cntCS(e)
	op, err := cntOp(e, mine, "entity.setFields", &id, map[string]any{"set": map[string]any{"slug": "mine"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cntOp(e, theirs, "entity.setFields", &id, map[string]any{"set": map[string]any{"slug": "theirs"}}); err != nil {
		t.Fatal(err)
	}
	cntPublish(e, theirs)
	e.Must(e.Admin, "rebase-changeset", map[string]any{"changesetId": mine, "expectedSeq": cntSeq(e, mine)}, &rebased)
	detail, err := changes.GetChangeset(context.Background(), e.Q, e.Admin.ProjectID, mine)
	if err != nil || !detail.HasConflicts {
		t.Fatal(detail, err)
	}
	e.Must(e.Admin, "rebase-changeset", map[string]any{"changesetId": mine, "expectedSeq": cntSeq(e, mine), "resolutions": []any{map[string]any{"operationId": op.Operations[0].ID, "choice": "mine"}}}, &rebased)
	cntPublish(e, mine)
	if !strings.Contains(string(cntRead(e, id, "staging", nil).Data), "mine") {
		t.Fatal("resolution lost")
	}
}

// CHG-031/FR-030: storage faults never leave partial working versions or operations.
func TestContentStorageAtomicity(t *testing.T) {
	for _, fault := range []struct{ table, op string }{{"objects", "INSERT"}, {"object_versions", "INSERT"}, {"changeset_objects", "INSERT"}, {"changesets", "UPDATE"}, {"operations", "INSERT"}, {"object_versions", "UPDATE"}} {
		t.Run(fault.table+fault.op, func(t *testing.T) {
			e := cntSetup(t)
			cs := cntCS(e)
			e.Break(fault.table, fault.op)
			_, err := cntOp(e, cs, "entity.create", nil, map[string]any{"schema": "Author", "environment": "staging", "data": map[string]any{"name": "fault"}})
			cmstest.ExpectDBError(t, err)
			if cntSeq(e, cs) != 0 {
				t.Fatal("seq changed")
			}
			for _, table := range []string{"objects", "object_versions", "operations", "changeset_objects", "published_pointers", "river_job"} {
				var n int
				if err := e.Pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
					t.Fatal(err)
				}
				if n != 0 {
					t.Fatal(table, n)
				}
			}
		})
	}
}

// CNT-011/PUB-020: final recheck catches a target deleted after approval and leaves every publication effect unchanged.
func TestContentPublicationRecheckIsAtomic(t *testing.T) {
	e := cntSetup(t)
	initial := cntCS(e)
	author := cntCreate(e, initial, "Author", map[string]any{"name": "author"})
	cntPublish(e, initial)
	cs := cntCS(e)
	data := cntArticle("late")
	data["author"] = map[string]any{"entityId": author.String()}
	article := cntCreate(e, cs, "Article", data)
	if rv := cntSubmit(e, cs); rv.Changeset.State != "approved" {
		t.Fatal(rv)
	}
	deletion := cntCS(e)
	if _, err := cntOp(e, deletion, "entity.delete", &author, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	deleted := cntPublish(e, deletion)
	snapshot := func() string {
		var raw string
		err := e.Pool.QueryRow(context.Background(), `SELECT jsonb_build_object(
 'heads',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM objects o),
 'versions',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM object_versions v),
 'pointers',(SELECT jsonb_agg(to_jsonb(p) ORDER BY object_id) FROM published_pointers p),
 'publications',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM publications p),
 'cs',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM changesets c),
 'checks',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM checks c),
 'outbox',(SELECT jsonb_agg(to_jsonb(j) ORDER BY id) FROM river_job j))::text`).Scan(&raw)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot()
	err := e.Do(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil)
	if cmstest.Code(err) != "VALIDATION_FAILED" || snapshot() != before {
		t.Fatal("partial failed publication", err)
	}
	if _, err := changes.GetContent(context.Background(), e.Q, e.Admin.ProjectID, article, "staging", nil); err == nil {
		t.Fatal("failed content exposed")
	}
	e.Must(e.Admin, "rollback", map[string]any{"publicationId": deleted.ID}, nil)
	e.Must(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil)
	cntRead(e, article, "staging", nil)
}

// CNT-011: circular entity references are permitted when both targets publish atomically.
func TestContentCyclicReferencesAndUniqueReplacement(t *testing.T) {
	e := cntSetup(t)
	raw := strings.TrimSuffix(cmstest.DefaultManifest, "}") + `,"schemas":{"Author":{"version":1,"fields":{"name":{"type":"text","unique":true},"peer":{"type":"reference","schema":"Author"}}}}}`
	e.ActivateManifest("staging", []byte(raw))
	e.ActivateManifest("production", []byte(raw))
	cs := cntCS(e)
	a := cntCreate(e, cs, "Author", map[string]any{"name": "A"})
	b := cntCreate(e, cs, "Author", map[string]any{"name": "B", "peer": map[string]any{"entityId": a.String()}})
	if _, err := cntOp(e, cs, "entity.setFields", &a, map[string]any{"set": map[string]any{"peer": map[string]any{"entityId": b.String()}}}); err != nil {
		t.Fatal(err)
	}
	cntPublish(e, cs)
	swap := cntCS(e)
	for _, v := range []struct {
		id   uuid.UUID
		name string
	}{{a, "B"}, {b, "A"}} {
		if _, err := cntOp(e, swap, "entity.setFields", &v.id, map[string]any{"set": map[string]any{"name": v.name}}); err != nil {
			t.Fatal(err)
		}
	}
	cntPublish(e, swap)
	deletion := cntCS(e)
	if _, err := cntOp(e, deletion, "entity.delete", &a, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if _, err := cntOp(e, deletion, "entity.setFields", &a, map[string]any{"set": map[string]any{"name": "dead"}}); err == nil {
		t.Fatal("deleted edited")
	}
	if _, err := cntOp(e, deletion, "entity.restore", &a, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	cntPublish(e, deletion)
}

// CNT-003/PUB-031: promotion preserves head; old publication values must not be substituted into head uniqueness checks.
func TestContentPromotionKeepsCurrentHeadConstraints(t *testing.T) {
	e := cntSetup(t)
	first := cntCS(e)
	a := cntCreate(e, first, "Article", cntArticle("A"))
	p := cntPublish(e, first)
	second := cntCS(e)
	b := cntCreate(e, second, "Article", cntArticle("B"))
	cntPublish(e, second)
	swap := cntCS(e)
	for _, v := range []struct {
		id   uuid.UUID
		slug string
	}{{a, "B"}, {b, "A"}} {
		if _, err := cntOp(e, swap, "entity.setFields", &v.id, map[string]any{"set": map[string]any{"slug": v.slug}}); err != nil {
			t.Fatal(err)
		}
	}
	cntPublish(e, swap)
	e.Must(e.Admin, "promote", map[string]any{"publicationId": p.ID, "toEnvironment": "production"}, nil)
	var data map[string]any
	json.Unmarshal(cntRead(e, a, "production", nil).Data, &data)
	if data["slug"] != "A" {
		t.Fatal(data)
	}
	json.Unmarshal(cntRead(e, a, "", nil).Data, &data)
	if data["slug"] != "B" {
		t.Fatal("promotion moved head", data)
	}
}

const cntSchemas = `"schemas":{"Article":{"version":1,"fields":{"title":{"type":"text","required":true,"localized":true},"slug":{"type":"string","unique":true},"author":{"type":"reference","schema":"Author"},"cover":{"type":"asset","assetKind":"image"},"tags":{"type":"list","of":{"type":"string"}},"nested":{"type":"object","fields":{"label":{"type":"string"}}}}},"Author":{"version":1,"fields":{"name":{"type":"text"}}},"Settings":{"version":1,"singleton":true,"fields":{"title":{"type":"text"}}}}`

func cntSetup(t *testing.T) *cmstest.Env {
	e := cmstest.New(t, workflow.Register, publishing.Register)
	raw := strings.TrimSuffix(cmstest.DefaultManifest, "}") + "," + cntSchemas + "}"
	e.ActivateManifest("staging", []byte(raw))
	e.ActivateManifest("production", []byte(raw))
	e.Exec(`UPDATE projects SET settings='{"locales":["ru","en"],"defaultLocale":"ru"}'`)
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 0, "high": 0}, nil)
	return e
}
func cntCS(e *cmstest.Env) uuid.UUID {
	var cs changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "content"}, &cs)
	return cs.ID
}
func cntSeq(e *cmstest.Env, cs uuid.UUID) int32 {
	c, err := e.Q.GetChangeset(context.Background(), store.GetChangesetParams{ProjectID: e.Admin.ProjectID, ID: cs})
	if err != nil {
		e.T.Fatal(err)
	}
	return c.Seq
}
func cntOp(e *cmstest.Env, cs uuid.UUID, kind string, target *uuid.UUID, payload any) (changes.ApplyResult, error) {
	var r changes.ApplyResult
	err := e.Do(e.Admin, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs), "operations": []any{map[string]any{"type": kind, "target": target, "payload": payload}}}, &r)
	return r, err
}
func cntCreate(e *cmstest.Env, cs uuid.UUID, schema string, data any) uuid.UUID {
	r, err := cntOp(e, cs, "entity.create", nil, map[string]any{"schema": schema, "environment": "staging", "data": data})
	if err != nil {
		e.T.Fatal(err)
	}
	return r.Operations[0].Target
}
func cntSubmit(e *cmstest.Env, cs uuid.UUID) workflow.Review {
	var r workflow.Review
	e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs), "targets": []string{"staging"}}, &r)
	return r
}
func cntPublish(e *cmstest.Env, cs uuid.UUID) publishing.Publication {
	r := cntSubmit(e, cs)
	if r.Changeset.State != "approved" {
		e.T.Fatalf("not approved: %+v", r)
	}
	var pub publishing.Publication
	e.Must(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, &pub)
	return pub
}
func cntArticle(slug string) map[string]any {
	return map[string]any{"title": map[string]any{"ru": "Статья", "en": "Article"}, "slug": slug}
}
func cntRead(e *cmstest.Env, id uuid.UUID, env string, cs *uuid.UUID) changes.Content {
	value, err := changes.GetContent(context.Background(), e.Q, e.Admin.ProjectID, id, env, cs)
	if err != nil {
		e.T.Fatal(err)
	}
	return value
}

// CNT-010/011/013/024: drafts preserve missing required fields; only same-env prospective references publish.
func TestContentDraftAndAtomicPublication(t *testing.T) {
	e := cntSetup(t)
	cs := cntCS(e)
	author := cntCreate(e, cs, "Author", map[string]any{"name": "Автор"})
	article := cntCreate(e, cs, "Article", map[string]any{"slug": "first", "author": map[string]any{"entityId": author.String()}})
	if _, err := changes.GetContent(context.Background(), e.Q, e.Admin.ProjectID, article, "staging", nil); err == nil {
		t.Fatal("draft exposed")
	}
	draft := cntRead(e, article, "staging", &cs)
	if draft.SchemaVersion == nil || *draft.SchemaVersion != 1 {
		t.Fatal(draft)
	}
	if rv := cntSubmit(e, cs); rv.Changeset.State != "failed" {
		t.Fatal(rv)
	}
	e.Must(e.Admin, "reopen-changeset", map[string]any{"changesetId": cs}, nil)
	r, err := cntOp(e, cs, "entity.setFields", &article, map[string]any{"set": map[string]any{"title.ru": "Статья", "title.en": "Article"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) != 0 {
		t.Fatal(r.Warnings)
	}
	p := cntPublish(e, cs)
	if len(p.Items) != 2 {
		t.Fatal(p)
	}
	if !strings.Contains(string(cntRead(e, article, "staging", nil).Data), author.String()) {
		t.Fatal("reference lost")
	}
	var promoted publishing.Publication
	e.Must(e.Admin, "promote", map[string]any{"publicationId": p.ID, "toEnvironment": "production"}, &promoted)
	cntRead(e, article, "production", nil)
}

// CNT-012, PUB-033: deleting referenced objects and rolling away required targets must be atomic failures.
func TestContentDeleteRestoreAndRollback(t *testing.T) {
	e := cntSetup(t)
	initial := cntCS(e)
	author := cntCreate(e, initial, "Author", map[string]any{"name": "Автор"})
	p := cntPublish(e, initial)
	cs := cntCS(e)
	data := cntArticle("linked")
	data["author"] = map[string]any{"entityId": author.String()}
	article := cntCreate(e, cs, "Article", data)
	cntPublish(e, cs)
	if err := e.Do(e.Admin, "rollback", map[string]any{"publicationId": p.ID}, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
		t.Fatal(err)
	}
	cntRead(e, author, "staging", nil)
	deletion := cntCS(e)
	if _, err := cntOp(e, deletion, "entity.delete", &author, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if rv := cntSubmit(e, deletion); rv.Changeset.State != "failed" {
		t.Fatal(rv)
	}
	cntRead(e, author, "staging", nil)
	e.Must(e.Admin, "reopen-changeset", map[string]any{"changesetId": deletion}, nil)
	if _, err := cntOp(e, deletion, "entity.setFields", &article, map[string]any{"unset": []string{"author"}}); err != nil {
		t.Fatal(err)
	}
	removed := cntPublish(e, deletion)
	o, err := e.Q.ContentIdentity(context.Background(), store.ContentIdentityParams{ProjectID: e.Admin.ProjectID, ID: author})
	if err != nil || o.DeletedAt == nil {
		t.Fatal(o, err)
	}
	if _, err := changes.GetContent(context.Background(), e.Q, e.Admin.ProjectID, author, "staging", nil); err == nil {
		t.Fatal("deleted served")
	}
	e.Must(e.Admin, "rollback", map[string]any{"publicationId": removed.ID}, nil)
	cntRead(e, author, "staging", nil)
	restore := cntCS(e)
	if _, err := cntOp(e, restore, "entity.delete", &author, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	e.Must(e.Admin, "undo", map[string]any{"changesetId": restore, "expectedSeq": cntSeq(e, restore)}, nil)
	if cntRead(e, author, "staging", &restore).Deleted {
		t.Fatal("undo didn't restore")
	}
}

// CNT-003/004: uniqueness includes the project head from other environments and the entire candidate batch.
func TestContentUniqueAndSingleton(t *testing.T) {
	for _, schema := range []string{"Article", "Settings"} {
		t.Run(schema, func(t *testing.T) {
			e := cntSetup(t)
			first := cntCS(e)
			data := map[string]any{"title": "Settings"}
			if schema == "Article" {
				data = cntArticle("same")
			}
			cntCreate(e, first, schema, data)
			cntPublish(e, first)
			second := cntCS(e)
			cntCreate(e, second, schema, data)
			if rv := cntSubmit(e, second); rv.Changeset.State != "failed" {
				t.Fatal(rv)
			}
		})
	}
}

// CNT-011: unpublished, foreign, wrong-schema and asset-kind references remain publication errors.
func TestContentInvalidReferences(t *testing.T) {
	for _, scenario := range []string{"missing", "unpublished", "foreign", "schema", "asset"} {
		t.Run(scenario, func(t *testing.T) {
			e := cntSetup(t)
			cs := cntCS(e)
			target := uuid.New()
			if scenario == "unpublished" {
				target = cntCreate(e, cntCS(e), "Author", map[string]any{})
			}
			if scenario == "schema" {
				source := cntCS(e)
				target = cntCreate(e, source, "Settings", map[string]any{})
				cntPublish(e, source)
			}
			if scenario == "foreign" {
				res, err := projects.Bootstrap(context.Background(), e.Pool, "other", "Other", time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				a, err := auth.Authenticate(context.Background(), e.Q, "Bearer "+res.Token)
				if err != nil {
					t.Fatal(err)
				}
				other := cmstest.Env{T: t, Pool: e.Pool, Q: e.Q, Admin: a}
				target = other.ContentFixture("entity", "Author", map[string]any{"name": "foreign"}, "staging")
			}
			data := cntArticle("invalid")
			key := "author"
			ref := "entityId"
			if scenario == "asset" {
				key = "cover"
				ref = "assetId"
			}
			data[key] = map[string]any{ref: target.String()}
			if scenario == "schema" {
				if _, err := cntOp(e, cs, "entity.create", nil, map[string]any{"schema": "Article", "environment": "staging", "data": data}); err == nil {
					t.Fatal("wrong reference schema accepted")
				}
				return
			}
			cntCreate(e, cs, "Article", data)
			if rv := cntSubmit(e, cs); rv.Changeset.State != "failed" {
				t.Fatal(rv)
			}
		})
	}
}

// CNT-013, CHG-031: malformed values and paths roll back the entire command and journal sequence.
func TestContentInvalidEditsAndUndo(t *testing.T) {
	e := cntSetup(t)
	cs := cntCS(e)
	id := cntCreate(e, cs, "Article", cntArticle("valid"))
	for _, payload := range []any{
		map[string]any{"set": map[string]any{"unknown": 1}}, map[string]any{"set": map[string]any{"title": true}},
		map[string]any{"set": map[string]any{"tags": []any{1}}}, map[string]any{"set": map[string]any{"title.xx": "x"}},
		map[string]any{"set": map[string]any{"title": "x", "title.ru": "y"}}, map[string]any{"set": map[string]any{"slug.x": "x"}},
		map[string]any{"unset": []string{"slug", "slug"}}, map[string]any{}, map[string]any{"set": map[string]any{"__proto__.x": 1}},
	} {
		before := cntSeq(e, cs)
		if _, err := cntOp(e, cs, "entity.setFields", &id, payload); err == nil {
			t.Fatal(payload)
		}
		if cntSeq(e, cs) != before {
			t.Fatal("seq mutated")
		}
	}
	before := cntRead(e, id, "staging", &cs)
	if _, err := cntOp(e, cs, "entity.setFields", &id, map[string]any{"set": map[string]any{"title.ru": "Другая", "nested.label": "new"}, "unset": []string{"slug"}}); err != nil {
		t.Fatal(err)
	}
	e.Must(e.Admin, "undo", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs)}, nil)
	after := cntRead(e, id, "staging", &cs)
	var b, a any
	json.Unmarshal(before.Data, &b)
	json.Unmarshal(after.Data, &a)
	// Undo restores the exact body, including missing parent objects.
	if !reflect.DeepEqual(a, b) {
		t.Fatal(a, b)
	}
}

// CNT-024: environment validation cannot silently accept schema versions from the future.
func TestContentVersionAhead(t *testing.T) {
	e := cntSetup(t)
	cs := cntCS(e)
	id := cntCreate(e, cs, "Article", cntArticle("ahead"))
	e.Exec(`UPDATE object_versions SET schema_version=2 WHERE object_id=$1`, id)
	if rv := cntSubmit(e, cs); rv.Changeset.State != "failed" {
		t.Fatal(rv)
	}
	e.Must(e.Admin, "reopen-changeset", map[string]any{"changesetId": cs}, nil)
	if _, err := cntOp(e, cs, "entity.setFields", &id, map[string]any{"set": map[string]any{"slug": "downgrade"}}); cmstest.Code(err) != "SCHEMA_VERSION_AHEAD" {
		t.Fatal(err)
	}
	c, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", &cs)
	if err != nil {
		t.Fatal(err)
	}
	version, err := e.Q.GetChangesetObject(context.Background(), store.GetChangesetObjectParams{ChangesetID: cs, ObjectID: id})
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.Validate(context.Background(), validation.Document{ObjectID: id, VersionID: version.WorkingVersionID, Body: version.WorkingBody})
	if err != nil || r.Valid || string(r.Diagnostics[0].Code) != "SCHEMA_VERSION_AHEAD" {
		t.Fatal(r, err)
	}
}

// CNT-013/CHG-030: malformed creation, wrong kinds and foreign CS reads cannot mutate content.
func TestContentCommandBoundaries(t *testing.T) {
	e := cntSetup(t)
	cs := cntCS(e)
	for _, payload := range []any{map[string]any{}, map[string]any{"schema": "Author", "data": map[string]any{}}, map[string]any{"schema": "Missing", "environment": "staging", "data": map[string]any{}}, map[string]any{"schema": "Author", "environment": "missing", "data": map[string]any{}}, map[string]any{"schema": "Author", "environment": "staging", "data": true}, map[string]any{"schema": "Author", "environment": "staging", "data": map[string]any{"name": false}}, map[string]any{"schema": "Author", "environment": "staging", "data": map[string]any{}, "extra": true}} {
		if _, err := cntOp(e, cs, "entity.create", nil, payload); err == nil {
			t.Fatal(payload)
		}
	}
	id := cntCreate(e, cs, "Author", map[string]any{"name": "author"})
	if _, err := cntOp(e, cs, "entity.create", nil, map[string]any{"schema": "Author", "environment": "production", "data": map[string]any{}}); err == nil {
		t.Fatal("mixed creation contract")
	}
	for _, v := range []struct {
		kind    string
		payload any
	}{{"asset.updateMeta", map[string]any{"set": map[string]any{"title": "asset"}}}, {"node.rename", map[string]any{"nodeId": "n_root", "name": "document"}}, {"entity.delete", map[string]any{"extra": true}}} {
		if _, err := cntOp(e, cs, v.kind, &id, v.payload); err == nil {
			t.Fatal(v)
		}
	}
	_, doc := e.Draft(e.Admin, "doc")
	if _, err := cntOp(e, cs, "entity.delete", &doc, map[string]any{}); err == nil {
		t.Fatal("document deleted as entity")
	}
	absent := uuid.New()
	if _, err := cntOp(e, cs, "entity.setFields", &absent, map[string]any{"set": map[string]any{"name": "missing"}}); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal(err)
	}
	// Explicit targets permit starting an entity edit under production's contract.
	var production changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "production", "targets": []string{"production"}}, &production)
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": production.ID, "expectedSeq": 0, "operations": []any{map[string]any{"type": "entity.create", "payload": map[string]any{"schema": "Author", "environment": "production", "data": map[string]any{"name": "production"}}}}}, nil)
	if err := e.Do(e.Admin, "create-changeset", map[string]any{"title": "missing", "targets": []string{"missing"}}, nil); err == nil {
		t.Fatal("unknown target")
	}
}

// Read paths distinguish storage errors from missing content.
func TestContentReadFailures(t *testing.T) {
	for _, table := range []string{"environments", "object_versions", "changesets", "manifests", "projects"} {
		t.Run(table, func(t *testing.T) {
			e := cntSetup(t)
			cs := cntCS(e)
			id := cntCreate(e, cs, "Author", map[string]any{})
			e.Break(table, "")
			_, err := changes.GetContent(context.Background(), e.Q, e.Admin.ProjectID, id, "staging", &cs)
			cmstest.ExpectDBError(t, err)
		})
	}
	e := cntSetup(t)
	cs := cntCS(e)
	id := cntCreate(e, cs, "Author", map[string]any{})
	e.ActivateManifest("staging", []byte(cmstest.DefaultManifest))
	c, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", &cs)
	if err != nil {
		t.Fatal(err)
	}
	co, err := e.Q.GetChangesetObject(context.Background(), store.GetChangesetObjectParams{ChangesetID: cs, ObjectID: id})
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.Validate(context.Background(), validation.Document{ObjectID: id, VersionID: co.WorkingVersionID, Body: co.WorkingBody})
	if err != nil || r.Valid || string(r.Diagnostics[0].Code) != "CONTENT_SCHEMA_UNKNOWN" {
		t.Fatal(r, err)
	}
	if _, err := c.Validate(context.Background(), validation.Document{ObjectID: id, VersionID: co.WorkingVersionID, Body: []byte(`bad`)}); err == nil {
		t.Fatal("bad body accepted")
	}

	if _, err := changes.GetContent(context.Background(), e.Q, e.Admin.ProjectID, id, "staging", &cs); cmstest.Code(err) != "CONTENT_SCHEMA_UNKNOWN" {
		t.Fatal(err)
	}
}
