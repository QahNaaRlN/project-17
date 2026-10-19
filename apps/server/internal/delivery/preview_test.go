package delivery_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/delivery"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

func preview(e *cmstest.Env, env string, cs *uuid.UUID) delivery.PreviewToken {
	e.T.Helper()
	var p delivery.PreviewToken
	payload := map[string]any{"environment": env}
	if cs != nil {
		payload["changesetId"] = cs
	}
	e.Must(e.Admin, "create-preview-token", payload, &p)
	return p
}

func previewAccess(t *testing.T, e *cmstest.Env, p delivery.PreviewToken) delivery.Access {
	t.Helper()
	a, err := delivery.AuthenticatePreview(context.Background(), e.Q, "Preview "+p.Token)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestDraftReadsWorkingVersions(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	sale := publishPage(e, "/sale")

	// Черновик: новая страница, переименование и смена маршрута опубликованной.
	var c changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "draft"}, &c)
	var res changes.ApplyResult
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": c.ID, "expectedSeq": 0, "operations": []any{
		map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": "/new", "root": map[string]any{"id": "n_root", "type": "Box"}}},
		map[string]any{"type": "document.setRoute", "target": sale, "payload": map[string]any{"path": "/summer-sale"}},
	}}, &res)
	newPage := res.Operations[0].Target

	p := preview(e, "staging", &c.ID)
	if p.ChangesetID == nil || *p.ChangesetID != c.ID || p.Environment != "staging" || p.ExpiresAt.IsZero() {
		t.Errorf("токен: %+v", p)
	}
	a := previewAccess(t, e, p)
	if !a.Draft || *a.ChangesetID != c.ID || a.ProjectID != e.Admin.ProjectID || a.Environment != "staging" {
		t.Fatalf("доступ: %+v", a)
	}
	routes, err := delivery.GetRoutes(ctx, e.Q, a)
	if err != nil || len(routes) != 2 || routes[0].Path != "/new" || routes[1].Path != "/summer-sale" {
		t.Errorf("маршруты черновика: %+v %v", routes, err)
	}
	page, err := delivery.GetPage(ctx, e.Q, a, "/new")
	if err != nil || page.Page.ObjectID != newPage || page.Diagnostics == nil || len(page.Diagnostics) != 0 {
		t.Errorf("новая страница: %+v %v", page, err)
	}
	if _, err := delivery.GetPage(ctx, e.Q, a, "/sale"); cmstest.Code(err) != "NOT_FOUND" {
		t.Errorf("старый маршрут в черновике: %v", err)
	}

	// Без Change Set черновик — это head: новой страницы нет, у sale прежний маршрут.
	head := previewAccess(t, e, preview(e, "staging", nil))
	routes, _ = delivery.GetRoutes(ctx, e.Q, head)
	if len(routes) != 1 || routes[0].Path != "/sale" {
		t.Errorf("head: %+v", routes)
	}
	if _, err := delivery.GetDocument(ctx, e.Q, head, newPage); cmstest.Code(err) != "NOT_FOUND" {
		t.Errorf("документ чужого Change Set: %v", err)
	}
}

func TestDraftDiagnostics(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	var c changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "broken"}, &c)
	var res changes.ApplyResult
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": c.ID, "expectedSeq": 0, "operations": []any{
		map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "root": map[string]any{"id": "n_root", "type": "Box"}}},
	}}, &res)
	doc := res.Operations[0].Target
	e.Exec(`UPDATE object_versions SET body = jsonb_set(body, '{root}', '"n_missing"') WHERE object_id = $1`, doc)
	d, err := delivery.GetDocument(ctx, e.Q, previewAccess(t, e, preview(e, "staging", &c.ID)), doc)
	if err != nil || len(d.Diagnostics) == 0 {
		t.Errorf("диагностика: %+v %v", d, err)
	}
}

