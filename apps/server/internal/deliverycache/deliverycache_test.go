package deliverycache_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/deliverycache"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/redistest"
)

func entry(keys ...string) deliverycache.Entry {
	return deliverycache.Entry{Body: []byte(`{"ok":true}`), ETag: `"e"`, SurrogateKeys: keys}
}

// STU-523, 525, 526: entries live until a tag they depend on changes.
func TestCache(t *testing.T) {
	ctx := context.Background()
	c := deliverycache.New(redistest.New(t))
	page := deliverycache.Key("store", "staging", "page", "/a", "ru")
	doc := deliverycache.Key("store", "staging", "document", "1", "")
	if c.Get(ctx, page) != nil {
		t.Fatal("empty cache")
	}
	pre, ok := c.Versions(ctx, []string{"store:staging:routes"})
	if !ok || !c.Put(ctx, page, entry("store:staging:obj", "store:staging:routes"), pre) || !c.Put(ctx, doc, entry("store:staging:doc"), nil) {
		t.Fatal("put")
	}
	if e := c.Get(ctx, page); e == nil || string(e.Body) != `{"ok":true}` || e.ETag != `"e"` || len(e.SurrogateKeys) != 2 {
		t.Fatal(e)
	}
	if err := c.Invalidate(ctx, []string{"store:staging:obj"}); err != nil {
		t.Fatal(err)
	}
	if c.Get(ctx, page) != nil || c.Get(ctx, doc) == nil {
		t.Fatal("only dependent entries go stale")
	}
	// Another environment is not touched.
	prod := deliverycache.Key("store", "production", "document", "1", "")
	c.Put(ctx, prod, entry("store:production:doc"), nil)
	_ = c.Invalidate(ctx, []string{"store:staging:doc"})
	if c.Get(ctx, prod) == nil || c.Get(ctx, doc) != nil {
		t.Fatal("environments are isolated")
	}
}

// STU-525: a response read before a publication and stored after its invalidation is never served.
func TestCacheRaceWithPublication(t *testing.T) {
	ctx := context.Background()
	c := deliverycache.New(redistest.New(t))
	key := deliverycache.Key("store", "staging", "page", "/a", "")
	pre, _ := c.Versions(ctx, []string{"store:staging:routes"})
	// ...the database is read here, then a publication commits and its job runs...
	if err := c.Invalidate(ctx, []string{"store:staging:routes", "store:staging:obj"}); err != nil {
		t.Fatal(err)
	}
	if !c.Put(ctx, key, entry("store:staging:obj", "store:staging:routes"), pre) {
		t.Fatal("put")
	}
	if c.Get(ctx, key) != nil {
		t.Fatal("stale response served")
	}
}

// STU-523, 529: keys never collide, large and malformed entries are not served.
func TestCacheLimits(t *testing.T) {
	ctx := context.Background()
	r := redistest.New(t)
	c := deliverycache.New(r)
	if deliverycache.Key("p", "e", "page", "/a:b", "") == deliverycache.Key("p", "e", "page", "/a", "b") {
		t.Fatal("keys collide")
	}
	big := entry()
	big.Body = bytes.Repeat([]byte("x"), deliverycache.MaxEntry+1)
	if c.Put(ctx, "k", big, nil) {
		t.Fatal("large entry stored")
	}
	r.R.Set(ctx, "broken", "{", 0)
	if c.Get(ctx, "broken") != nil {
		t.Fatal("malformed entry")
	}
	if deliverycache.New(nil) != nil {
		t.Fatal("no Redis — no cache")
	}
	var none *deliverycache.Cache
	if none.Invalidate(ctx, []string{"x"}) != nil || c.Invalidate(ctx, nil) != nil {
		t.Fatal("no-op invalidation")
	}
}

// STU-528: Redis failures turn into misses and retried invalidations, logged once per outage.
func TestCacheWithoutRedis(t *testing.T) {
	ctx := context.Background()
	var logs bytes.Buffer
	c := deliverycache.New(redistest.Down(t, slog.New(slog.NewTextHandler(&logs, nil))))
	if c.Get(ctx, "k") != nil {
		t.Fatal("get")
	}
	if _, ok := c.Versions(ctx, []string{"t"}); ok {
		t.Fatal("versions")
	}
	if c.Put(ctx, "k", entry("t"), nil) {
		t.Fatal("put")
	}
	if c.Put(ctx, "k", entry(), nil) {
		t.Fatal("set")
	}
	if c.Invalidate(ctx, []string{"t"}) == nil {
		t.Fatal("invalidate must fail so the job retries")
	}
	if strings.Count(logs.String(), "redis недоступен") != 1 {
		t.Fatal(logs.String())
	}
}
