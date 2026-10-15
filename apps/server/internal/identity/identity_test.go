package identity_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/delivery"
	"github.com/qahnaarln/project-17/apps/server/internal/httpapi"
	"github.com/qahnaarln/project-17/apps/server/internal/identity"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
	"golang.org/x/oauth2"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

type codeGrant struct{ nonce, challenge, redirect, subject, fault string }
type provider struct {
	srv            *httptest.Server
	mu             sync.Mutex
	key            *rsa.PrivateKey
	codes          map[string]codeGrant
	subject, fault string
}

func fakeProvider(t *testing.T) *provider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &provider{key: key, codes: map[string]codeGrant{}, subject: "alice"}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": p.srv.URL, "authorization_endpoint": p.srv.URL + "/authorize", "token_endpoint": p.srv.URL + "/token", "jwks_uri": p.srv.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			json.NewEncoder(w).Encode(map[string]any{"keys": []jose.JSONWebKey{{Key: &p.key.PublicKey, KeyID: "fixture", Algorithm: "RS256", Use: "sig"}}})
		case "/authorize":
			q := r.URL.Query()
			if q.Get("client_id") != "studio" || q.Get("code_challenge_method") != "S256" || q.Get("nonce") == "" || q.Get("response_type") != "code" {
				http.Error(w, "invalid request", 400)
				return
			}
			code, _ := auth.NewToken("")
			p.mu.Lock()
			p.codes[code] = codeGrant{q.Get("nonce"), q.Get("code_challenge"), q.Get("redirect_uri"), p.subject, p.fault}
			p.mu.Unlock()
			u, _ := url.Parse(q.Get("redirect_uri"))
			values := u.Query()
			values.Set("state", q.Get("state"))
			values.Set("code", code)
			u.RawQuery = values.Encode()
			http.Redirect(w, r, u.String(), 302)
		case "/token":
			r.ParseForm()
			p.mu.Lock()
			g, ok := p.codes[r.Form.Get("code")]
			delete(p.codes, r.Form.Get("code"))
			p.mu.Unlock()
			if !ok || oauth2.S256ChallengeFromVerifier(r.Form.Get("code_verifier")) != g.challenge || r.Form.Get("redirect_uri") != g.redirect {
				http.Error(w, "invalid grant", 400)
				return
			}
			if g.fault == "exchange" {
				http.Error(w, "private provider detail", 500)
				return
			}
			claims := map[string]any{"iss": p.srv.URL, "sub": g.subject, "aud": "studio", "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(), "nonce": g.nonce, "name": g.subject, "email": "shared@example.test"}
			switch g.fault {
			case "nonce":
				claims["nonce"] = "wrong"
			case "issuer":
				claims["iss"] = "https://other.invalid"
			case "audience":
				claims["aud"] = "other"
			case "expired":
				claims["exp"] = time.Now().Add(-time.Hour).Unix()
			case "subject":
				claims["sub"] = ""
			case "claims":
				claims["name"] = 4
			case "blank-name":
				claims["name"] = ""
			case "long-name":
				claims["name"] = strings.Repeat("a", 250)
			}
			signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: p.key}, (&jose.SignerOptions{}).WithHeader("kid", "fixture"))
			token, _ := jwt.Signed(signer).Claims(claims).Serialize()
			if g.fault == "signature" {
				token = strings.TrimSuffix(token, token[len(token)-10:]) + "aaaaaaaaaa"
			}
			result := map[string]any{"access_token": "fixture-access", "token_type": "Bearer", "id_token": token}
			if g.fault == "missing" {
				delete(result, "id_token")
			}
			json.NewEncoder(w).Encode(result)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(p.srv.Close)
	return p
}

type fixture struct {
	e        *cmstest.Env
	provider *provider
	service  *identity.Service
	server   *httptest.Server
	client   *http.Client
}

