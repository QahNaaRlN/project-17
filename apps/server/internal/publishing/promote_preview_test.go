package publishing_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

func promoteCard(t *testing.T, e *cmstest.Env, a auth.Actor, p uuid.UUID, target string) publishing.PromoteCard {
	t.Helper()
	c, _, _, err := publishing.LoadPromotePreview(context.Background(), e.Q, a, p, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func guardedPromote(p uuid.UUID, target, snapshot, key, reason string) commandbus.Request {
	raw, _ := json.Marshal(map[string]any{"publicationId": p, "toEnvironment": target, "expectedPromoteSnapshot": snapshot})
	return commandbus.Request{Name: "promote", Reason: reason, IdempotencyKey: key, Payload: raw}
}
func blocked(c publishing.PromoteCard, code string) bool {
	return !c.CanPromote && strings.Contains(strings.Join(c.Blockers, ","), code)
}

// STU-462, 463, 466, 471: preview, diff, guarded command and exact replay.
func TestPromotePreviewGuardedAndReplay(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	doc, p1, p2 := staged(e)
	c := promoteCard(t, e, e.Admin, p2.ID, "production")
	if !c.CanPromote || c.ToEnvironment != "production" || len(c.Objects) != 1 || c.Objects[0].Skipped || c.Objects[0].BeforeVersionID != nil || *c.Objects[0].AfterVersionID != *p2.Items[0].CurrentVersionID {
		t.Fatal(c)
	}
	page, err := publishing.ReadPromotePage(ctx, e.Q, e.Admin, p2.ID, doc, "production", c.Snapshot, "", 1)
	if err != nil || page.TotalChanges == 0 || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	if page.TotalChanges > 1 {
		next, err := publishing.ReadPromotePage(ctx, e.Q, e.Admin, p2.ID, doc, "production", c.Snapshot, *page.NextCursor, 1)
		if err != nil || len(next.Items) != 1 {
			t.Fatal(next, err)
		}
	}
	if _, err := publishing.ReadPromotePage(ctx, e.Q, e.Admin, p2.ID, doc, "production", "sha256:old", "", 1); cmstest.Code(err) != "PROMOTE_SNAPSHOT_STALE" {
		t.Fatal(err)
	}
	if _, err := publishing.ReadPromotePage(ctx, e.Q, e.Admin, p2.ID, doc, "production", c.Snapshot, "", 0); cmstest.Code(err) != "INVALID_ARGUMENT" {
		t.Fatal(err)
	}
	if _, err := publishing.ReadPromotePage(ctx, e.Q, e.Admin, p2.ID, doc, "production", c.Snapshot, "forged.cursor", 1); cmstest.Code(err) != "INVALID_ARGUMENT" {
		t.Fatal(err)
	}
	if _, err := e.Bus.Dispatch(ctx, e.Admin, guardedPromote(p2.ID, "production", c.Snapshot, "no-reason", " ")); cmstest.Code(err) != "INVALID_ARGUMENT" {
		t.Fatal(err)
	}
	var versionsBefore int
	if err := e.Pool.QueryRow(ctx, "SELECT count(*) FROM object_versions").Scan(&versionsBefore); err != nil {
		t.Fatal(err)
	}
	req := guardedPromote(p2.ID, "production", c.Snapshot, "retry-promote", "Релиз витрины")
	first, err := e.Bus.Dispatch(ctx, e.Admin, req)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := e.Bus.Dispatch(ctx, e.Admin, req)
	if err != nil || !replay.Replayed || string(replay.Body) != string(first.Body) {
		t.Fatal(replay, err)
	}
	var versionsAfter int
	if err := e.Pool.QueryRow(ctx, "SELECT count(*) FROM object_versions").Scan(&versionsAfter); err != nil {
		t.Fatal(err)
	}
	list, _ := publishing.ListPublications(ctx, e.Q, e.Admin.ProjectID, ptr("production"))
	if versionsBefore != versionsAfter || len(list) != 1 || *list[0].Reason != "Релиз витрины" {
		t.Fatal(versionsBefore, versionsAfter, list)
	}
	if d, _ := published(e, doc, "production"); rootName(t, d) != "v2" {
		t.Fatal(rootName(t, d))
	}
	if head, _ := changes.GetDocument(ctx, e.Q, e.Admin.ProjectID, doc, nil); rootName(t, head) != "v2" {
		t.Fatal(rootName(t, head))
	}
	// The executed promotion changed production: the old snapshot is stale.
	if _, err := e.Bus.Dispatch(ctx, e.Admin, guardedPromote(p2.ID, "production", c.Snapshot, "stale", "Повтор")); cmstest.Code(err) != "PROMOTE_SNAPSHOT_STALE" {
		t.Fatal(err)
	}
	if again := promoteCard(t, e, e.Admin, p2.ID, "production"); !blocked(again, "PROMOTE_NOTHING") || !again.Objects[0].Skipped {
		t.Fatal(again)
	}
	old := promoteCard(t, e, e.Admin, p1.ID, "production")
	if !blocked(old, "PROMOTE_OUTDATED:"+doc.String()) || strings.Contains(strings.Join(old.Blockers, ","), "PROMOTE_NOTHING") {
		t.Fatal(old)
	}
	if _, err := e.Bus.Dispatch(ctx, e.Admin, guardedPromote(p1.ID, "production", old.Snapshot, "outdated", "Старое")); cmstest.Code(err) != "PROMOTE_OUTDATED" {
		t.Fatal(err)
	}
	// Promotion from the target onward reuses the same rules (STU-474 is enforced by the BFF).
	promoted := promoteCard(t, e, e.Admin, list[0].ID, "staging")
	if !blocked(promoted, "PROMOTE_NOTHING") {
		t.Fatal(promoted)
	}
}

// STU-465: every mutable condition invalidates the reviewed promotion.
func TestPromoteSnapshotStaleConditions(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	_, _, p2 := staged(e)
	stale := func(name string, change func()) {
		t.Helper()
		c := promoteCard(t, e, e.Admin, p2.ID, "production")
		if !c.CanPromote {
			t.Fatal(name, c)
		}
		change()
		if _, err := e.Bus.Dispatch(ctx, e.Admin, guardedPromote(p2.ID, "production", c.Snapshot, name, "Перенос")); cmstest.Code(err) != "PROMOTE_SNAPSHOT_STALE" {
			t.Fatal(name, err)
		}
	}
	stale("policy", func() {
		e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 0, "high": 1}, nil)
	})
	stale("target-pointer", func() {
		cs, _ := e.Draft(e.Admin, "other")
		approve(e, cs)
		if _, err := publish(e, cs, "production"); err != nil {
			t.Fatal(err)
		}
	})
	stale("approval", func() {
		e.Exec("INSERT INTO approvals (id, changeset_id, approver_id, decision, content_hash) VALUES ($1, $2, $3, 'approve', $4)", uuid.New(), *p2.ChangesetID, e.Admin.ID, []byte{0})
	})
	// The target contract (manifest and kind) is part of the snapshot.
	stale("environment", func() {
		e.Exec("UPDATE environments SET kind='preview' WHERE name='production' AND project_id=$1", e.Admin.ProjectID)
	})
}

