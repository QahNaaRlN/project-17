package delivery_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/delivery"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

func setup(t *testing.T) *cmstest.Env {
	e := cmstest.New(t, workflow.Register, publishing.Register, delivery.Register)
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 0, "high": 0}, nil)
	return e
}

func key(e *cmstest.Env, env string) delivery.Key {
	e.T.Helper()
	var k delivery.Key
	e.Must(e.Admin, "create-delivery-key", map[string]any{"environment": env, "name": "сайт " + env}, &k)
	return k
}

// publishPage публикует страницу по маршруту path в staging.
func publishPage(e *cmstest.Env, path string) uuid.UUID {
	e.T.Helper()
	var c changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": path}, &c)
	var res changes.ApplyResult
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": c.ID, "expectedSeq": 0, "operations": []any{
		map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": path, "root": map[string]any{"id": "n_root", "type": "Box", "name": path}}},
	}}, &res)
	e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": c.ID, "expectedSeq": 1}, nil)
	e.Must(e.Admin, "publish", map[string]any{"changesetId": c.ID, "environment": "staging"}, nil)
	return res.Operations[0].Target
}

func access(t *testing.T, e *cmstest.Env, k delivery.Key) delivery.Access {
	t.Helper()
	a, err := delivery.Authenticate(context.Background(), e.Q, "Bearer "+k.Secret)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestDeliveryKeys(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	k := key(e, "staging")
	if len(k.Secret) < 40 || k.Secret[:8] != auth.DeliveryKeyPrefix || k.Environment != "staging" || k.Name != "сайт staging" {
		t.Errorf("ключ: %+v", k)
	}
	a := access(t, e, k)
	if a.ProjectSlug != "store" || a.Environment != "staging" || a.ProjectID != e.Admin.ProjectID {
		t.Errorf("доступ: %+v", a)
	}
	list, err := delivery.ListKeys(ctx, e.Q, e.Admin.ProjectID)
	if err != nil || len(list) != 1 || list[0].Secret != "" || list[0].RevokedAt != nil {
		t.Errorf("список: %+v %v", list, err)
	}

	var revoked delivery.Key
	e.Must(e.Admin, "revoke-delivery-key", map[string]any{"id": k.ID}, &revoked)
	if revoked.RevokedAt == nil || revoked.ID != k.ID {
		t.Errorf("отзыв: %+v", revoked)
	}
	if _, err := delivery.Authenticate(ctx, e.Q, "Bearer "+k.Secret); err != delivery.ErrUnauthenticated {
		t.Errorf("отозванный ключ: %v", err)
	}
	if err := e.Do(e.Admin, "revoke-delivery-key", map[string]any{"id": k.ID}, nil); cmstest.Code(err) != "NOT_FOUND" {
		t.Errorf("повторный отзыв: %v", err)
	}
	for _, header := range []string{"", "Basic x", "Bearer cms_svc_x", "Bearer cms_pub_unknown"} {
		if _, err := delivery.Authenticate(ctx, e.Q, header); err != delivery.ErrUnauthenticated {
			t.Errorf("%q: %v", header, err)
		}
	}
}

func TestDeliveryKeyErrors(t *testing.T) {
	e := setup(t)
	for name, tc := range map[string]struct {
		payload map[string]any
		code    string
	}{
		"нет окружения": {map[string]any{"environment": "nowhere", "name": "x"}, "NOT_FOUND"},
		"пустое имя":    {map[string]any{"environment": "staging", "name": "  "}, "VALIDATION_FAILED"},
		"длинное имя":   {map[string]any{"environment": "staging", "name": string(make([]rune, 101))}, "VALIDATION_FAILED"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := e.Do(e.Admin, "create-delivery-key", tc.payload, nil); cmstest.Code(err) != tc.code {
				t.Errorf("%v", err)
			}
		})
	}
	if err := e.Do(e.Human("editor", auth.ContentPublish), "create-delivery-key", map[string]any{"environment": "staging", "name": "x"}, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Errorf("без project.admin: %v", err)
	}
}

