package publishing_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"strings"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

func historyCard(t *testing.T, e *cmstest.Env, p uuid.UUID, preview bool) publishing.HistoryCard {
	t.Helper()
	c, _, _, err := publishing.LoadPublicationHistory(context.Background(), e.Q, e.Admin, p, preview, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func guardedRequest(p uuid.UUID, snapshot, key string) commandbus.Request {
	raw, _ := json.Marshal(map[string]any{"publicationId": p, "resetHead": false, "expectedRollbackSnapshot": snapshot})
	return commandbus.Request{Name: "rollback", Reason: "Вернуть предыдущую публикацию", IdempotencyKey: key, Payload: raw}
}
func TestHistoryGuardedRollbackAndExactReplay(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cs, doc := e.Draft(e.Admin, "v1")
	approve(e, cs)
	p1, err := publish(e, cs, "staging")
	if err != nil {
		t.Fatal(err)
	}
	edit := e.Edit(e.Admin, doc, "v2")
	approve(e, edit)
	p2, err := publish(e, edit, "staging")
	if err != nil {
		t.Fatal(err)
	}
	draft := e.Edit(e.Admin, doc, "draft")
	historical := historyCard(t, e, p2.ID, false)
	preview := historyCard(t, e, p2.ID, true)
	if !preview.CanRollback || len(preview.Objects) != 1 || historical.Objects[0].Summary.ChangeCount == 0 {
		t.Fatal(preview, historical)
	}
	page, err := publishing.ReadHistoryPage(ctx, e.Q, e.Admin, p2.ID, doc, true, preview.Snapshot, "", 1)
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	if page.Items[0].Before.Value != "v2" || page.Items[0].After.Value != "v1" {
		t.Fatal(page)
	}
	req := guardedRequest(p2.ID, preview.Snapshot, "retry-rollback")
	var versionsBefore, changesetsBefore int
	if err := e.Pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM object_versions),(SELECT count(*) FROM changesets)").Scan(&versionsBefore, &changesetsBefore); err != nil {
		t.Fatal(err)
	}
	first, err := e.Bus.Dispatch(ctx, e.Admin, req)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := e.Bus.Dispatch(ctx, e.Admin, req)
	if err != nil || !replay.Replayed || string(replay.Body) != string(first.Body) {
		t.Fatal(replay, err)
	}
	var versionsAfter, changesetsAfter int
	if err := e.Pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM object_versions),(SELECT count(*) FROM changesets)").Scan(&versionsAfter, &changesetsAfter); err != nil {
		t.Fatal(err)
	}
	if versionsBefore != versionsAfter || changesetsBefore != changesetsAfter {
		t.Fatal("rollback created versions or changesets")
	}
	if _, err = e.Bus.Dispatch(ctx, e.Admin, guardedRequest(p2.ID, preview.Snapshot, "stale")); cmstest.Code(err) != "ROLLBACK_SNAPSHOT_STALE" {
		t.Fatal(err)
	}
	d, err := published(e, doc, "staging")
	if err != nil || rootName(t, d) != "v1" {
		t.Fatal(d, err)
	}
	head, err := changes.GetDocument(ctx, e.Q, e.Admin.ProjectID, doc, nil)
	if err != nil || rootName(t, head) != "v2" {
		t.Fatal(head, err)
	}
	working, err := changes.GetDocument(ctx, e.Q, e.Admin.ProjectID, doc, &draft)
	if err != nil || rootName(t, working) != "draft" {
		t.Fatal(working, err)
	}
	after := historyCard(t, e, p2.ID, false)
	if after.Snapshot != historical.Snapshot {
		t.Fatal("historical snapshot changed")
	}
	list, err := publishing.ReadHistory(ctx, e.Q, e.Admin, "staging", "", 50)
	if err != nil || len(list.Items) != 3 || list.Items[0].Kind != "rollback" {
		t.Fatal(list, err)
	}
	c := historyCard(t, e, p1.ID, true)
	if !c.CanRollback {
		t.Fatal(c)
	}
	if _, err = e.Bus.Dispatch(ctx, e.Admin, guardedRequest(p1.ID, c.Snapshot, "remove-first")); err != nil {
		t.Fatal(err)
	}
	if _, err = published(e, doc, "staging"); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal(err)
	}
}