func setup(t *testing.T) *fixture {
	e := cmstest.New(t, identity.Register, workflow.Register, publishing.Register, delivery.Register)
	p := fakeProvider(t)
	s, err := identity.New(context.Background(), e.Pool, identity.Config{Issuer: p.srv.URL, ClientID: "studio", ClientSecret: "fixture-secret", PublicOrigin: "http://127.0.0.1:18085", AllowLoopbackHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.NewRouter(httpapi.Deps{Pool: e.Pool, Bus: e.Bus, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Identity: s}))
	t.Cleanup(server.Close)
	return &fixture{e, p, s, server, &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (f *fixture) request(t *testing.T, method, path string, cookies []*http.Cookie, csrf string, body string) (*http.Response, map[string]any) {
	t.Helper()
	r, _ := http.NewRequest(method, f.server.URL+path, strings.NewReader(body))
	r.Header.Set("X-CMS-Project", "store")
	r.Header.Set("Origin", f.service.Config.PublicOrigin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", uuid.NewString())
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	response, err := f.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data := map[string]any{}
	_ = json.NewDecoder(response.Body).Decode(&data)
	return response, data
}
func (f *fixture) begin(t *testing.T) (string, []*http.Cookie) {
	t.Helper()
	r, _ := f.request(t, "GET", "/auth/login", nil, "", "")
	if r.StatusCode != 302 {
		t.Fatal(r.StatusCode)
	}
	cookies := r.Cookies()
	auth, err := f.client.Get(r.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	auth.Body.Close()
	callback, _ := url.Parse(auth.Header.Get("Location"))
	return "/auth/callback?" + callback.RawQuery, cookies
}
func (f *fixture) login(t *testing.T, subject string) ([]*http.Cookie, identity.Info) {
	t.Helper()
	f.provider.mu.Lock()
	f.provider.subject = subject
	f.provider.mu.Unlock()
	callback, cookies := f.begin(t)
	response, _ := f.request(t, "GET", callback, cookies, "", "")
	if response.StatusCode != 302 || strings.Contains(response.Header.Get("Location"), "authError") {
		t.Fatal("login failed", response.StatusCode, response.Header.Get("Location"))
	}
	cookies = response.Cookies()
	req, _ := http.NewRequest("GET", f.server.URL+"/auth/session", nil)
	req.Header.Set("X-CMS-Project", "store")
	for _, c := range cookies {
		if c.MaxAge > 0 {
			req.AddCookie(c)
		}
	}
	info, err := f.service.Info(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return cookies, info
}
func (f *fixture) grant(t *testing.T, user uuid.UUID, name string, rights ...auth.Right) uuid.UUID {
	t.Helper()
	var role store.Role
	f.e.Must(f.e.Admin, "create-role", map[string]any{"name": name, "capabilities": rights}, &role)
	f.e.Must(f.e.Admin, "bind-human-role", map[string]any{"actorId": user, "roleId": role.ID}, nil)
	return role.ID
}

// STU-080/081/082/084: signed OIDC creates a stable, unprivileged human; sessions are revocable.
func TestOIDCSessionLifecycle(t *testing.T) {
	f := setup(t)
	cookies, info := f.login(t, "alice")
	if info.HasAccess || len(info.Rights) != 0 || info.Actor.Kind != auth.ActorHuman {
		t.Fatal(info)
	}
	response, data := f.request(t, "GET", "/api/v1/pages?environment=staging", cookies, "", "")
	if response.StatusCode != 403 || data["code"] != "PROJECT_ACCESS_REQUIRED" {
		t.Fatal(data)
	}
	f.grant(t, info.User.ID, "editor", auth.DesignRead, auth.ContentRead, auth.DesignCompose, auth.ContentWrite)
	response, data = f.request(t, "GET", "/auth/session", cookies, "", "")
	if response.StatusCode != 200 || data["hasAccess"] != true {
		t.Fatal(data)
	}
	for _, c := range cookies {
		if c.MaxAge > 0 && (!c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Domain != "" || c.Path != "/") {
			t.Fatal("cookie flags")
		}
	}
	second, again := f.login(t, "alice")
	if again.User.ID != info.User.ID {
		t.Fatal("subject binding changed")
	}
	_, other := f.login(t, "bob")
	if other.User.ID == info.User.ID || other.HasAccess {
		t.Fatal("email linked identities")
	}
	response, data = f.request(t, "POST", "/auth/logout", second, "wrong", "")
	if response.StatusCode != 403 {
		t.Fatal(data)
	}
	response, data = f.request(t, "POST", "/auth/logout", second, again.CSRFToken, "")
	if response.StatusCode != 200 {
		t.Fatal(data)
	}
	_, data = f.request(t, "GET", "/auth/session", second, "", "")
	if data["authenticated"] != false {
		t.Fatal(data)
	}
	f.e.Exec("UPDATE actors SET disabled_at=now() WHERE id=$1", info.User.ID)
	_, data = f.request(t, "GET", "/auth/session", cookies, "", "")
	if data["authenticated"] != false {
		t.Fatal(data)
	}
}

// STU-080: state/browser binding and signed ID-token constraints cannot be bypassed.
func TestOIDCRejectsReplayAndInvalidTokens(t *testing.T) {
	f := setup(t)
	callback, cookies := f.begin(t)
	wrong := []*http.Cookie{{Name: "cms_login", Value: strings.Repeat("x", 43)}}
	response, _ := f.request(t, "GET", callback, wrong, "", "")
	if !strings.Contains(response.Header.Get("Location"), "authError") {
		t.Fatal("browser binding bypass")
	}
	response, _ = f.request(t, "GET", callback, cookies, "", "")
	if strings.Contains(response.Header.Get("Location"), "authError") {
		t.Fatal("correct browser rejected")
	}
	response, _ = f.request(t, "GET", callback, cookies, "", "")
	if !strings.Contains(response.Header.Get("Location"), "authError") {
		t.Fatal("replay accepted")
	}
	for _, fault := range []string{"nonce", "issuer", "audience", "expired", "subject", "signature", "missing", "exchange", "claims"} {
		t.Run(fault, func(t *testing.T) {
			f.provider.mu.Lock()
			f.provider.fault = fault
			f.provider.mu.Unlock()
			callback, cookies := f.begin(t)
			response, _ := f.request(t, "GET", callback, cookies, "", "")
			if !strings.Contains(response.Header.Get("Location"), "authError") {
				t.Fatal("invalid token accepted")
			}
			for _, c := range response.Cookies() {
				if c.Name == "cms_session" && c.MaxAge > 0 {
					t.Fatal("session minted")
				}
			}
		})
	}
	for _, path := range []string{"/auth/callback", "/auth/callback?state=bad&code=bad", "/auth/callback?state=" + strings.Repeat("a", 43) + "&code=x&error=denied"} {
		response, _ := f.request(t, "GET", path, cookies, "", "")
		if !strings.Contains(response.Header.Get("Location"), "authError") {
			t.Fatal(path)
		}
	}
}

// STU-081/083/084: real cookie-authenticated author and reviewer use the existing Command Bus.
func TestTwoOIDCHumansReviewAndPublish(t *testing.T) {
	f := setup(t)
	author, a := f.login(t, "alice")
	reviewer, b := f.login(t, "bob")
	f.grant(t, a.User.ID, "writer", auth.DesignRead, auth.ContentRead, auth.DesignCompose, auth.ContentWrite, auth.ContentPublish)
	reviewerRole := f.grant(t, b.User.ID, "reviewer", auth.DesignRead, auth.ContentRead, auth.ContentPublish)
	command := func(c []*http.Cookie, csrf, name string, payload any) (int, map[string]any) {
		raw, _ := json.Marshal(map[string]any{"payload": payload, "reason": "identity integration"})
		r, d := f.request(t, "POST", "/api/v1/commands/"+name, c, csrf, string(raw))
		return r.StatusCode, d
	}
	status, data := command(author, "", "create-changeset", map[string]any{"title": "human work"})
	if status != 403 || data["code"] != "CSRF_INVALID" {
		t.Fatal(data)
	}
	status, data = command(author, a.CSRFToken, "create-changeset", map[string]any{"title": "human work", "targets": []string{"staging"}})
	if status != 200 {
		t.Fatal(data)
	}
	cs := data["result"].(map[string]any)["id"].(string)
	status, data = command(author, a.CSRFToken, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": 0, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": "/human", "root": map[string]any{"id": "n_root", "type": "Box"}}}}})
	if status != 200 {
		t.Fatal(data)
	}
	status, data = command(author, a.CSRFToken, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1})
	if status != 200 {
		t.Fatal(data)
	}
	status, data = command(author, a.CSRFToken, "approve-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1})
	if status != 403 || data["code"] != "APPROVAL_SELF" {
		t.Fatal(data)
	}
	_, data = f.request(t, "GET", "/api/v1/work-items?environment=staging", reviewer, "", "")
	items := data["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != cs || items[0].(map[string]any)["own"] != false {
		t.Fatal(data)
	}
	doc := items[0].(map[string]any)["documentId"].(string)
	r, data := f.request(t, "GET", "/api/v1/documents/"+doc+"?changesetId="+cs, reviewer, "", "")
	if r.StatusCode != 200 {
		t.Fatal(data)
	}
	status, data = command(author, a.CSRFToken, "reopen-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1})
	if status != 200 {
		t.Fatal(data)
	}
	r, data = f.request(t, "GET", "/api/v1/changesets/"+cs+"/review", reviewer, "", "")
	if r.StatusCode != 404 || data["code"] != "NOT_FOUND" {
		t.Fatal("foreign reopened work must be hidden", data)
	}
	_, data = f.request(t, "GET", "/api/v1/work-items?environment=staging", reviewer, "", "")
	if len(data["items"].([]any)) != 0 {
		t.Fatal("foreign open work in reviewer queue", data)
	}
	status, data = command(author, a.CSRFToken, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1})
	if status != 200 {
		t.Fatal(data)
	}
	status, data = command(reviewer, b.CSRFToken, "approve-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1})
	if status != 200 || data["result"].(map[string]any)["changeset"].(map[string]any)["state"] != "approved" {
		t.Fatal(data)
	}
	status, data = command(author, a.CSRFToken, "publish", map[string]any{"changesetId": cs, "expectedSeq": 1, "environment": "staging"})
	if status != 200 {
		t.Fatal(data)
	}
	r, data = f.request(t, "GET", "/api/v1/documents/"+doc+"?environment=staging", reviewer, "", "")
	if r.StatusCode != 200 || data["state"] != "committed" {
		t.Fatal(data)
	}
	f.e.Must(f.e.Admin, "unbind-human-role", map[string]any{"actorId": b.User.ID, "roleId": reviewerRole}, nil)
	r, data = f.request(t, "GET", "/api/v1/work-items?environment=staging", reviewer, "", "")
	if r.StatusCode != 403 || data["code"] != "PROJECT_ACCESS_REQUIRED" {
		t.Fatal(data)
	}
}
func TestIdentityConfiguration(t *testing.T) {
	if s, err := identity.New(context.Background(), nil, identity.Config{}); s != nil || err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []identity.Config{{Issuer: "http://remote.invalid", ClientID: "x", PublicOrigin: "https://studio.test"}, {Issuer: "https://issuer.test", ClientID: "x", PublicOrigin: "http://studio.test", AllowLoopbackHTTP: true}, {Issuer: "https://issuer.test", ClientID: "x", PublicOrigin: "https://studio.test/path"}, {ClientSecret: "secret"}} {
		if !errors.Is(cfg.Validate(), identity.ErrConfig) {
			t.Fatal("unsafe config accepted")
		}
	}
	if err := (identity.Config{Issuer: "https://issuer.test/realm", ClientID: "x", PublicOrigin: "https://studio.test"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.New(context.Background(), nil, identity.Config{Issuer: "http://127.0.0.1:1", ClientID: "x", PublicOrigin: "http://127.0.0.1:18085", AllowLoopbackHTTP: true}); err == nil {
		t.Fatal("discovery should fail")
	}
}

// STU-081/082: the cookie actor must still match the editor after its preflight.
func TestExpectedActorAndOriginBoundary(t *testing.T) {
	f := setup(t)
	cookies, info := f.login(t, "bob")
	f.grant(t, info.User.ID, "reader", auth.DesignRead, auth.ContentRead)
	for _, method := range []string{"GET", "POST"} {
		path := "/api/v1/pages?environment=staging"
		if method == "POST" {
			path = "/api/v1/commands/create-changeset"
		}
		r, _ := http.NewRequest(method, f.server.URL+path, strings.NewReader(`{"payload":{"title":"must not run"}}`))
		r.Header.Set("X-CMS-Project", "store")
		r.Header.Set("X-CMS-Actor", uuid.NewString())
		r.Header.Set("Origin", f.service.Config.PublicOrigin)
		r.Header.Set("X-CSRF-Token", info.CSRFToken)
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		response, err := f.client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		var data map[string]any
		json.NewDecoder(response.Body).Decode(&data)
		response.Body.Close()
		if response.StatusCode != 403 || data["code"] != "SESSION_CHANGED" {
			t.Fatal(data)
		}
	}
	r, _ := http.NewRequest("POST", f.server.URL+"/auth/logout", nil)
	r.Header.Set("X-CMS-Project", "store")
	r.Header.Set("Origin", "https://other.test")
	r.Header.Set("X-CSRF-Token", info.CSRFToken)
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	response, err := f.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("cross-origin logout accepted")
	}
	_, data := f.request(t, "GET", "/auth/session", cookies, "", "")
	if data["authenticated"] != true {
		t.Fatal("cross-origin revoked session")
	}
}

// STU-080/081: login rotates the presented session; database expiry cannot be bypassed.
func TestSessionRotationAndExpiry(t *testing.T) {
	f := setup(t)
	old, _ := f.login(t, "alice")
	callback, flow := f.begin(t)
	for _, cookie := range old {
		if cookie.MaxAge > 0 {
			flow = append(flow, cookie)
		}
	}
	response, _ := f.request(t, "GET", callback, flow, "", "")
	if strings.Contains(response.Header.Get("Location"), "authError") {
		t.Fatal("rotation failed")
	}
	fresh := response.Cookies()
	_, data := f.request(t, "GET", "/auth/session", old, "", "")
	if data["authenticated"] != false {
		t.Fatal("old session retained")
	}
	_, data = f.request(t, "GET", "/auth/session", fresh, "", "")
	if data["authenticated"] != true {
		t.Fatal("new session missing")
	}
	f.e.Exec("UPDATE human_sessions SET expires_at=now()-interval '1 second'")
	_, data = f.request(t, "GET", "/auth/session", fresh, "", "")
	if data["authenticated"] != false {
		t.Fatal("expired session accepted")
	}
}
