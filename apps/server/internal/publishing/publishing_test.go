package publishing_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

// setup — стенд без обязательных согласований, чтобы подача сразу давала approved.
func setup(t *testing.T) *cmstest.Env {
	e := cmstest.New(t, workflow.Register, publishing.Register)
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 0, "high": 0}, nil)
	return e
}

func approve(e *cmstest.Env, cs uuid.UUID) {
	e.T.Helper()
	e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1}, nil)
}

func publish(e *cmstest.Env, cs uuid.UUID, env string) (publishing.Publication, error) {
	var p publishing.Publication
	err := e.Do(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": env}, &p)
	return p, err
}

func rootName(t *testing.T, d changes.Document) string {
	t.Helper()
	var body struct {
		Nodes map[string]struct{ Name string }
	}
	if err := json.Unmarshal(d.Body, &body); err != nil {
		t.Fatal(err)
	}
	return body.Nodes["n_root"].Name
}

func published(e *cmstest.Env, doc uuid.UUID, env string) (changes.Document, error) {
	return publishing.GetPublishedDocument(context.Background(), e.Q, e.Admin.ProjectID, doc, env)
}

func TestPublishMovesHeadAndPointer(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cs, doc := e.Draft(e.Admin, "v1")
	approve(e, cs)
	if _, err := e.Bus.Dispatch(ctx, e.Admin, commandbus.Request{Name: "publish", IdempotencyKey: uuid.NewString(), Reason: "релиз",
		Payload: json.RawMessage(`{"changesetId":"` + cs.String() + `","environment":"staging"}`)}); err != nil {
		t.Fatal(err)
	}
	pubs, err := publishing.ListPublications(ctx, e.Q, e.Admin.ProjectID, nil)
	if err != nil || len(pubs) != 1 {
		t.Fatalf("публикации: %+v %v", pubs, err)
	}
	p := pubs[0]
	if p.Kind != "publish" || p.Environment != "staging" || *p.ChangesetID != cs || p.Reason == nil || *p.Reason != "релиз" {
		t.Errorf("публикация: %+v", p)
	}
	full, err := publishing.GetPublication(ctx, e.Q, e.Admin.ProjectID, p.ID)
	if err != nil || len(full.Items) != 1 || full.Items[0].ObjectID != doc || full.Items[0].PreviousVersionID != nil {
		t.Errorf("элементы: %+v %v", full, err)
	}

	d, err := published(e, doc, "staging")
	if err != nil || d.State != "committed" || rootName(t, d) != "v1" || d.VersionID != *full.Items[0].CurrentVersionID {
		t.Errorf("staging: %+v %v", d, err)
	}
	if _, err := published(e, doc, "production"); cmstest.Code(err) != "NOT_FOUND" {
		t.Errorf("production не тронут: %v", err)
	}
	head, err := changes.GetDocument(ctx, e.Q, e.Admin.ProjectID, doc, nil)
	if err != nil || head.VersionID != d.VersionID {
		t.Errorf("head: %+v %v", head, err)
	}
	c, _ := changes.GetChangeset(ctx, e.Q, e.Admin.ProjectID, cs)
	if c.State != "merged" {
		t.Errorf("Change Set: %s", c.State)
	}
	if _, err := publish(e, cs, "staging"); cmstest.Code(err) != "CHANGESET_STATE_INVALID" {
		t.Errorf("повторная публикация: %v", err)
	}
}

func TestPublishErrors(t *testing.T) {
	e := setup(t)
	e.Must(e.Admin, "create-environment", map[string]any{"name": "pr-1", "kind": "preview"}, nil)
	cs, _ := e.Draft(e.Admin, "v1")
	if _, err := publish(e, cs, "staging"); cmstest.Code(err) != "CHANGESET_STATE_INVALID" {
		t.Errorf("не согласован: %v", err)
	}
	approve(e, cs)
	for env, code := range map[string]string{"nowhere": "NOT_FOUND", "pr-1": "ENVIRONMENT_NOT_PUBLISHABLE"} {
		if _, err := publish(e, cs, env); cmstest.Code(err) != code {
			t.Errorf("%s: %v", env, err)
		}
	}
	if _, err := publish(e, uuid.New(), "staging"); cmstest.Code(err) != "NOT_FOUND" {
		t.Errorf("нет Change Set: %v", err)
	}
	if err := e.Do(e.Human("viewer", auth.ContentRead), "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Errorf("без content.publish: %v", err)
	}
}

func TestPublishRequiresRebaseWhenHeadMoved(t *testing.T) {
	e := setup(t)
	cs, doc := e.Draft(e.Admin, "v1")
	approve(e, cs)
	if _, err := publish(e, cs, "staging"); err != nil {
		t.Fatal(err)
	}
	a, b := e.Edit(e.Admin, doc, "a"), e.Edit(e.Admin, doc, "b")
	approve(e, a)
	approve(e, b)
	if _, err := publish(e, a, "staging"); err != nil {
		t.Fatal(err)
	}
	if _, err := publish(e, b, "staging"); cmstest.Code(err) != "REBASE_REQUIRED" {
		t.Errorf("head сдвинулся: %v", err)
	}
	// Head снят после согласования — и публикация, и подача требуют rebase.
	d, c := e.Edit(e.Admin, doc, "d"), e.Edit(e.Admin, doc, "c")
	approve(e, d)
	e.Exec("UPDATE objects SET head_version_id = NULL WHERE id = $1", doc)
	if _, err := publish(e, d, "staging"); cmstest.Code(err) != "REBASE_REQUIRED" {
		t.Errorf("публикация: %v", err)
	}
	if err := e.Do(e.Admin, "submit-changeset", map[string]any{"changesetId": c, "expectedSeq": 1}, nil); cmstest.Code(err) != "REBASE_REQUIRED" {
		t.Errorf("подача: %v", err)
	}
}

func TestRollback(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cs, doc := e.Draft(e.Admin, "v1")
	approve(e, cs)
	p1, _ := publish(e, cs, "staging")
	edit := e.Edit(e.Admin, doc, "v2")
	approve(e, edit)
	p2, err := publish(e, edit, "staging")
	if err != nil {
		t.Fatal(err)
	}

	if err := e.Do(e.Admin, "rollback", map[string]any{"publicationId": p1.ID}, nil); cmstest.Code(err) != "ROLLBACK_SUPERSEDED" {
		t.Errorf("перекрытая публикация: %v", err)
	}
	if err := e.Do(e.Admin, "rollback", map[string]any{"publicationId": uuid.New()}, nil); cmstest.Code(err) != "NOT_FOUND" {
		t.Errorf("нет публикации: %v", err)
	}

	var rb publishing.Publication
	e.Must(e.Admin, "rollback", map[string]any{"publicationId": p2.ID}, &rb)
	if rb.Kind != "rollback" || *rb.SourcePublicationID != p2.ID || rb.ChangesetID != nil || rb.Items[0].CurrentVersionID == nil {
		t.Errorf("откат: %+v", rb)
	}
	if d, _ := published(e, doc, "staging"); rootName(t, d) != "v1" {
		t.Errorf("staging после отката: %s", rootName(t, d))
	}
	if head, _ := changes.GetDocument(ctx, e.Q, e.Admin.ProjectID, doc, nil); rootName(t, head) != "v1" {
		t.Errorf("head после отката: %s", rootName(t, head))
	}

	// Откат первой публикации снимает документ с публикации и обнуляет head.
	e.Must(e.Admin, "rollback", map[string]any{"publicationId": p1.ID}, &rb)
	if _, err := published(e, doc, "staging"); cmstest.Code(err) != "NOT_FOUND" {
		t.Errorf("документ снят с публикации: %v", err)
	}
	if rb.Items[0].CurrentVersionID != nil {
		t.Errorf("%+v", rb.Items)
	}

	staging := "staging"
	list, _ := publishing.ListPublications(ctx, e.Q, e.Admin.ProjectID, &staging)
	kinds := []string{}
	for _, p := range list {
		kinds = append(kinds, p.Kind)
	}
	if len(kinds) != 4 || kinds[0] != "rollback" || kinds[3] != "publish" {
		t.Errorf("история: %v", kinds)
	}
	prod := "production"
	if list, _ := publishing.ListPublications(ctx, e.Q, e.Admin.ProjectID, &prod); len(list) != 0 {
		t.Errorf("production: %+v", list)
	}
	if _, err := publishing.GetPublication(ctx, e.Q, e.Admin.ProjectID, uuid.New()); cmstest.Code(err) != "NOT_FOUND" {
		t.Errorf("%v", err)
	}
}

func TestRollbackKeepsHeadWhenAsked(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cs, doc := e.Draft(e.Admin, "v1")
	approve(e, cs)
	publish(e, cs, "staging")
	edit := e.Edit(e.Admin, doc, "v2")
	approve(e, edit)
	p2, _ := publish(e, edit, "staging")
	e.Must(e.Admin, "rollback", map[string]any{"publicationId": p2.ID, "resetHead": false}, nil)
	if d, _ := published(e, doc, "staging"); rootName(t, d) != "v1" {
		t.Errorf("staging: %s", rootName(t, d))
	}
	if head, _ := changes.GetDocument(ctx, e.Q, e.Admin.ProjectID, doc, nil); rootName(t, head) != "v2" {
		t.Errorf("head не трогали: %s", rootName(t, head))
	}
}

func TestRollbackKeepsHeadMovedByOthers(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cs, doc := e.Draft(e.Admin, "v1")
	approve(e, cs)
	p1, _ := publish(e, cs, "staging")
	// Head сдвинула публикация в production; откат staging head не трогает.
	edit := e.Edit(e.Admin, doc, "v2")
	approve(e, edit)
	if _, err := publish(e, edit, "production"); err != nil {
		t.Fatal(err)
	}
	e.Must(e.Admin, "rollback", map[string]any{"publicationId": p1.ID}, nil)
	if head, _ := changes.GetDocument(ctx, e.Q, e.Admin.ProjectID, doc, nil); rootName(t, head) != "v2" {
		t.Errorf("head: %s", rootName(t, head))
	}
	if d, _ := published(e, doc, "production"); rootName(t, d) != "v2" {
		t.Errorf("production: %s", rootName(t, d))
	}
}
