package publishing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

func promote(e *cmstest.Env, pub uuid.UUID, env string) (publishing.Publication, error) {
	var p publishing.Publication
	err := e.Do(e.Admin, "promote", map[string]any{"publicationId": pub, "toEnvironment": env}, &p)
	return p, err
}

// staged публикует новую страницу в staging и правку её корня; возвращает документ и обе публикации.
func staged(e *cmstest.Env) (doc uuid.UUID, p1, p2 publishing.Publication) {
	e.T.Helper()
	cs, doc := e.Draft(e.Admin, "v1")
	approve(e, cs)
	p1, err := publish(e, cs, "staging")
	if err != nil {
		e.T.Fatal(err)
	}
	edit := e.Edit(e.Admin, doc, "v2")
	approve(e, edit)
	if p2, err = publish(e, edit, "staging"); err != nil {
		e.T.Fatal(err)
	}
	return doc, p1, p2
}

func TestPromoteToProduction(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	doc, _, p2 := staged(e)

	if _, err := e.Bus.Dispatch(ctx, e.Admin, commandbus.Request{Name: "promote", IdempotencyKey: uuid.NewString(), Reason: "релиз",
		Payload: []byte(`{"publicationId":"` + p2.ID.String() + `","toEnvironment":"production"}`)}); err != nil {
		t.Fatal(err)
	}
	list, _ := publishing.ListPublications(ctx, e.Q, e.Admin.ProjectID, ptr("production"))
	if len(list) != 1 {
		t.Fatalf("публикации production: %+v", list)
	}
	p := list[0]
	if p.Kind != "promote" || *p.SourcePublicationID != p2.ID || *p.ChangesetID != *p2.ChangesetID || *p.Reason != "релиз" {
		t.Errorf("публикация: %+v", p)
	}
	full, _ := publishing.GetPublication(ctx, e.Q, e.Admin.ProjectID, p.ID)
	if len(full.Items) != 1 || full.Items[0].PreviousVersionID != nil || *full.Items[0].CurrentVersionID != *p2.Items[0].CurrentVersionID {
		t.Errorf("элементы: %+v", full.Items)
	}
	if d, _ := published(e, doc, "production"); rootName(t, d) != "v2" {
		t.Errorf("production: %s", rootName(t, d))
	}
	if _, err := promote(e, p2.ID, "production"); cmstest.Code(err) != "PROMOTE_NOTHING" {
		t.Errorf("повтор: %v", err)
	}

	// Откат продвижения возвращает production, но не трогает head: v2 по-прежнему в staging.
	var rb publishing.Publication
	e.Must(e.Admin, "rollback", map[string]any{"publicationId": p.ID}, &rb)
	if _, err := published(e, doc, "production"); cmstest.Code(err) != "NOT_FOUND" {
		t.Errorf("production после отката: %v", err)
	}
	if head, _ := changes.GetDocument(ctx, e.Q, e.Admin.ProjectID, doc, nil); rootName(t, head) != "v2" {
		t.Errorf("head не должен меняться: %s", rootName(t, head))
	}
	if _, err := promote(e, rb.ID, "staging"); cmstest.Code(err) != "PROMOTE_NOT_SUPPORTED" {
		t.Errorf("откат не продвигается: %v", err)
	}
}

func TestPromoteOrdering(t *testing.T) {
	e := setup(t)
	doc, p1, p2 := staged(e)
	// Старая публикация в пустое окружение — можно; затем более новая сдвигает указатель.
	if _, err := promote(e, p1.ID, "production"); err != nil {
		t.Fatal(err)
	}
	next, err := promote(e, p2.ID, "production")
	if err != nil || *next.Items[0].PreviousVersionID != *p1.Items[0].CurrentVersionID {
		t.Fatalf("%+v %v", next, err)
	}
	// Вернуть production к старой версии продвижением нельзя.
	if _, err := promote(e, p1.ID, "production"); cmstest.Code(err) != "PROMOTE_OUTDATED" {
		t.Errorf("откат продвижением: %v", err)
	}
	// Продвижение продвижения — в третье окружение.
	e.Must(e.Admin, "create-environment", map[string]any{"name": "eu", "kind": "standard"}, nil)
	if _, err := promote(e, next.ID, "eu"); err != nil {
		t.Errorf("promote из promote: %v", err)
	}
	if d, _ := published(e, doc, "eu"); rootName(t, d) != "v2" {
		t.Errorf("eu: %s", rootName(t, d))
	}
}

