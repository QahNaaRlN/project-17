package publishing_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

func TestPublishDatabaseFaults(t *testing.T) {
	for name, brk := range map[string]func(*cmstest.Env){
		"environments":      func(e *cmstest.Env) { e.Break("environments", "") },
		"changeset_objects": func(e *cmstest.Env) { e.Break("changeset_objects", "") },
		"objects":           func(e *cmstest.Env) { e.Break("objects", "") },
		"publications":      func(e *cmstest.Env) { e.Break("publications", "INSERT") },
		"version number":    func(e *cmstest.Env) { e.Exec("ALTER TABLE object_versions RENAME COLUMN number TO broken") },
		"commit":            func(e *cmstest.Env) { e.Break("object_versions", "UPDATE") },
		"head":              func(e *cmstest.Env) { e.Break("objects", "UPDATE") },
		"pointer read":      func(e *cmstest.Env) { e.Break("published_pointers", "") },
		"pointer write":     func(e *cmstest.Env) { e.Break("published_pointers", "INSERT") },
		"merge":             func(e *cmstest.Env) { e.Break("changesets", "UPDATE") },
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			cs, _ := e.Draft(e.Admin, "v1")
			approve(e, cs)
			brk(e)
			_, err := publish(e, cs, "staging")
			cmstest.ExpectDBError(t, err)
		})
	}
}

func TestRollbackDatabaseFaults(t *testing.T) {
	for name, brk := range map[string]func(*cmstest.Env){
		"publications":      func(e *cmstest.Env) { e.Break("publications", "") },
		"publication_items": func(e *cmstest.Env) { e.Break("publication_items", "") },
		"objects":           func(e *cmstest.Env) { e.Break("objects", "") },
		"pointer read":      func(e *cmstest.Env) { e.Break("published_pointers", "") },
		"create":            func(e *cmstest.Env) { e.Break("publications", "INSERT") },
		"pointer delete":    func(e *cmstest.Env) { e.Break("published_pointers", "DELETE") },
		"head":              func(e *cmstest.Env) { e.Break("objects", "UPDATE") },
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			cs, _ := e.Draft(e.Admin, "v1")
			approve(e, cs)
			p, _ := publish(e, cs, "staging")
			brk(e)
			cmstest.ExpectDBError(t, e.Do(e.Admin, "rollback", map[string]any{"publicationId": p.ID}, nil))
		})
	}
}

func TestQueriesDatabaseFaults(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cs, doc := e.Draft(e.Admin, "v1")
	approve(e, cs)
	p, _ := publish(e, cs, "staging")
	e.Break("publication_items", "")
	if _, err := publishing.GetPublication(ctx, e.Q, e.Admin.ProjectID, p.ID); err == nil {
		t.Error("элементы публикации")
	}
	e.Pool.Close()
	id := uuid.New()
	for name, call := range map[string]func() error{
		"GetPublication":   func() error { _, err := publishing.GetPublication(ctx, e.Q, e.Admin.ProjectID, id); return err },
		"ListPublications": func() error { _, err := publishing.ListPublications(ctx, e.Q, e.Admin.ProjectID, nil); return err },
		"GetPublishedDocument": func() error {
			_, err := publishing.GetPublishedDocument(ctx, e.Q, e.Admin.ProjectID, doc, "staging")
			return err
		},
	} {
		if err := call(); err == nil || cmstest.Code(err) != "" {
			t.Errorf("%s: ожидалась ошибка БД, получено %v", name, err)
		}
	}
}
