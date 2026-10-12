// Package identity implements browser-bound OIDC login and revocable human sessions.
package identity

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"golang.org/x/oauth2"
)

var ErrConfig = errors.New("OIDC_CONFIG_INVALID")
var ErrAccess = errors.New("PROJECT_ACCESS_REQUIRED")
var ErrCSRF = errors.New("CSRF_INVALID")
var ErrChanged = errors.New("SESSION_CHANGED")

type Config struct {
	Issuer, ClientID, ClientSecret, PublicOrigin string
	AllowLoopbackHTTP                            bool
}

func safeURL(raw string, loopback bool) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Scheme == "https" || (loopback && u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")))
}
func endpointURL(raw string, loopback bool) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	u.RawQuery = ""
	return safeURL(u.String(), loopback)
}

type transport struct{ loopback bool }

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) {
	if !endpointURL(r.URL.String(), t.loopback) {
		return nil, ErrConfig
	}
	return http.DefaultTransport.RoundTrip(r)
}
func (c Config) Validate() error {
	if c.Issuer == "" && c.ClientID == "" && c.ClientSecret == "" && c.PublicOrigin == "" && !c.AllowLoopbackHTTP {
		return nil
	}
	origin, err := url.Parse(c.PublicOrigin)
	if c.Issuer == "" || c.ClientID == "" || !safeURL(c.Issuer, c.AllowLoopbackHTTP) || !safeURL(c.PublicOrigin, c.AllowLoopbackHTTP) || err != nil || origin.Path != "" || origin.String() != c.PublicOrigin {
		return ErrConfig
	}
	return nil
}

type Service struct {
	Pool     *pgxpool.Pool
	Config   Config
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	client   *http.Client
}

func New(ctx context.Context, pool *pgxpool.Pool, c Config) (*Service, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.Issuer == "" {
		return nil, nil
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: transport{loopback: c.AllowLoopbackHTTP}}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, client), c.Issuer)
	if err != nil {
		return nil, errors.New("OIDC_DISCOVERY_FAILED")
	}
	endpoint := provider.Endpoint()
	if !endpointURL(endpoint.AuthURL, c.AllowLoopbackHTTP) || !endpointURL(endpoint.TokenURL, c.AllowLoopbackHTTP) {
		return nil, ErrConfig
	}
	return &Service{Pool: pool, Config: c, client: client, oauth: oauth2.Config{ClientID: c.ClientID, ClientSecret: c.ClientSecret, Endpoint: endpoint, RedirectURL: c.PublicOrigin + "/api/studio/auth/callback", Scopes: []string{oidc.ScopeOpenID, "profile"}}, verifier: provider.Verifier(&oidc.Config{ClientID: c.ClientID})}, nil
}
func (s *Service) cookieName(flow bool) string {
	name := "cms_session"
	if flow {
		name = "cms_login"
	}
	if strings.HasPrefix(s.Config.PublicOrigin, "https:") {
		name = "__Host-" + name
	}
	return name
}
func (s *Service) cookie(w http.ResponseWriter, value string, flow bool, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(flow), Value: value, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.Config.PublicOrigin, "https:"), SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}
func (s *Service) secret(r *http.Request, flow bool) string {
	var value string
	count := 0
	for _, c := range r.Cookies() {
		if c.Name == s.cookieName(flow) {
			count++
			value = c.Value
		}
	}
	if count != 1 || len(value) != 43 {
		return ""
	}
	return value
}

type Info struct {
	Authenticated bool         `json:"authenticated"`
	User          *User        `json:"user,omitempty"`
	CSRFToken     string       `json:"csrfToken,omitempty"`
	Rights        []auth.Right `json:"rights"`
	HasAccess     bool         `json:"hasAccess"`
	ExpiresAt     *time.Time   `json:"expiresAt,omitempty"`
	Actor         auth.Actor   `json:"-"`
}
type User struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

