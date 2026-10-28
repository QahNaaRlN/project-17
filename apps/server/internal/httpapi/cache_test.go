package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/deliverycache"
	"github.com/qahnaarln/project-17/apps/server/internal/httpapi"
	"github.com/qahnaarln/project-17/apps/server/internal/jobs"
	"github.com/qahnaarln/project-17/apps/server/internal/ratelimit"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/redistest"
)

// invalidate runs the queued delivery_cache_invalidate jobs, as the River worker does.
func invalidate(t *testing.T, e env, cache *deliverycache.Cache) {
	t.Helper()
	ctx := context.Background()
	rows, err := e.pool.Query(ctx, "DELETE FROM river_job WHERE kind = 'delivery_cache_invalidate' RETURNING args")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var args jobs.InvalidateArgs
		if err := rows.Scan(&raw); err != nil || json.Unmarshal(raw, &args) != nil {
			t.Fatal(err)
		}
		if err := cache.Invalidate(ctx, args.Keys); err != nil {
			t.Fatal(err)
		}
	}
}

// fetch requests a path twice and checks the second answer comes from the cache unchanged.
func fetchTwice(t *testing.T, e env, path string, h map[string]string) {
	t.Helper()
	first, a := e.do(t, "GET", path, "", h)
	second, b := e.do(t, "GET", path, "", h)
	if first.StatusCode != 200 || first.Header.Get("X-CMS-Cache") != "miss" || second.Header.Get("X-CMS-Cache") != "hit" {
		t.Fatal(path, first.StatusCode, first.Header.Get("X-CMS-Cache"), second.Header.Get("X-CMS-Cache"), a)
	}
	for _, header := range []string{"ETag", "Cache-Control", "Surrogate-Key", "Content-Type"} {
		if first.Header.Get(header) != second.Header.Get(header) {
			t.Fatal(path, header, first.Header.Get(header), second.Header.Get(header))
		}
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal(path, a, b)
	}
}

// STU-523…527, 536: published responses come from Redis until a publication changes them.
func TestDeliveryOriginCache(t *testing.T) {
	cache := deliverycache.New(redistest.New(t))
	e := setup(t, func(d *httpapi.Deps) { d.Cache = cache })
	key, product := publishedSite(t, e)
	invalidate(t, e, cache)

	actor, err := auth.Authenticate(context.Background(), store.New(e.pool), "Bearer "+e.token)
	if err != nil {
		t.Fatal(err)
	}
	fixture := cmstest.Env{T: t, Pool: e.pool, Q: store.New(e.pool), Admin: actor}
	fixture.ActivateManifest("staging", []byte(strings.TrimSuffix(cmstest.DefaultManifest, "}")+`,"schemas":{"Article":{"version":1,"fields":{"title":{"type":"text"}}}}}`))
	entity := fixture.ContentFixture("entity", "Article", map[string]any{"title": "Published"}, "staging")
	asset := fixture.ContentFixture("asset", "", map[string]any{"assetKind": "image", "fileHash": "test", "title": "Picture"}, "staging")

	for _, path := range []string{"page?path=/products/shirt", "page?path=/products/shirt&locale=ru", "document/" + product, "routes", "entity/" + entity.String(), "asset/" + asset.String()} {
		fetchTwice(t, e, "/delivery/v1/store/staging/"+path, bearer(key))
	}
	// 304 from the cache.
	resp, _ := e.do(t, "GET", "/delivery/v1/store/staging/routes", "", bearer(key))
	h := bearer(key)
	h["If-None-Match"] = resp.Header.Get("ETag")
	if resp, body := e.do(t, "GET", "/delivery/v1/store/staging/routes", "", h); resp.StatusCode != http.StatusNotModified || body != nil || resp.Header.Get("X-CMS-Cache") != "hit" {
		t.Fatal(resp.StatusCode, body)
	}
	// Errors and drafts are never cached.
	for i := 0; i < 2; i++ {
		if resp, _ := e.do(t, "GET", "/delivery/v1/store/staging/page?path=/missing", "", bearer(key)); resp.StatusCode != 404 || resp.Header.Get("X-CMS-Cache") != "" {
			t.Fatal(resp.StatusCode, resp.Header)
		}
	}
	token := e.command(t, "create-preview-token", `{"environment":"staging"}`)["token"].(string)
	if resp, _ := e.do(t, "GET", "/delivery/v1/store/staging/routes", "", map[string]string{"Authorization": "Preview " + token}); resp.Header.Get("X-CMS-Cache") != "" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(resp.Header)
	}

	// A publication moves the route; once its job runs the cache answers with the new state.
	cs := e.command(t, "create-changeset", `{"title":"move"}`)["id"].(string)
	e.command(t, "apply-operations", `{"changesetId":"`+cs+`","expectedSeq":0,"operations":[{"type":"document.setRoute","target":"`+product+`","payload":{"path":"/items/:slug"}}]}`)
	e.command(t, "submit-changeset", `{"changesetId":"`+cs+`","expectedSeq":1}`)
	publication := e.command(t, "publish", `{"changesetId":"`+cs+`","environment":"staging"}`)["id"].(string)
	invalidate(t, e, cache)
	if resp, _ := e.do(t, "GET", "/delivery/v1/store/staging/page?path=/products/shirt", "", bearer(key)); resp.StatusCode != 404 {
		t.Fatal("stale page after publish", resp.StatusCode)
	}
	fetchTwice(t, e, "/delivery/v1/store/staging/page?path=/items/shirt", bearer(key))
	if _, body := e.do(t, "GET", "/delivery/v1/store/staging/routes", "", bearer(key)); !strings.Contains(routesOf(body), "/items/:slug") {
		t.Fatal("stale routes", body)
	}

	// Rollback restores the previous route.
	e.command(t, "rollback", `{"publicationId":"`+publication+`","resetHead":false}`)
	invalidate(t, e, cache)
	if resp, _ := e.do(t, "GET", "/delivery/v1/store/staging/page?path=/products/shirt", "", bearer(key)); resp.StatusCode != 200 || resp.Header.Get("X-CMS-Cache") != "miss" {
		t.Fatal("stale page after rollback", resp.StatusCode, resp.Header.Get("X-CMS-Cache"))
	}
}

