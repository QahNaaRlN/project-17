package auth_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

type fixture struct {
	pool    *pgxpool.Pool
	q       *store.Queries
	project store.Project
	actor   store.Actor
}

func setup(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	pool := pgtest.NewDB(t)
	q := store.New(pool)
	p, err := q.CreateProject(ctx, store.CreateProjectParams{ID: uuid.New(), Slug: "store", Name: "Store"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := q.CreateActor(ctx, store.CreateActorParams{ID: uuid.New(), Kind: "service", ProjectID: &p.ID, DisplayName: "ci"})
	if err != nil {
		t.Fatal(err)
	}
	for name, caps := range map[string][]string{"editor": {"content.read", "content.write"}, "dev": {"content.read", "manifest.register"}} {
		r, err := q.CreateRole(ctx, store.CreateRoleParams{ID: uuid.New(), ProjectID: p.ID, Name: name, Capabilities: caps})
		if err != nil {
			t.Fatal(err)
		}
		if err := q.BindRole(ctx, store.BindRoleParams{ProjectID: p.ID, ActorID: a.ID, RoleID: r.ID}); err != nil {
			t.Fatal(err)
		}
	}
	return fixture{pool, q, p, a}
}

func exec(t *testing.T, f fixture, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func pgxRow(t *testing.T, f fixture, sql string, args ...any) pgx.Row {
	t.Helper()
	return f.pool.QueryRow(context.Background(), sql, args...)
}

func (f fixture) token(t *testing.T, scopes []string, expires time.Time) (string, uuid.UUID) {
	t.Helper()
	secret, hash := auth.NewToken(auth.ServiceTokenPrefix)
	tok, err := f.q.CreateAPIToken(context.Background(), store.CreateAPITokenParams{
		ID: uuid.New(), ProjectID: f.project.ID, ActorID: f.actor.ID, TokenHash: hash, Scopes: scopes, ExpiresAt: expires,
	})
	if err != nil {
		t.Fatal(err)
	}
	return secret, tok.ID
}

func TestAuthenticate(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	secret, _ := f.token(t, []string{"content.read", "manifest.register", "content.write"}, time.Now().Add(time.Hour))

	actor, err := auth.Authenticate(ctx, f.q, "Bearer "+secret)
	if err != nil {
		t.Fatal(err)
	}
	if actor.ID != f.actor.ID || actor.Kind != auth.ActorService || actor.ProjectSlug != "store" || actor.ProjectID != f.project.ID || actor.DisplayName != "ci" {
		t.Errorf("актор: %+v", actor)
	}
	want := []auth.Right{auth.ContentRead, auth.ContentWrite, auth.ManifestRegister}
	if got := actor.Rights.Sorted(); !reflect.DeepEqual(got, want) {
		t.Errorf("права: %v, ожидалось объединение ролей %v", got, want)
	}
}

func TestAuthenticateTouchesToken(t *testing.T) {
	f := setup(t)
	secret, id := f.token(t, []string{auth.ScopeAll}, time.Now().Add(time.Hour))
	if _, err := auth.Authenticate(context.Background(), f.q, "Bearer "+secret); err != nil {
		t.Fatal(err)
	}
	p, err := f.q.GetTokenPrincipal(context.Background(), auth.HashToken(secret))
	if err != nil || p.TokenID != id {
		t.Fatal(err)
	}
	var used *time.Time
	if err := pgxRow(t, f, "SELECT last_used_at FROM api_tokens WHERE id = $1", id).Scan(&used); err != nil || used == nil {
		t.Errorf("last_used_at не обновлён: %v %v", used, err)
	}
}

func TestAuthenticateRejects(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	expired, _ := f.token(t, []string{auth.ScopeAll}, time.Now().Add(-time.Minute))
	revoked, revokedID := f.token(t, []string{auth.ScopeAll}, time.Now().Add(time.Hour))
	exec(t, f, "UPDATE api_tokens SET revoked_at = now() WHERE id = $1", revokedID)

	disabledSecret, _ := f.token(t, []string{auth.ScopeAll}, time.Now().Add(time.Hour))
	cases := map[string]string{
		"без заголовка":     "",
		"не Bearer":         "Basic abc",
		"пустой токен":      "Bearer ",
		"неизвестный токен": "Bearer cms_svc_unknown",
		"истёкший токен":    "Bearer " + expired,
		"отозванный токен":  "Bearer " + revoked,
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := auth.Authenticate(ctx, f.q, header); !errors.Is(err, auth.ErrUnauthenticated) {
				t.Errorf("ожидалась ErrUnauthenticated, получено %v", err)
			}
		})
	}
	t.Run("заблокированный актор", func(t *testing.T) {
		exec(t, f, "UPDATE actors SET disabled_at = now() WHERE id = $1", f.actor.ID)
		if _, err := auth.Authenticate(ctx, f.q, "Bearer "+disabledSecret); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Errorf("получено %v", err)
		}
	})
}