func TestPagesAndDocuments(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	home := publishPage(e, "/")
	product := publishPage(e, "/products/:slug")
	sale := publishPage(e, "/products/sale")
	a := access(t, e, key(e, "staging"))

	for path, want := range map[string]struct {
		id     uuid.UUID
		params map[string]string
	}{
		"/":               {home, map[string]string{}},
		"/products/shirt": {product, map[string]string{"slug": "shirt"}},
		"/products/sale":  {sale, map[string]string{}}, // литерал сильнее параметра
	} {
		p, err := delivery.GetPage(ctx, e.Q, a, path)
		if err != nil || p.Page.ObjectID != want.id || len(p.Page.Params) != len(want.params) || p.Page.Params["slug"] != want.params["slug"] {
			t.Errorf("%s: %+v %v", path, p.Page, err)
		}
	}
	for _, path := range []string{"/products", "/products/a/b", "/missing"} {
		if _, err := delivery.GetPage(ctx, e.Q, a, path); cmstest.Code(err) != "NOT_FOUND" {
			t.Errorf("%s: %v", path, err)
		}
	}
	d, err := delivery.GetDocument(ctx, e.Q, a, sale)
	if err != nil || *d.Path != "/products/sale" || len(d.Document) == 0 {
		t.Errorf("документ: %+v %v", d, err)
	}
	if _, err := delivery.GetDocument(ctx, e.Q, a, uuid.New()); cmstest.Code(err) != "NOT_FOUND" {
		t.Errorf("%v", err)
	}
	if routes, err := delivery.GetRoutes(ctx, e.Q, a); err != nil || len(routes) != 3 {
		t.Errorf("маршруты: %+v %v", routes, err)
	}
	// В production ничего не опубликовано.
	prod := access(t, e, key(e, "production"))
	if _, err := delivery.GetPage(ctx, e.Q, prod, "/"); cmstest.Code(err) != "NOT_FOUND" {
		t.Errorf("production: %v", err)
	}
}

func TestMatch(t *testing.T) {
	route := func(path string) publishing.Route { return publishing.Route{Path: path, ObjectID: uuid.New()} }
	routes := []publishing.Route{route("/a/:x/c"), route("/a/b/:y"), route("/:p"), route("/")}
	for path, want := range map[string]string{
		"/a/b/c": "/a/b/:y", // первый различающийся сегмент — литерал
		"/a/z/c": "/a/:x/c",
		"/q":     "/:p",
		"/":      "/",
	} {
		r, _, ok := delivery.Match(routes, path)
		if !ok || r.Path != want {
			t.Errorf("%s: %s %v", path, r.Path, ok)
		}
	}
	for _, path := range []string{"//", "/a//c", "/a/b"} {
		if r, _, ok := delivery.Match(routes, path); ok {
			t.Errorf("%s не должен совпасть: %s", path, r.Path)
		}
	}
}

func TestDeliveryDatabaseFaults(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	k := key(e, "staging")
	a := access(t, e, k)
	e.Pool.Close()
	if _, err := delivery.Authenticate(ctx, e.Q, "Bearer "+k.Secret); err == nil || err == delivery.ErrUnauthenticated {
		t.Errorf("Authenticate: %v", err)
	}
	if _, err := delivery.ListKeys(ctx, e.Q, a.ProjectID); err == nil {
		t.Error("ListKeys")
	}
	if _, err := delivery.GetPage(ctx, e.Q, a, "/"); err == nil || cmstest.Code(err) != "" {
		t.Errorf("GetPage: %v", err)
	}
	if _, err := delivery.GetDocument(ctx, e.Q, a, uuid.New()); err == nil || cmstest.Code(err) != "" {
		t.Errorf("GetDocument: %v", err)
	}
}

func TestDeliveryKeyCommandFaults(t *testing.T) {
	for name, brk := range map[string]func(*cmstest.Env){
		"environments": func(e *cmstest.Env) { e.Break("environments", "") },
		"insert":       func(e *cmstest.Env) { e.Break("delivery_keys", "INSERT") },
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			brk(e)
			cmstest.ExpectDBError(t, e.Do(e.Admin, "create-delivery-key", map[string]any{"environment": "staging", "name": "x"}, nil))
		})
	}
	for name, brk := range map[string]func(*cmstest.Env){
		"update": func(e *cmstest.Env) { e.Break("delivery_keys", "UPDATE") },
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			k := key(e, "staging")
			brk(e)
			cmstest.ExpectDBError(t, e.Do(e.Admin, "revoke-delivery-key", map[string]any{"id": k.ID}, nil))
		})
	}
}