func TestHistoryPaginationContextAndPermissions(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cs, _ := e.Draft(e.Admin, "history")
	approve(e, cs)
	p, err := publish(e, cs, "staging")
	if err != nil {
		t.Fatal(err)
	}
	e.Exec("INSERT INTO publications(id,project_id,environment_id,kind,actor_id,created_at) SELECT gen_random_uuid(),project_id,environment_id,'publish',actor_id,created_at FROM publications CROSS JOIN generate_series(1,120) WHERE id=$1", p.ID)
	seen := map[uuid.UUID]bool{}
	cursor := ""
	for {
		list, err := publishing.ReadHistory(ctx, e.Q, e.Admin, "staging", cursor, 17)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range list.Items {
			if seen[p.ID] {
				t.Fatal("duplicate")
			}
			seen[p.ID] = true
		}
		if list.NextCursor == nil {
			break
		}
		cursor = *list.NextCursor
	}
	if len(seen) != 121 {
		t.Fatal(len(seen))
	}
	list, err := publishing.ReadHistory(ctx, e.Q, e.Admin, "staging", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"x", strings.Repeat("x", 2049), *list.NextCursor + "x"} {
		if _, err := publishing.ReadHistory(ctx, e.Q, e.Admin, "staging", bad, 1); cmstest.Code(err) != "INVALID_ARGUMENT" {
			t.Fatal(err)
		}
	}
	if _, err := publishing.ReadHistory(ctx, e.Q, e.Admin, "production", *list.NextCursor, 1); cmstest.Code(err) != "INVALID_ARGUMENT" {
		t.Fatal(err)
	}
	if _, err := publishing.ReadHistory(ctx, e.Q, e.Admin, "staging", *list.NextCursor, 2); cmstest.Code(err) != "INVALID_ARGUMENT" {
		t.Fatal(err)
	}
	actor := e.Admin
	actor = e.Human("Other reader", auth.AllRights...)
	if _, err := publishing.ReadHistory(ctx, e.Q, actor, "staging", *list.NextCursor, 1); cmstest.Code(err) != "INVALID_ARGUMENT" {
		t.Fatal(err)
	}
	for _, limit := range []int{0, 101} {
		if _, err := publishing.ReadHistory(ctx, e.Q, e.Admin, "staging", "", limit); cmstest.Code(err) != "INVALID_ARGUMENT" {
			t.Fatal(err)
		}
	}
	if _, err := publishing.ReadHistory(ctx, e.Q, e.Admin, "missing", "", 1); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal(err)
	}
	actor = e.Admin
	actor.Rights = auth.RightSet{auth.ContentRead: true}
	if _, _, _, err := publishing.LoadPublicationHistory(ctx, e.Q, actor, p.ID, false, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	actor.Rights = auth.RightSet{}
	if _, err := publishing.ReadHistory(ctx, e.Q, actor, "staging", "", 1); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	actor = e.Admin
	actor.ProjectID = uuid.New()
	if _, _, _, err := publishing.LoadPublicationHistory(ctx, e.Q, actor, p.ID, false, nil); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal(err)
	}
}