func TestPreviewTokenErrors(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if err := e.Do(e.Admin, "create-preview-token", map[string]any{"environment": "nowhere"}, nil); cmstest.Code(err) != "NOT_FOUND" {
		t.Errorf("нет окружения: %v", err)
	}
	if err := e.Do(e.Admin, "create-preview-token", map[string]any{"environment": "staging", "changesetId": uuid.New()}, nil); cmstest.Code(err) != "NOT_FOUND" {
		t.Errorf("нет Change Set: %v", err)
	}
	if err := e.Do(e.Human("nobody", auth.AssetWrite), "create-preview-token", map[string]any{"environment": "staging"}, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Errorf("без content.read: %v", err)
	}
	p, prod := preview(e, "staging", nil), preview(e, "production", nil)
	for name, header := range map[string]string{
		"без схемы": p.Token,
		"мусор":     "Preview x.y.z",
	} {
		if _, err := delivery.AuthenticatePreview(ctx, e.Q, header); err != delivery.ErrUnauthenticated {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Ключ окружения сменён — его токены недействительны, токены других окружений — нет.
	e.Exec("UPDATE environments SET preview_key = '\\x00' WHERE name = 'staging'")
	if _, err := delivery.AuthenticatePreview(ctx, e.Q, "Preview "+p.Token); err != delivery.ErrUnauthenticated {
		t.Errorf("сменённый ключ: %v", err)
	}
	previewAccess(t, e, prod)
	// Окружение токена переименовано — токен ссылается на несуществующее.
	e.Exec("UPDATE environments SET name = 'production-old' WHERE name = 'production'")
	if _, err := delivery.AuthenticatePreview(ctx, e.Q, "Preview "+prod.Token); err != delivery.ErrUnauthenticated {
		t.Errorf("неизвестное окружение: %v", err)
	}
	e.Pool.Close()
	if _, err := delivery.AuthenticatePreview(ctx, e.Q, "Preview "+p.Token); err == nil || err == delivery.ErrUnauthenticated {
		t.Errorf("сбой БД: %v", err)
	}
}

// prPreview prepares preview/pr-1 bound to staging as create-environment with a
// source does (STU-315); the GitHub catalogue itself is covered in capabilities.
func prPreview(e *cmstest.Env) {
	e.T.Helper()
	e.Exec(`INSERT INTO capability_github_configs(id,project_id,environment_id,environment,config_id,fingerprint,credential_id,enabled,changed_by)
		SELECT $1,project_id,id,name,'repo','f','key',true,$2 FROM environments WHERE project_id=$3 AND name='staging'`, uuid.New(), e.Admin.ID, e.Admin.ProjectID)
	e.Exec(`INSERT INTO environments(id,project_id,name,kind,expires_at,source_base_environment_id,source_config_id,source_repository_id,source_pull_number,registration_seq)
		SELECT $1,s.project_id,'preview/pr-1','preview',now()+interval '14 days',s.id,c.id,'42',1,1
		FROM environments s JOIN capability_github_configs c ON c.environment_id=s.id WHERE s.project_id=$2 AND s.name='staging'`, uuid.New(), e.Admin.ProjectID)
}

func TestSTU327_329PreviewTokenBoundToBuild(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	prPreview(e)
	var staging, production changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "staging", "targets": []string{"staging"}}, &staging)
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "production", "targets": []string{"production"}}, &production)
	reader := e.Human("reader", auth.ContentRead)
	if err := e.Do(reader, "create-preview-token", map[string]any{"environment": "preview/pr-1", "changesetId": staging.ID}, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatalf("STU-327 design.read: %v", err)
	}
	if err := e.Do(e.Admin, "create-preview-token", map[string]any{"environment": "preview/pr-1"}, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatalf("STU-327 Change Set required: %v", err)
	}
	if err := e.Do(e.Admin, "create-preview-token", map[string]any{"environment": "preview/pr-1", "changesetId": production.ID}, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatalf("STU-327 base environment: %v", err)
	}
	p := preview(e, "preview/pr-1", &staging.ID)
	if p.RegistrationSeq == nil || *p.RegistrationSeq != 1 {
		t.Fatalf("STU-329 token: %+v", p)
	}
	if a := previewAccess(t, e, p); !a.Draft || a.Environment != "preview/pr-1" {
		t.Fatalf("access: %+v", a)
	}
	e.Exec(`UPDATE environments SET registration_seq=2 WHERE name='preview/pr-1'`)
	if _, err := delivery.AuthenticatePreview(ctx, e.Q, "Preview "+p.Token); err == nil {
		t.Fatal("STU-329 token of an older build accepted")
	}
	if standard := preview(e, "staging", &staging.ID); standard.RegistrationSeq != nil {
		t.Fatalf("standard tokens are unchanged: %+v", standard)
	}
}
