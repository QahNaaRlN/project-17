package identity_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/identity"
)

// events returns the journal entries of a type: project, actor and data.
func events(t *testing.T, f *fixture, typ string) []map[string]any {
	t.Helper()
	rows, err := f.e.Pool.Query(context.Background(), `SELECT project_id, actor_id, data FROM security_events WHERE type = $1 ORDER BY created_at, id`, typ)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var project, actor *uuid.UUID
		var raw []byte
		if err := rows.Scan(&project, &actor, &raw); err != nil {
			t.Fatal(err)
		}
		data := map[string]any{}
		_ = json.Unmarshal(raw, &data)
		out = append(out, map[string]any{"project": project, "actor": actor, "data": data, "raw": string(raw)})
	}
	return out
}

// STU-505…507, 509: logins, refusals, roles and the journal query.
func TestSecurityJournal(t *testing.T) {
	f := setup(t)
	cookies, info := f.login(t, "alice")
	login := events(t, f, "auth.login")
	if len(login) != 1 || *login[0]["actor"].(*uuid.UUID) != info.Actor.ID || *login[0]["project"].(*uuid.UUID) != f.e.Admin.ProjectID ||
		strings.Contains(login[0]["raw"].(string), "alice") {
		t.Fatal(login)
	}
	if r, _ := f.request(t, "GET", "/auth/callback?state=x&code=y", nil, "", ""); r.StatusCode != 302 {
		t.Fatal(r.StatusCode)
	}
	if failed := events(t, f, "auth.login_failed"); len(failed) != 1 || failed[0]["data"].(map[string]any)["reason"] != "REQUEST_INVALID" {
		t.Fatal(failed)
	}

	// A person without rights is refused by the Query API and by the Command Bus.
	if r, _ := f.request(t, "GET", "/api/v1/security-events", cookies, "", ""); r.StatusCode != 403 {
		t.Fatal(r.StatusCode)
	}
	if r, _ := f.request(t, "POST", "/api/v1/commands/create-role", cookies, info.CSRFToken, `{"payload":{"name":"x","capabilities":[]}}`); r.StatusCode != 403 {
		t.Fatal(r.StatusCode)
	}
	denied := events(t, f, "access.denied")
	if len(denied) != 2 || denied[0]["data"].(map[string]any)["route"] != "/api/v1/security-events" || denied[1]["data"].(map[string]any)["route"] != "/api/v1/commands/create-role" ||
		*denied[0]["project"].(*uuid.UUID) != f.e.Admin.ProjectID {
		t.Fatal(denied)
	}
	for _, d := range denied {
		if *d["actor"].(*uuid.UUID) != info.Actor.ID {
			t.Fatal(d)
		}
	}

	f.grant(t, info.Actor.ID, "admins", auth.ProjectAdmin)
	f.e.Must(f.e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 1, "high": 1}, nil)
	if changed := events(t, f, "policy.changed"); len(changed) != 1 || changed[0]["data"].(map[string]any)["command"] != "set-approval-policy" ||
		len(changed[0]["data"].(map[string]any)["payloadSha256"].(string)) != 64 {
		t.Fatal(changed)
	}
	if tokens := events(t, f, "token.created"); len(tokens) != 1 || strings.Contains(tokens[0]["raw"].(string), "cms_svc_") {
		t.Fatal(tokens)
	}
	if len(events(t, f, "role.created")) != 1 || len(events(t, f, "role.bound")) != 1 {
		t.Fatal("role events")
	}
	r, page := f.request(t, "GET", "/api/v1/security-events?limit=2", cookies, "", "")
	if r.StatusCode != 200 || len(page["items"].([]any)) != 2 || page["nextCursor"] == nil {
		t.Fatal(r.StatusCode, page)
	}
	r, next := f.request(t, "GET", "/api/v1/security-events?limit=100&after="+page["nextCursor"].(string), cookies, "", "")
	if r.StatusCode != 200 || len(next["items"].([]any)) == 0 || next["nextCursor"] != nil {
		t.Fatal(r.StatusCode, next)
	}
	r, typed := f.request(t, "GET", "/api/v1/security-events?type=role.bound", cookies, "", "")
	if r.StatusCode != 200 || len(typed["items"].([]any)) != 1 {
		t.Fatal(typed)
	}
	for _, bad := range []string{"limit=0", "limit=x", "after=%25", "after=" + strings.Repeat("A", 8), "type=" + strings.Repeat("t", 65), "other=1"} {
		if r, _ := f.request(t, "GET", "/api/v1/security-events?"+bad, cookies, "", ""); r.StatusCode != 400 {
			t.Fatal(bad, r.StatusCode)
		}
	}
	if r, _ := f.request(t, "POST", "/auth/logout", cookies, info.CSRFToken, ""); r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	if out := events(t, f, "auth.logout"); len(out) != 1 {
		t.Fatal(out)
	}
}

// SEC-008, STU-510: anonymisation clears identity, revokes access, keeps history.
func TestAnonymizeUser(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	cookies, info := f.login(t, "bob")
	f.grant(t, info.Actor.ID, "editors", auth.ContentRead)
	issuer := f.provider.srv.URL
	if _, _, err := identity.Anonymize(ctx, f.e.Pool, issuer, "nobody"); !errors.Is(err, identity.ErrUserNotFound) {
		t.Fatal(err)
	}
	id, changed, err := identity.Anonymize(ctx, f.e.Pool, issuer, "bob")
	if err != nil || !changed || id != info.Actor.ID {
		t.Fatal(id, changed, err)
	}
	var name string
	var email, subject *string
	var disabled bool
	var bindings, sessions int
	if err := f.e.Pool.QueryRow(ctx, `SELECT display_name, email, oidc_subject, disabled_at IS NOT NULL,
		(SELECT count(*) FROM role_bindings WHERE actor_id = $1), (SELECT count(*) FROM human_sessions WHERE actor_id = $1)
		FROM actors WHERE id = $1`, id).Scan(&name, &email, &subject, &disabled, &bindings, &sessions); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "Удалённый пользователь") || subject != nil || !disabled || bindings != 0 || sessions != 0 ||
		email == nil || strings.Contains(*email, "bob") {
		t.Fatal(name, email, subject, disabled, bindings, sessions)
	}
	if r, data := f.request(t, "GET", "/auth/session", cookies, "", ""); r.StatusCode != 200 || data["authenticated"] == true {
		t.Fatal(data)
	}
	if again, changed, err := identity.Anonymize(ctx, f.e.Pool, issuer, "bob"); err != nil || changed || again != id {
		t.Fatal(again, changed, err)
	}
	if out := events(t, f, "user.anonymized"); len(out) != 1 || out[0]["project"].(*uuid.UUID) != nil {
		t.Fatal(out)
	}
}