func TestHistoryPreviewStalePolicyRoutesAndUnsupported(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cs, doc := page(e, "/old")
	p1, err := publish(e, cs, "staging")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := publish(e, reroute(e, doc, "/new"), "staging")
	if err != nil {
		t.Fatal(err)
	}
	c := historyCard(t, e, p2.ID, true)
	if !c.CanRollback || *c.Objects[0].BeforePath != "/new" || *c.Objects[0].AfterPath != "/old" {
		t.Fatal(c)
	}
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 1, "medium": 1, "high": 1}, nil)
	if _, err := e.Bus.Dispatch(ctx, e.Admin, guardedRequest(p2.ID, c.Snapshot, "policy-stale")); cmstest.Code(err) != "ROLLBACK_SNAPSHOT_STALE" {
		t.Fatal(err)
	}
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 0, "high": 0}, nil)
	c = historyCard(t, e, p2.ID, true)
	// Another page now occupies the restored route. The entire preview is blocked.
	cs, _ = page(e, "/old")
	if _, err := publish(e, cs, "staging"); err != nil {
		t.Fatal(err)
	}
	blocked := historyCard(t, e, p2.ID, true)
	if blocked.CanRollback || !strings.Contains(strings.Join(blocked.Blockers, ","), "ROUTE_CONFLICT") {
		t.Fatal(blocked)
	}
	if _, err := e.Bus.Dispatch(ctx, e.Admin, guardedRequest(p2.ID, blocked.Snapshot, "route-conflict")); cmstest.Code(err) != "VALIDATION_FAILED" {
		t.Fatal(err)
	}
	super := historyCard(t, e, p1.ID, true)
	if super.CanRollback {
		t.Fatal(super)
	}
	if _, err := e.Bus.Dispatch(ctx, e.Admin, guardedRequest(p1.ID, super.Snapshot, "superseded")); cmstest.Code(err) != "ROLLBACK_SUPERSEDED" {
		t.Fatal(err)
	}
	actor := e.Admin
	actor.Rights = auth.RightSet{auth.DesignRead: true}
	v, _, _, err := publishing.LoadPublicationHistory(ctx, e.Q, actor, p2.ID, true, nil)
	if err != nil || v.CanRollback {
		t.Fatal(v, err)
	}
	e.Exec("UPDATE publications SET manifest_changed=true,current_manifest_id=(SELECT active_manifest_id FROM environments WHERE environments.id=publications.environment_id) WHERE id=$1", p2.ID)
	v = historyCard(t, e, p2.ID, true)
	if v.CanRollback || len(v.Objects) != 0 {
		t.Fatal(v)
	}
	if _, err := e.Bus.Dispatch(ctx, e.Admin, guardedRequest(p2.ID, v.Snapshot, "manifest")); cmstest.Code(err) != "ROLLBACK_UI_UNSUPPORTED" {
		t.Fatal(err)
	}
	unknown := uuid.New()
	if _, _, _, err := publishing.LoadPublicationHistory(ctx, e.Q, e.Admin, p2.ID, false, &unknown); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal(err)
	}
}