func (s *Service) Info(ctx context.Context, r *http.Request) (Info, error) {
	q := store.New(s.Pool)
	principal, err := q.HumanSessionPrincipal(ctx, auth.HashToken(s.secret(r, false)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Info{Rights: []auth.Right{}}, auth.ErrUnauthenticated
	}
	if err != nil {
		return Info{}, err
	}
	project, err := q.GetProjectBySlug(ctx, r.Header.Get("X-CMS-Project"))
	if errors.Is(err, pgx.ErrNoRows) {
		return Info{}, ErrAccess
	}
	if err != nil {
		return Info{}, err
	}
	caps, err := q.ActorCapabilities(ctx, store.ActorCapabilitiesParams{ProjectID: project.ID, ActorID: principal.ID})
	if err != nil {
		return Info{}, err
	}
	members, err := q.HumanRoleCount(ctx, store.HumanRoleCountParams{ProjectID: project.ID, ActorID: principal.ID})
	if err != nil {
		return Info{}, err
	}
	actor := auth.Actor{ID: principal.ID, Kind: auth.ActorHuman, DisplayName: principal.DisplayName, ProjectID: project.ID, ProjectSlug: project.Slug, Rights: auth.EffectiveRights(caps, []string{auth.ScopeAll})}
	return Info{Authenticated: true, User: &User{ID: principal.ID, Name: principal.DisplayName}, CSRFToken: principal.CsrfToken, Rights: actor.Rights.Sorted(), HasAccess: members > 0, ExpiresAt: &principal.ExpiresAt, Actor: actor}, nil
}
func (s *Service) CheckCSRF(r *http.Request, info Info) error {
	if r.Header.Get("Origin") != s.Config.PublicOrigin || info.CSRFToken == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(info.CSRFToken)) != 1 {
		return ErrCSRF
	}
	return nil
}
func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	switch {
	case r.URL.Path == "/auth/login" && r.Method == "GET":
		s.login(w, r)
	case r.URL.Path == "/auth/callback" && r.Method == "GET":
		s.callback(w, r)
	case r.URL.Path == "/auth/session" && r.Method == "GET":
		info, err := s.Info(r.Context(), r)
		if errors.Is(err, auth.ErrUnauthenticated) {
			reply(w, 200, Info{Rights: []auth.Right{}})
		} else if err != nil {
			reply(w, 503, map[string]string{"code": "SESSION_UNAVAILABLE"})
		} else {
			reply(w, 200, info)
		}
	case r.URL.Path == "/auth/logout" && r.Method == "POST":
		info, err := s.Info(r.Context(), r)
		if r.Header.Get("Origin") != s.Config.PublicOrigin || (err == nil && s.CheckCSRF(r, info) != nil) {
			reply(w, 403, map[string]string{"code": "CSRF_INVALID"})
			return
		}
		if err != nil && !errors.Is(err, auth.ErrUnauthenticated) {
			reply(w, 503, map[string]string{"code": "SESSION_UNAVAILABLE"})
			return
		}
		if err := store.New(s.Pool).DeleteHumanSession(r.Context(), auth.HashToken(s.secret(r, false))); err != nil {
			reply(w, 503, map[string]string{"code": "SESSION_UNAVAILABLE"})
			return
		}
		s.cookie(w, "", false, -1)
		s.cookie(w, "", true, -1)
		reply(w, 200, map[string]bool{"ok": true})
	default:
		reply(w, 404, map[string]string{"code": "NOT_FOUND"})
	}
}
func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	q := store.New(s.Pool)
	if _, err := q.GetProjectBySlug(r.Context(), r.Header.Get("X-CMS-Project")); err != nil {
		reply(w, 400, map[string]string{"code": "PROJECT_ACCESS_REQUIRED"})
		return
	}
	state, stateHash := auth.NewToken("")
	browser, browserHash := auth.NewToken("")
	nonce, _ := auth.NewToken("")
	verifier := oauth2.GenerateVerifier()
	if err := q.CleanupOIDCLogins(r.Context()); err != nil {
		reply(w, 503, map[string]string{"code": "SESSION_UNAVAILABLE"})
		return
	}
	if err := q.CreateOIDCLogin(r.Context(), store.CreateOIDCLoginParams{StateHash: stateHash, BrowserHash: browserHash, Verifier: verifier, Nonce: nonce, ProjectSlug: r.Header.Get("X-CMS-Project"), ExpiresAt: time.Now().Add(10 * time.Minute)}); err != nil {
		reply(w, 503, map[string]string{"code": "SESSION_UNAVAILABLE"})
		return
	}
	s.cookie(w, browser, true, 600)
	http.Redirect(w, r, s.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce)), http.StatusFound)
}
func (s *Service) callback(w http.ResponseWriter, r *http.Request) {
	fail := func() {
		http.Redirect(w, r, s.Config.PublicOrigin+"/editor?authError=OIDC_LOGIN_FAILED", http.StatusFound)
	}
	values := r.URL.Query()
	if len(values["state"]) != 1 || len(values["code"]) != 1 || len(values.Get("state")) != 43 || len(values.Get("code")) > 4096 || values.Get("code") == "" || values.Has("error") {
		fail()
		return
	}
	q := store.New(s.Pool)
	login, err := q.ConsumeOIDCLogin(r.Context(), store.ConsumeOIDCLoginParams{StateHash: auth.HashToken(values.Get("state")), BrowserHash: auth.HashToken(s.secret(r, true)), ProjectSlug: r.Header.Get("X-CMS-Project")})
	if err != nil || login.ProjectSlug != r.Header.Get("X-CMS-Project") {
		fail()
		return
	}
	s.cookie(w, "", true, -1)
	ctx := oidc.ClientContext(r.Context(), s.client)
	token, err := s.oauth.Exchange(ctx, values.Get("code"), oauth2.VerifierOption(login.Verifier))
	if err != nil {
		fail()
		return
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		fail()
		return
	}
	verified, err := s.verifier.Verify(ctx, raw)
	if err != nil || subtle.ConstantTimeCompare([]byte(verified.Nonce), []byte(login.Nonce)) != 1 || verified.Subject == "" || len(verified.Subject) > 255 {
		fail()
		return
	}
	var claims struct {
		Name string `json:"name"`
	}
	if err := verified.Claims(&claims); err != nil {
		fail()
		return
	}
	name := []rune(claims.Name)
	if len(name) == 0 {
		name = []rune("OIDC user")
	}
	if len(name) > 200 {
		name = name[:200]
	}
	secret, hash := auth.NewToken("")
	csrf, _ := auth.NewToken("")
	tx, err := s.Pool.Begin(r.Context())
	if err != nil {
		fail()
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	tq := store.New(tx)
	actor, err := tq.UpsertOIDCHuman(r.Context(), store.UpsertOIDCHumanParams{ID: uuid.Must(uuid.NewV7()), DisplayName: string(name), OidcIssuer: &verified.Issuer, OidcSubject: &verified.Subject})
	if err != nil {
		fail()
		return
	}
	if err := tq.CleanupHumanSessions(r.Context()); err != nil {
		fail()
		return
	}
	if err := tq.DeleteHumanSession(r.Context(), auth.HashToken(s.secret(r, false))); err != nil {
		fail()
		return
	}
	if err := tq.CreateHumanSession(r.Context(), store.CreateHumanSessionParams{TokenHash: hash, ActorID: actor.ID, CsrfToken: csrf, ExpiresAt: time.Now().Add(8 * time.Hour)}); err != nil {
		fail()
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		fail()
		return
	}
	s.cookie(w, secret, false, 8*3600)
	http.Redirect(w, r, s.Config.PublicOrigin+"/editor", http.StatusFound)
}
