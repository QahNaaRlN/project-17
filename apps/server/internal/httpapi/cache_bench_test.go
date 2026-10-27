package httpapi_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/deliverycache"
	"github.com/qahnaarln/project-17/apps/server/internal/httpapi"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/redistest"
)

// BenchmarkDeliveryPageCacheHit — a published /page answered from Redis through the full
// router: delivery key check in PostgreSQL, entry GET and tag MGET (STU-538, a guide for
// NFR-002 before the load tests; no threshold applies).
func BenchmarkDeliveryPageCacheHit(b *testing.B) {
	ctx := context.Background()
	pool := pgtest.NewDB(b)
	res, err := projects.Bootstrap(ctx, pool, "store", "Магазин", time.Hour)
	if err != nil {
		b.Fatal(err)
	}
	q := store.New(pool)
	env, err := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: res.ProjectID, Name: "staging"})
	if err != nil {
		b.Fatal(err)
	}
	actor, err := auth.Authenticate(ctx, q, "Bearer "+res.Token)
	if err != nil {
		b.Fatal(err)
	}
	secret, hash := auth.NewToken(auth.DeliveryKeyPrefix)
	if _, err := q.CreateDeliveryKey(ctx, store.CreateDeliveryKeyParams{ID: uuid.Must(uuid.NewV7()), ProjectID: res.ProjectID,
		EnvironmentID: env.ID, Name: "bench", TokenHash: hash, CreatedBy: actor.ID}); err != nil {
		b.Fatal(err)
	}
	cache := deliverycache.New(redistest.New(b))
	keys := []string{"store:staging:page", "store:staging:routes"}
	pre, _ := cache.Versions(ctx, keys)
	if !cache.Put(ctx, deliverycache.Key("store", "staging", "page", "/", ""), deliverycache.Entry{Body: []byte(`{"page":{}}`), ETag: `"bench"`, SurrogateKeys: keys}, pre) {
		b.Fatal("put")
	}
	srv := httptest.NewServer(httpapi.NewRouter(httpapi.Deps{Pool: pool, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Cache: cache}))
	b.Cleanup(srv.Close)
	req, _ := http.NewRequest("GET", srv.URL+"/delivery/v1/store/staging/page?path=/", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	b.ResetTimer()
	for b.Loop() {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.Header.Get("X-CMS-Cache") != "hit" {
			b.Fatal(resp.StatusCode, resp.Header)
		}
	}
}