func TestHistoryDetailsCursorsAndGuards(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cs, doc := e.Draft(e.Admin, "name")
	approve(e, cs)
	p, err := publish(e, cs, "staging")
	if err != nil {
		t.Fatal(err)
	}
	c := historyCard(t, e, p.ID, false)
	page, err := publishing.ReadHistoryPage(ctx, e.Q, e.Admin, p.ID, doc, false, "", "", 1)
	if err != nil || page.NextCursor == nil {
		t.Fatal(page, err)
	}
	for cursor := page.NextCursor; cursor != nil; cursor = page.NextCursor {
		page, err = publishing.ReadHistoryPage(ctx, e.Q, e.Admin, p.ID, doc, false, "", *cursor, 1)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := publishing.ReadHistoryPage(ctx, e.Q, e.Admin, p.ID, doc, true, c.Snapshot, "", 1); cmstest.Code(err) != "ROLLBACK_SNAPSHOT_STALE" {
		t.Fatal(err)
	}
	for _, bad := range []string{"x", "eA.eA"} {
		if _, err := publishing.ReadHistoryPage(ctx, e.Q, e.Admin, p.ID, doc, false, "", bad, 1); cmstest.Code(err) != "INVALID_ARGUMENT" {
			t.Fatal(err)
		}
	}
	for _, limit := range []int{0, 101} {
		if _, err := publishing.ReadHistoryPage(ctx, e.Q, e.Admin, p.ID, doc, false, "", "", limit); cmstest.Code(err) != "INVALID_ARGUMENT" {
			t.Fatal(err)
		}
	}
	c = historyCard(t, e, p.ID, true)
	req := guardedRequest(p.ID, c.Snapshot, "no-reason")
	req.Reason = ""
	if _, err := e.Bus.Dispatch(ctx, e.Admin, req); cmstest.Code(err) != "INVALID_ARGUMENT" {
		t.Fatal(err)
	}
	req = guardedRequest(p.ID, c.Snapshot, "big-reason")
	req.Reason = strings.Repeat("r", 2001)
	if _, err := e.Bus.Dispatch(ctx, e.Admin, req); cmstest.Code(err) != "INVALID_ARGUMENT" {
		t.Fatal(err)
	}
	req = guardedRequest(p.ID, c.Snapshot, "head")
	req.Payload = json.RawMessage(`{"publicationId":"` + p.ID.String() + `","resetHead":true,"expectedRollbackSnapshot":"` + c.Snapshot + `"}`)
	if _, err := e.Bus.Dispatch(ctx, e.Admin, req); cmstest.Code(err) != "INVALID_ARGUMENT" {
		t.Fatal(err)
	}

}

// STU-167/169: storage failures cannot become an empty history or safe preview.
func TestHistoryStorageFailures(t *testing.T) {
	for _, table := range []string{"publications", "publication_items", "objects", "environments", "published_pointers", "projects", "routes", "object_versions", "roles", "role_bindings"} {
		t.Run(table, func(t *testing.T) {
			e := setup(t)
			ctx := context.Background()
			cs, doc := e.Draft(e.Admin, "v1")
			approve(e, cs)
			p, err := publish(e, cs, "staging")
			if err != nil {
				t.Fatal(err)
			}
			e.Break(table, "")
			if _, _, _, err := publishing.LoadPublicationHistory(ctx, e.Q, e.Admin, p.ID, true, nil); err == nil {
				t.Fatal("unsafe preview")
			}
			if _, err := publishing.ReadHistoryPage(ctx, e.Q, e.Admin, p.ID, doc, true, "bad", "", 1); err == nil {
				t.Fatal("unsafe detail")
			}
		})
	}
}

// STU-168: a pointer restored to the same version still has a new publication provenance.
func TestHistorySnapshotRejectsPointerABA(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cs, doc := e.Draft(e.Admin, "v1")
	approve(e, cs)
	p1, err := publish(e, cs, "staging")
	if err != nil {
		t.Fatal(err)
	}
	old := historyCard(t, e, p1.ID, true)
	edit := e.Edit(e.Admin, doc, "v2")
	approve(e, edit)
	p2, err := publish(e, edit, "staging")
	if err != nil {
		t.Fatal(err)
	}
	fresh := historyCard(t, e, p2.ID, true)
	if _, err := e.Bus.Dispatch(ctx, e.Admin, guardedRequest(p2.ID, fresh.Snapshot, "back")); err != nil {
		t.Fatal(err)
	}
	restored := historyCard(t, e, p1.ID, true)
	if !restored.CanRollback || restored.Snapshot == old.Snapshot {
		t.Fatal(restored)
	}
	if _, err := e.Bus.Dispatch(ctx, e.Admin, guardedRequest(p1.ID, old.Snapshot, "aba")); cmstest.Code(err) != "ROLLBACK_SNAPSHOT_STALE" {
		t.Fatal(err)
	}
}

// STU-173: bounded APIs reject incomplete/oversized comparisons explicitly.
// STU-167/168: cached request rights cannot bypass a revoked role.
func TestHistoryRevokedRights(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cs, _ := e.Draft(e.Admin, "v1")
	approve(e, cs)
	p, err := publish(e, cs, "staging")
	if err != nil {
		t.Fatal(err)
	}
	human := e.Human("Reviewer", auth.DesignRead, auth.ContentRead, auth.ContentPublish)
	card, _, _, err := publishing.LoadPublicationHistory(ctx, e.Q, human, p.ID, true, nil)
	if err != nil || !card.CanRollback {
		t.Fatal(card, err)
	}
	e.Exec("UPDATE roles SET capabilities=ARRAY['design.read','content.read'] WHERE id IN (SELECT role_id FROM role_bindings WHERE actor_id=$1)", human.ID)
	if _, err := e.Bus.Dispatch(ctx, human, guardedRequest(p.ID, card.Snapshot, "revoked")); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	e.Exec("DELETE FROM role_bindings WHERE actor_id=$1", human.ID)
	if _, _, _, err := publishing.LoadPublicationHistory(ctx, e.Q, human, p.ID, false, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
}
func TestHistoryObjectAndResponseLimits(t *testing.T) {
	t.Run("object count", func(t *testing.T) {
		e := setup(t)
		cs, _ := e.Draft(e.Admin, "v1")
		approve(e, cs)
		p, err := publish(e, cs, "staging")
		if err != nil {
			t.Fatal(err)
		}
		e.Exec("INSERT INTO objects(id,project_id,kind) SELECT gen_random_uuid(),$1,'asset' FROM generate_series(1,1001)", e.Admin.ProjectID)
		e.Exec("INSERT INTO publication_items(publication_id,object_id) SELECT $1,id FROM objects WHERE project_id=$2 AND kind='asset'", p.ID, e.Admin.ProjectID)
		if _, _, _, err := publishing.LoadPublicationHistory(context.Background(), e.Q, e.Admin, p.ID, false, nil); cmstest.Code(err) != "PUBLICATION_HISTORY_LIMIT" {
			t.Fatal(err)
		}
	})
	t.Run("single change", func(t *testing.T) {
		e := setup(t)
		cs, _ := e.Draft(e.Admin, "v1")
		approve(e, cs)
		p, err := publish(e, cs, "staging")
		if err != nil {
			t.Fatal(err)
		}
		e.Exec("UPDATE object_versions SET body=jsonb_set(body,'{nodes,n_root,name}',to_jsonb(repeat('x',5*1024*1024))) WHERE id=$1", *p.Items[0].CurrentVersionID)
		if _, _, _, err := publishing.LoadPublicationHistory(context.Background(), e.Q, e.Admin, p.ID, false, nil); cmstest.Code(err) != "PUBLICATION_HISTORY_LIMIT" {
			t.Fatal(err)
		}
	})
}

// STU-164/165/167/169: the full mixed publication is authorized and rolled back together.
func TestHistoryMixedKindsAndAssetRedaction(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cs, doc := e.Draft(e.Admin, "Mixed")
	approve(e, cs)
	p, err := publish(e, cs, "staging")
	if err != nil {
		t.Fatal(err)
	}
	entity := e.ContentFixture("entity", "Article", map[string]any{"title": map[string]any{"ru": "Статья"}}, "staging")
	asset := e.ContentFixture("asset", "", map[string]any{"title": "Image", "fileHash": strings.Repeat("a", 64), "storageKey": "private-storage-secret", "uploadUrl": "https://private.invalid/signed-secret"}, "staging")
	for _, id := range []uuid.UUID{entity, asset} {
		e.Exec("INSERT INTO publication_items(publication_id,object_id,current_version_id) SELECT $1,id,head_version_id FROM objects WHERE id=$2", p.ID, id)
	}
	c := historyCard(t, e, p.ID, true)
	if !c.CanRollback || len(c.Objects) != 3 {
		t.Fatal(c)
	}
	page, err := publishing.ReadHistoryPage(ctx, e.Q, e.Admin, p.ID, asset, false, "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), "private-storage-secret") || strings.Contains(string(raw), "signed-secret") {
		t.Fatal("asset secret exposed")
	}
	reader := e.Admin
	reader.Rights = auth.RightSet{auth.DesignRead: true}
	if _, _, _, err := publishing.LoadPublicationHistory(ctx, e.Q, reader, p.ID, false, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	if _, err := publishing.ReadHistory(ctx, e.Q, reader, "staging", "", 50); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	if _, err := e.Bus.Dispatch(ctx, e.Admin, guardedRequest(p.ID, c.Snapshot, "mixed")); err != nil {
		t.Fatal(err)
	}
	if _, err := published(e, doc, "staging"); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal(err)
	}
	var count int
	if err := e.Pool.QueryRow(ctx, "SELECT count(*) FROM published_pointers WHERE object_id=ANY($1::uuid[])", []uuid.UUID{doc, entity, asset}).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}