func routesOf(body map[string]any) string {
	raw, _ := json.Marshal(body["items"])
	return string(raw)
}

// STU-522, 528: without a reachable Redis every answer is still correct.
// ST-001: PostgreSQL is the source of truth; Redis holds derived data.
func TestDeliveryWithoutRedis(t *testing.T) {
	cache := deliverycache.New(redistest.Down(t, slog.New(slog.NewTextHandler(io.Discard, nil))))
	e := setup(t, func(d *httpapi.Deps) { d.Cache = cache })
	key, _ := publishedSite(t, e)
	for i := 0; i < 2; i++ {
		resp, body := e.do(t, "GET", "/delivery/v1/store/staging/page?path=/products/shirt", "", bearer(key))
		if resp.StatusCode != 200 || resp.Header.Get("X-CMS-Cache") != "bypass" || body["page"] == nil {
			t.Fatal(resp.StatusCode, resp.Header, body)
		}
	}
}

// STU-530…532, 534: delivery keys and preview tokens have their own budgets.
func TestDeliveryRateLimits(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	limiter := ratelimit.New(nil).WithClock(func() time.Time { return now })
	e := setup(t, func(d *httpapi.Deps) {
		d.Limiter = limiter
		d.Limits = ratelimit.Limits{Delivery: 2, Preview: 1, ServiceCommands: 1000}
	})
	key, _ := publishedSite(t, e)
	other := e.command(t, "create-delivery-key", `{"environment":"staging","name":"other"}`)["key"].(string)
	for i := 0; i < 2; i++ {
		if resp, _ := e.do(t, "GET", "/delivery/v1/store/staging/routes", "", bearer(key)); resp.StatusCode != 200 {
			t.Fatal(i, resp.StatusCode)
		}
	}
	resp, body := e.do(t, "GET", "/delivery/v1/store/staging/routes", "", bearer(key))
	expectProblem(t, resp, body, 429, "RATE_LIMITED")
	if resp.Header.Get("Retry-After") != "1" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(resp.Header)
	}
	if resp, _ := e.do(t, "GET", "/delivery/v1/store/staging/routes", "", bearer(other)); resp.StatusCode != 200 {
		t.Fatal("keys are independent", resp.StatusCode)
	}
	token := e.command(t, "create-preview-token", `{"environment":"staging"}`)["token"].(string)
	preview := map[string]string{"Authorization": "Preview " + token}
	if resp, _ := e.do(t, "GET", "/delivery/v1/store/staging/routes", "", preview); resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	resp, body = e.do(t, "GET", "/delivery/v1/store/staging/routes", "", preview)
	expectProblem(t, resp, body, 429, "RATE_LIMITED")
	// Refused delivery requests are not journaled (volume).
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM security_events WHERE type = 'access.denied'`).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	now = now.Add(time.Second)
	if resp, _ := e.do(t, "GET", "/delivery/v1/store/staging/routes", "", bearer(key)); resp.StatusCode != 200 {
		t.Fatal("next window", resp.StatusCode)
	}
}

// STU-531, 532: service tokens are limited on commands only, and refusals are journaled.
func TestServiceCommandLimit(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	e := setup(t, func(d *httpapi.Deps) {
		d.Limiter = ratelimit.New(nil).WithClock(func() time.Time { return now })
		d.Limits = ratelimit.Limits{ServiceCommands: 1}
	})
	e.command(t, "create-changeset", `{"title":"one"}`)
	resp, body := e.do(t, "POST", "/api/v1/commands/create-changeset", `{"payload":{"title":"two"}}`, e.authed(map[string]string{"Idempotency-Key": "limit-two"}))
	expectProblem(t, resp, body, 429, "RATE_LIMITED")
	for i := 0; i < 3; i++ {
		if resp, _ := e.do(t, "GET", "/api/v1/environments", "", e.authed(nil)); resp.StatusCode != 200 {
			t.Fatal("queries are not limited for services", resp.StatusCode)
		}
	}
	var raw []byte
	if err := e.pool.QueryRow(context.Background(), `SELECT data FROM security_events WHERE type = 'access.denied'`).Scan(&raw); err != nil || !strings.Contains(string(raw), `"status": 429`) {
		t.Fatal(string(raw), err)
	}
}