func TestPromoteMovesOnlyChangedObjects(t *testing.T) {
	e := setup(t)
	a, docA := e.Draft(e.Admin, "a")
	approve(e, a)
	pa, _ := publish(e, a, "staging")
	if _, err := promote(e, pa.ID, "production"); err != nil {
		t.Fatal(err)
	}
	// Change Set с двумя документами: один уже в production, другой нет.
	var both changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "both"}, &both)
	if err := e.Apply(e.Admin, both.ID, 0, docA, cmstest.Op("node.rename", map[string]any{"nodeId": "n_root", "name": "a2"})); err != nil {
		t.Fatal(err)
	}
	var res changes.ApplyResult
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": both.ID, "expectedSeq": 1, "operations": []any{
		map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "root": map[string]any{"id": "n_root", "type": "Box"}}},
	}}, &res)
	e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": both.ID, "expectedSeq": 2}, nil)
	pb, err := publish(e, both.ID, "staging")
	if err != nil || len(pb.Items) != 2 {
		t.Fatalf("%+v %v", pb, err)
	}
	// Вручную выравниваем docA в production с этой публикацией — продвигается только новый документ.
	for _, it := range pb.Items {
		if it.ObjectID == docA {
			e.Exec(`UPDATE published_pointers SET version_id = $1
			  WHERE object_id = $2 AND environment_id = (SELECT id FROM environments WHERE name = 'production')`, *it.CurrentVersionID, docA)
		}
	}
	p, err := promote(e, pb.ID, "production")
	if err != nil || len(p.Items) != 1 || p.Items[0].ObjectID != res.Operations[0].Target {
		t.Errorf("%+v %v", p, err)
	}
}

func TestPromoteErrors(t *testing.T) {
	e := setup(t)
	e.Must(e.Admin, "create-environment", map[string]any{"name": "pr-1", "kind": "preview"}, nil)
	_, p1, _ := staged(e)
	for name, tc := range map[string]struct {
		pub  uuid.UUID
		env  string
		code string
	}{
		"нет публикации":  {uuid.New(), "production", "NOT_FOUND"},
		"нет окружения":   {p1.ID, "nowhere", "NOT_FOUND"},
		"preview":         {p1.ID, "pr-1", "ENVIRONMENT_NOT_PUBLISHABLE"},
		"то же окружение": {p1.ID, "staging", "PROMOTE_SAME_ENVIRONMENT"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := promote(e, tc.pub, tc.env); cmstest.Code(err) != tc.code {
				t.Errorf("%v", err)
			}
		})
	}
	if err := e.Do(e.Human("viewer", auth.ContentRead), "promote", map[string]any{"publicationId": p1.ID, "toEnvironment": "production"}, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Errorf("без content.publish: %v", err)
	}
}

func TestPromoteDatabaseFaults(t *testing.T) {
	for name, tc := range map[string]struct {
		prepare bool // в production уже есть версия объекта
		brk     func(e *cmstest.Env)
	}{
		"publications":      {false, func(e *cmstest.Env) { e.Break("publications", "") }},
		"publication_items": {false, func(e *cmstest.Env) { e.Break("publication_items", "") }},
		"objects":           {false, func(e *cmstest.Env) { e.Break("objects", "") }},
		"pointers":          {false, func(e *cmstest.Env) { e.Break("published_pointers", "") }},
		"versions":          {true, func(e *cmstest.Env) { e.Break("object_versions", "") }},
		"create":            {false, func(e *cmstest.Env) { e.Break("publications", "INSERT") }},
		"move":              {false, func(e *cmstest.Env) { e.Break("published_pointers", "INSERT") }},
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			_, p1, p2 := staged(e)
			if tc.prepare {
				if _, err := promote(e, p1.ID, "production"); err != nil {
					t.Fatal(err)
				}
			}
			tc.brk(e)
			_, err := promote(e, p2.ID, "production")
			cmstest.ExpectDBError(t, err)
		})
	}
}

func ptr(s string) *string { return &s }

// PROMOTE_OUTDATED перечисляет все объекты с более новыми версиями.
func TestPromoteOutdatedListsAllObjects(t *testing.T) {
	e := setup(t)
	var c changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "two"}, &c)
	page := map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "root": map[string]any{"id": "n_root", "type": "Box"}}}
	var res changes.ApplyResult
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": c.ID, "expectedSeq": 0, "operations": []any{page, page}}, &res)
	e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": c.ID, "expectedSeq": 2}, nil)
	p1, _ := publish(e, c.ID, "staging")

	var edit changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "edit"}, &edit)
	for i, op := range res.Operations {
		if err := e.Apply(e.Admin, edit.ID, i, op.Target, cmstest.Op("node.rename", map[string]any{"nodeId": "n_root", "name": "v2"})); err != nil {
			t.Fatal(err)
		}
	}
	e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": edit.ID, "expectedSeq": 2}, nil)
	p2, _ := publish(e, edit.ID, "staging")
	if _, err := promote(e, p2.ID, "production"); err != nil {
		t.Fatal(err)
	}
	err := e.Do(e.Admin, "promote", map[string]any{"publicationId": p1.ID, "toEnvironment": "production"}, nil)
	var cerr *commandbus.Error
	if !errors.As(err, &cerr) || cerr.Code != "PROMOTE_OUTDATED" || len(cerr.Params["objects"].([]uuid.UUID)) != 2 {
		t.Errorf("%v", err)
	}
}