// STU-464, 468, 475: access, blockers and unsupported publications.
func TestPromotePreviewBlockersAndAccess(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cs, _ := page(e, "/landing")
	p, err := publish(e, cs, "staging")
	if err != nil {
		t.Fatal(err)
	}
	if c := promoteCard(t, e, e.Admin, p.ID, "staging"); !blocked(c, "PROMOTE_SAME_ENVIRONMENT") {
		t.Fatal(c)
	}
	if _, _, _, err := publishing.LoadPromotePreview(ctx, e.Q, e.Admin, p.ID, "missing", nil); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal(err)
	}
	if _, _, _, err := publishing.LoadPromotePreview(ctx, e.Q, e.Admin, uuid.New(), "production", nil); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal(err)
	}
	unknown := uuid.New()
	if _, _, _, err := publishing.LoadPromotePreview(ctx, e.Q, e.Admin, p.ID, "production", &unknown); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal(err)
	}
	reader := e.Human("Reader", auth.DesignRead, auth.ContentRead)
	if c := promoteCard(t, e, reader, p.ID, "production"); !blocked(c, "PROMOTE_FORBIDDEN") {
		t.Fatal(c)
	}
	none := e.Human("None", auth.ContentRead)
	if _, _, _, err := publishing.LoadPromotePreview(ctx, e.Q, none, p.ID, "production", nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	// Another page already owns the route in production.
	other, otherDoc := page(e, "/landing-other")
	if _, err := publish(e, other, "production"); err != nil {
		t.Fatal(err)
	}
	e.Exec("UPDATE routes SET path='/landing' WHERE object_id=$1", otherDoc)
	if c := promoteCard(t, e, e.Admin, p.ID, "production"); !blocked(c, "PATH_TAKEN") {
		t.Fatal(c)
	}
	e.Exec("UPDATE routes SET path='/landing-other' WHERE object_id=$1", otherDoc)
	if c := promoteCard(t, e, e.Admin, p.ID, "production"); !c.CanPromote {
		t.Fatal(c)
	}
	var rb publishing.Publication
	e.Must(e.Admin, "rollback", map[string]any{"publicationId": p.ID}, &rb)
	if c := promoteCard(t, e, e.Admin, rb.ID, "production"); !blocked(c, "PROMOTE_NOT_SUPPORTED") {
		t.Fatal(c)
	}
	e.Exec("UPDATE publications SET manifest_changed=true WHERE id=$1", p.ID)
	if c := promoteCard(t, e, e.Admin, p.ID, "production"); !blocked(c, "PROMOTE_SCHEMA_NOT_SUPPORTED") {
		t.Fatal(c)
	}
	e.Exec("UPDATE environments SET kind='preview' WHERE name='production' AND project_id=$1", e.Admin.ProjectID)
	if c := promoteCard(t, e, e.Admin, p.ID, "production"); !blocked(c, "ENVIRONMENT_NOT_PUBLISHABLE") {
		t.Fatal(c)
	}
}
