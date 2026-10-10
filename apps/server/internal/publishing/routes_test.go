package publishing_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

// page создаёт и подаёт Change Set с новой страницей по маршруту path.
func page(e *cmstest.Env, path string) (cs, doc uuid.UUID) {
	e.T.Helper()
	var c changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": path}, &c)
	var res changes.ApplyResult
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": c.ID, "expectedSeq": 0, "operations": []any{
		map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": path, "root": map[string]any{"id": "n_root", "type": "Box"}}},
	}}, &res)
	approve(e, c.ID)
	return c.ID, res.Operations[0].Target
}

// reroute создаёт и подаёт Change Set, меняющий маршрут документа.
func reroute(e *cmstest.Env, doc uuid.UUID, path any) uuid.UUID {
	e.T.Helper()
	var c changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "route"}, &c)
	if err := e.Apply(e.Admin, c.ID, 0, doc, cmstest.Op("document.setRoute", map[string]any{"path": path})); err != nil {
		e.T.Fatal(err)
	}
	approve(e, c.ID)
	return c.ID
}

func routes(e *cmstest.Env, env string) map[string]uuid.UUID {
	e.T.Helper()
	list, err := publishing.ListRoutes(context.Background(), e.Q, e.Admin.ProjectID, env)
	if err != nil {
		e.T.Fatal(err)
	}
	out := map[string]uuid.UUID{}
	for _, r := range list {
		out[r.Path] = r.ObjectID
	}
	return out
}

func TestRoutesFollowPublications(t *testing.T) {
	e := setup(t)
	cs, doc := page(e, "/sale")
	p1, err := publish(e, cs, "staging")
	if err != nil {
		t.Fatal(err)
	}
	if r := routes(e, "staging"); len(r) != 1 || r["/sale"] != doc {
		t.Fatalf("staging: %v", r)
	}
	if len(routes(e, "production")) != 0 {
		t.Error("production пуст")
	}
	if d, _ := published(e, doc, "staging"); d.Path == nil || *d.Path != "/sale" {
		t.Errorf("путь опубликованной версии: %v", d.Path)
	}

	p2, err := publish(e, reroute(e, doc, "/summer-sale"), "staging")
	if err != nil {
		t.Fatal(err)
	}
	if r := routes(e, "staging"); len(r) != 1 || r["/summer-sale"] != doc {
		t.Errorf("после смены маршрута: %v", r)
	}
	if _, err := promote(e, p1.ID, "production"); err != nil {
		t.Fatal(err)
	}
	if r := routes(e, "production"); r["/sale"] != doc {
		t.Errorf("production: %v", r)
	}
	e.Must(e.Admin, "rollback", map[string]any{"publicationId": p2.ID}, nil)
	if r := routes(e, "staging"); len(r) != 1 || r["/sale"] != doc {
		t.Errorf("после отката: %v", r)
	}
	// Снятие маршрута и откат первой публикации убирают страницу из таблицы.
	if _, err := publish(e, reroute(e, doc, nil), "staging"); err != nil {
		t.Fatal(err)
	}
	if r := routes(e, "staging"); len(r) != 0 {
		t.Errorf("маршрут снят: %v", r)
	}
	if list, _ := publishing.ListRoutes(context.Background(), e.Q, e.Admin.ProjectID, "nowhere"); len(list) != 0 {
		t.Errorf("нет окружения: %v", list)
	}
}

func TestPublishPathTaken(t *testing.T) {
	e := setup(t)
	// Оба Change Set заняли форму маршрута до публикации — второй не публикуется.
	a, _ := page(e, "/products/:id")
	b, _ := page(e, "/products/:slug")
	if _, err := publish(e, a, "staging"); err != nil {
		t.Fatal(err)
	}
	if _, err := publish(e, b, "staging"); cmstest.Code(err) != "PATH_TAKEN" {
		t.Errorf("head: %v", err)
	}
}

func TestPromotePathTakenInEnvironment(t *testing.T) {
	e := setup(t)
	csX, x := page(e, "/a")
	px, _ := publish(e, csX, "staging")
	if _, err := promote(e, px.ID, "production"); err != nil {
		t.Fatal(err)
	}
	// X уходит с /a в head и staging, но в production по-прежнему на /a.
	if _, err := publish(e, reroute(e, x, "/b"), "staging"); err != nil {
		t.Fatal(err)
	}
	csY, _ := page(e, "/a")
	py, err := publish(e, csY, "staging")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := promote(e, py.ID, "production"); cmstest.Code(err) != "PATH_TAKEN" {
		t.Errorf("production: %v", err)
	}
}

func TestRoutesDatabaseFaults(t *testing.T) {
	for name, brk := range map[string]func(*cmstest.Env){
		"delete": func(e *cmstest.Env) { e.Break("routes", "DELETE") },
		"insert": func(e *cmstest.Env) { e.Break("routes", "INSERT") },
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			cs, _ := page(e, "/x")
			brk(e)
			_, err := publish(e, cs, "staging")
			cmstest.ExpectDBError(t, err)
		})
	}
	e := setup(t)
	e.Pool.Close()
	if _, err := publishing.ListRoutes(context.Background(), e.Q, e.Admin.ProjectID, "staging"); err == nil {
		t.Error("закрытый пул")
	}
}
