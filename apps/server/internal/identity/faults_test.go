package identity_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/identity"
	"github.com/qahnaarln/project-17/apps/server/internal/platform/postgres"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

// STU-084: privileges are explicit, scoped, revocable and validated on every admin command.
func TestHumanRoleBoundaries(t *testing.T) {
	f := setup(t)
	_, info := f.login(t, "alice")
	role := f.grant(t, info.User.ID, "writer", auth.DesignRead)
	foreignProject, foreignRole := uuid.New(), uuid.New()
	f.e.Exec("INSERT INTO projects (id,slug,name) VALUES ($1,'foreign','Foreign')", foreignProject)
	f.e.Exec("INSERT INTO roles (id,project_id,name,capabilities) VALUES ($1,$2,'foreign',ARRAY['design.read'])", foreignRole, foreignProject)
	for _, name := range []string{"bind-human-role", "unbind-human-role"} {
		if err := f.e.Do(f.e.Admin, name, map[string]any{"actorId": info.User.ID, "roleId": foreignRole}, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
			t.Fatal(err)
		}
	}
	for _, payload := range []any{map[string]any{"name": "bad role", "capabilities": []string{}}, map[string]any{"name": "valid", "capabilities": []string{"unknown"}}} {
		if err := f.e.Do(f.e.Admin, "create-role", payload, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
			t.Fatal(err)
		}
	}
	if err := f.e.Do(f.e.Admin, "create-role", map[string]any{"name": "writer", "capabilities": []string{}}, nil); cmstest.Code(err) != "ROLE_EXISTS" {
		t.Fatal(err)
	}
	for _, p := range []any{map[string]any{"actorId": f.e.Admin.ID, "roleId": role}, map[string]any{"actorId": info.User.ID, "roleId": uuid.New()}} {
		if err := f.e.Do(f.e.Admin, "bind-human-role", p, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
			t.Fatal(err)
		}
	}
	human := f.e.Human("outsider", auth.DesignRead)
	if err := f.e.Do(human, "bind-human-role", map[string]any{"actorId": info.User.ID, "roleId": role}, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	f.e.Must(f.e.Admin, "unbind-human-role", map[string]any{"actorId": info.User.ID, "roleId": role}, nil)
	f.e.Exec("UPDATE actors SET disabled_at=now() WHERE id=$1", info.User.ID)
	if err := f.e.Do(f.e.Admin, "bind-human-role", map[string]any{"actorId": info.User.ID, "roleId": role}, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
		t.Fatal(err)
	}
}

// STU-081/084: downgrade must preserve stored login/session/identity state.
func TestHumanIdentityMigrationGuard(t *testing.T) {
	for _, signedIn := range []bool{false, true} {
		t.Run(map[bool]string{false: "login", true: "session"}[signedIn], func(t *testing.T) {
			f := setup(t)
			var cookies []*http.Cookie
			if signedIn {
				cookies, _ = f.login(t, "alice")
			} else {
				f.begin(t)
			}
			// Reach the identity migration; later transport metadata can be removed
			// without weakening the guard on persisted human/session facts.
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			for {
				var version int
				if err := f.e.Pool.QueryRow(context.Background(), `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version); err != nil {
					t.Fatal(err)
				}
				if version == 16 {
					break
				}
				if version < 16 {
					t.Fatal("identity migration missing", version)
				}
				if err := postgres.Migrate(context.Background(), f.e.Pool, "down", log); err != nil {
					t.Fatal(err)
				}
			}
			err := postgres.Migrate(context.Background(), f.e.Pool, "down", log)
			if err == nil || !strings.Contains(err.Error(), "human identity/session data prevents downgrade") {
				t.Fatal(err)
			}
			var version int
			if err := f.e.Pool.QueryRow(context.Background(), `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version); err != nil || version != 16 {
				t.Fatal(version, err)
			}
			if signedIn {
				_, data := f.request(t, "GET", "/auth/session", cookies, "", "")
				if data["authenticated"] != true {
					t.Fatal("downgrade destroyed session", data)
				}
			}
		})
	}
}

// STU-080/081: transient storage failures never issue an authenticated cookie or partial identity.
func TestOIDCSessionTransactionFailures(t *testing.T) {
	for _, phase := range []string{"actor", "cleanup", "insert", "commit"} {
		t.Run(phase, func(t *testing.T) {
			f := setup(t)
			callback, cookies := f.begin(t)
			f.e.Exec(`CREATE FUNCTION identity_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private database detail'; END $$`)
			switch phase {
			case "actor":
				f.e.Exec(`CREATE TRIGGER identity_fail BEFORE INSERT ON actors FOR EACH ROW EXECUTE FUNCTION identity_failure()`)
			case "cleanup":
				f.e.Exec(`CREATE TRIGGER identity_fail BEFORE DELETE ON human_sessions FOR EACH STATEMENT EXECUTE FUNCTION identity_failure()`)
			case "insert":
				f.e.Exec(`CREATE TRIGGER identity_fail BEFORE INSERT ON human_sessions FOR EACH ROW EXECUTE FUNCTION identity_failure()`)
			case "commit":
				f.e.Exec(`CREATE CONSTRAINT TRIGGER identity_fail AFTER INSERT ON human_sessions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION identity_failure()`)
			}
			response, _ := f.request(t, "GET", callback, cookies, "", "")
			if !strings.Contains(response.Header.Get("Location"), "authError") {
				t.Fatal("failure was ignored")
			}
			for _, c := range response.Cookies() {
				if c.Name == "cms_session" && c.MaxAge > 0 {
					t.Fatal("session cookie issued")
				}
			}
			var n int
			if err := f.e.Pool.QueryRow(context.Background(), "SELECT count(*) FROM actors WHERE oidc_issuer IS NOT NULL").Scan(&n); err != nil || n != 0 {
				t.Fatal("partial actor", n, err)
			}
		})
	}
}
func TestOIDCSessionReadAndLogoutFailures(t *testing.T) {
	for _, table := range []string{"human_sessions", "projects", "roles"} {
		t.Run(table, func(t *testing.T) {
			f := setup(t)
			cookies, _ := f.login(t, "alice")
			f.e.Exec("ALTER TABLE " + table + " RENAME TO identity_broken")
			r, data := f.request(t, "GET", "/auth/session", cookies, "", "")
			if r.StatusCode != 503 || data["code"] != "SESSION_UNAVAILABLE" {
				t.Fatal(data)
			}
		})
	}
	f := setup(t)
	cookies, info := f.login(t, "alice")
	f.e.Exec(`CREATE FUNCTION logout_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private detail'; END $$`)
	f.e.Exec(`CREATE TRIGGER logout_failure BEFORE DELETE ON human_sessions FOR EACH ROW EXECUTE FUNCTION logout_failure()`)
	r, data := f.request(t, "POST", "/auth/logout", cookies, info.CSRFToken, "")
	if r.StatusCode != 503 || len(r.Cookies()) != 0 {
		t.Fatal(data)
	}
	f.e.Exec("DROP TRIGGER logout_failure ON human_sessions")
	r, data = f.request(t, "POST", "/auth/logout", cookies, info.CSRFToken, "")
	if r.StatusCode != 200 {
		t.Fatal(data)
	}
	r, _ = f.request(t, "GET", "/auth/unknown", nil, "", "")
	if r.StatusCode != 404 {
		t.Fatal(r.StatusCode)
	}
}
func TestOIDCLoginFailureExpiryAndSecureCookies(t *testing.T) {
	f := setup(t)
	callback, cookies := f.begin(t)
	f.e.Exec("UPDATE oidc_logins SET expires_at=now()-interval '1 minute'")
	r, _ := f.request(t, "GET", callback, cookies, "", "")
	if !strings.Contains(r.Header.Get("Location"), "authError") {
		t.Fatal("expired flow accepted")
	}
	f.service.Config.PublicOrigin = "https://studio.example"
	r, _ = f.request(t, "GET", "/auth/login", nil, "", "")
	if r.StatusCode != 302 || len(r.Cookies()) != 1 || r.Cookies()[0].Name != "__Host-cms_login" || !r.Cookies()[0].Secure {
		t.Fatal("secure cookie contract")
	}
	f.e.Exec(`CREATE FUNCTION login_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private detail'; END $$`)
	f.e.Exec(`CREATE TRIGGER login_failure BEFORE INSERT ON oidc_logins FOR EACH ROW EXECUTE FUNCTION login_failure()`)
	r, data := f.request(t, "GET", "/auth/login", nil, "", "")
	if r.StatusCode != 503 || len(r.Cookies()) != 0 {
		t.Fatal(data)
	}
	f.e.Exec("DROP TRIGGER login_failure ON oidc_logins")
	f.e.Exec("ALTER TABLE oidc_logins RENAME TO broken_logins")
	r, data = f.request(t, "GET", "/auth/login", nil, "", "")
	if r.StatusCode != 503 {
		t.Fatal(data)
	}
}
func TestOIDCHumanDisplayNamesAndDuplicateCookies(t *testing.T) {
	f := setup(t)
	for _, fault := range []string{"blank-name", "long-name"} {
		f.provider.mu.Lock()
		f.provider.fault = fault
		f.provider.mu.Unlock()
		cookies, info := f.login(t, fault)
		if len([]rune(info.User.Name)) < 1 || len([]rune(info.User.Name)) > 200 {
			t.Fatal(info)
		}
		var session *http.Cookie
		for _, c := range cookies {
			if c.Name == "cms_session" {
				session = c
			}
		}
		_, data := f.request(t, "GET", "/auth/session", []*http.Cookie{session, session}, "", "")
		if data["authenticated"] != false {
			t.Fatal("duplicate cookie accepted")
		}
	}
	var n int
	if err := f.e.Pool.QueryRow(context.Background(), "SELECT count(*) FROM human_sessions").Scan(&n); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	var role store.Role
	f.e.Must(f.e.Admin, "create-role", map[string]any{"name": "empty", "capabilities": []string{}}, &role)
	if _, err := identity.New(context.Background(), f.e.Pool, identity.Config{ClientSecret: "secret"}); err == nil {
		t.Fatal("partial config accepted")
	}
}
