package imagedelivery

import (
	"bytes"
	"context"

	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/delivery"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
)

func TestMain(m *testing.M) { pgtest.Main(m) }
func config(proxy string) Config {
	return Config{PublicURL: "https://images.example.test", ProxyURL: proxy, Bucket: "assets", Key: strings.Repeat("11", 32), ProxyKey: strings.Repeat("22", 32), ProxySalt: strings.Repeat("33", 16)}
}

type fixture struct {
	e     *cmstest.Env
	s     *Service
	a     delivery.Access
	id    uuid.UUID
	count atomic.Int32
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{e: cmstest.New(t, delivery.Register)}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.count.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("credential forwarded")
		}
		w.Header().Set("Content-Type", "image/"+r.URL.Path[strings.LastIndex(r.URL.Path, ".")+1:])
		_, _ = w.Write([]byte("transformed"))
	}))
	t.Cleanup(upstream.Close)
	var err error
	f.s, err = New(config(upstream.URL))
	if err != nil {
		t.Fatal(err)
	}
	f.s.Pool = f.e.Pool
	f.s.Now = func() time.Time { return time.Unix(1800000000, 0) }
	env, err := f.e.Q.GetEnvironmentPreviewKey(context.Background(), store.GetEnvironmentPreviewKeyParams{Slug: "store", Name: "staging"})
	if err != nil {
		t.Fatal(err)
	}
	f.a = delivery.Access{ProjectID: env.ProjectID, ProjectSlug: "store", EnvironmentID: env.ID, Environment: "staging"}
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 320, 200)))
	hash := fingerprint(b.Bytes())
	f.e.Exec(`INSERT INTO asset_files(project_id,sha256,storage_key,mime_type,size_bytes,width,height,status) VALUES($1,decode($2,'hex'),$3,'image/png',$4,320,200,'ready')`, f.a.ProjectID, hash, "projects/"+f.a.ProjectID.String()+"/assets/"+hash, b.Len())
	f.id = f.e.ContentFixture("asset", "", map[string]any{"fileHash": hash, "mimeType": "image/png", "size": b.Len(), "assetKind": "image", "managedFile": true, "width": 320, "height": 200, "focalPoint": map[string]any{"x": .2, "y": .8}}, "staging")
	return f
}
func (f *fixture) mint(t *testing.T, o Options) Model {
	t.Helper()
	m, e := f.s.Mint(context.Background(), f.a, f.id, o)
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func parse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, e := url.Parse(raw)
	if e != nil {
		t.Fatal(e)
	}
	return u
}
func expect(t *testing.T, e error, code string) {
	t.Helper()
	var p *commandbus.Error
	if !errors.As(e, &p) || p.Code != code {
		t.Fatalf("want %s, got %v", code, e)
	}
}
func resign(s *Service, u *url.URL) {
	q := u.Query()
	q.Del("sig")
	q.Set("sig", digest(s.key, nil, u.EscapedPath()+"?"+q.Encode()))
	u.RawQuery = q.Encode()
}
func TestCNT050051VariantsAndSigning(t *testing.T) {
	f := setup(t)
	for _, format := range []string{"avif", "webp", "jpeg", "png"} {
		for _, fit := range []string{"contain", "cover"} {
			o := Options{Width: 640, Fit: fit, Format: format}
			if fit == "cover" {
				o.Height = 480
			}
			m := f.mint(t, o)
			if len(m.Variants) != 9 || m.FocalPoint != (Point{.2, .8}) {
				t.Fatal(m)
			}
			for i, v := range m.Variants {
				if v.Width != Widths[i] {
					t.Fatal(v)
				}
				im, e := f.s.Fetch(context.Background(), parse(t, v.URL))
				if e != nil {
					t.Fatal(e)
				}
				if string(im.Body) != "transformed" || im.MIME != "image/"+format || im.Draft || im.MaxAge != 300 || im.SurrogateKey != "store:staging:"+f.id.String() || im.ETag == "" {
					t.Fatal(im)
				}
			}
		}
	}
	// Official imgproxy signing example: salt + path, HMAC-SHA256, URL-safe unpadded base64.
	path := "/rs:fill:300:400:0/g:sm/aHR0cDovL2V4YW1w/bGUuY29tL2ltYWdl/cy9jdXJpb3NpdHku/anBn.png"
	if digest([]byte("secret"), []byte("hello"), path) != "oKfUtW34Dvo2BGQehJFR4Nr0_rIjOtdtzJ3QFsUcXH8" {
		t.Fatal("signing protocol")
	}
}
func TestCNT043TamperingNeverReachesProxy(t *testing.T) {
	f := setup(t)
	m := f.mint(t, Options{640, 0, "contain", "webp"})
	original := m.Variants[0].URL
	for _, k := range []string{"project", "v", "rev", "scope", "pk", "exp", "w", "h", "fit", "fmt", "sig"} {
		u := parse(t, original)
		q := u.Query()
		q.Set(k, "tampered")
		u.RawQuery = q.Encode()
		_, e := f.s.Fetch(context.Background(), u)
		expect(t, e, "ASSET_URL_INVALID")
	}
	for _, raw := range []string{original + "&w=320", original + "&extra=1", original + "&broken=%xx", original + ";bad"} {
		_, e := f.s.Fetch(context.Background(), parse(t, raw))
		expect(t, e, "ASSET_URL_INVALID")
	}
	uAlias := parse(t, original)
	parts := strings.Split(uAlias.RawQuery, "&")
	parts[0], parts[1] = parts[1], parts[0]
	uAlias.RawQuery = strings.Join(parts, "&")
	_, aliasErr := f.s.Fetch(context.Background(), uAlias)
	expect(t, aliasErr, "ASSET_URL_INVALID")
	u := parse(t, original)
	u.Path = strings.Replace(u.Path, "staging", "production", 1)
	_, e := f.s.Fetch(context.Background(), u)
	expect(t, e, "ASSET_URL_INVALID")
	f.s.Now = func() time.Time { return m.ExpiresAt }
	_, e = f.s.Fetch(context.Background(), parse(t, original))
	expect(t, e, "ASSET_URL_INVALID")
	if f.count.Load() != 0 {
		t.Fatal("unauthorized upstream request")
	}
}
func TestCNT043CurrentPublicationAndFile(t *testing.T) {
	f := setup(t)
	m := f.mint(t, Options{640, 0, "contain", "png"})
	u := parse(t, m.Variants[0].URL)
	production := f.a
	production.Environment = "production"
	_, e := f.s.Mint(context.Background(), production, f.id, Options{640, 0, "contain", "png"})
	if e == nil {
		t.Fatal("unpublished")
	}
	f.e.Exec(`UPDATE asset_files SET status='rejected'`)
	_, e = f.s.Fetch(context.Background(), u)
	expect(t, e, "ASSET_IMAGE_NOT_FOUND")
	f.e.Exec(`UPDATE asset_files SET status='ready'`)
	f.e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{focalPoint,x}','0.9') WHERE object_id=$1`, f.id)
	_, e = f.s.Fetch(context.Background(), u)
	expect(t, e, "ASSET_IMAGE_NOT_FOUND")
	f.e.Exec(`DELETE FROM published_pointers WHERE object_id=$1`, f.id)
	_, e = f.s.Fetch(context.Background(), u)
	if e == nil {
		t.Fatal("unpublished URL")
	}
	if f.count.Load() != 0 {
		t.Fatal("invalid state reached proxy")
	}
}
func TestAPI040PreviewScopeExpiryAndRotation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	env, _ := f.e.Q.GetEnvironmentPreviewKey(ctx, store.GetEnvironmentPreviewKeyParams{Slug: "store", Name: "staging"})
	f.a.Draft = true
	f.a.PreviewKeyHash = fingerprint(env.PreviewKey)
	f.a.ExpiresAt = f.s.Now().Add(45 * time.Second)
	m := f.mint(t, Options{640, 0, "contain", "webp"})
	if !m.ExpiresAt.Equal(f.a.ExpiresAt) {
		t.Fatal("preview expiry")
	}
	im, e := f.s.Fetch(ctx, parse(t, m.Variants[0].URL))
	if e != nil || !im.Draft || im.MaxAge != 45 {
		t.Fatal(im, e)
	}
	var cs struct {
		ID uuid.UUID `json:"id"`
	}
	f.e.Must(f.e.Admin, "create-changeset", map[string]any{"title": "image"}, &cs)
	f.a.ChangesetID = &cs.ID
	m = f.mint(t, Options{640, 0, "contain", "webp"})
	u := parse(t, m.Variants[0].URL)
	im, e = f.s.Fetch(ctx, u)
	if e != nil || !im.Draft {
		t.Fatal(e)
	}
	f.e.Exec(`UPDATE environments SET preview_key=decode(repeat('aa',32),'hex') WHERE id=$1`, f.a.EnvironmentID)
	_, e = f.s.Fetch(ctx, u)
	expect(t, e, "ASSET_URL_INVALID")
	f.a.ExpiresAt = f.s.Now()
	_, e = f.s.Mint(ctx, f.a, f.id, Options{640, 0, "contain", "webp"})
	expect(t, e, "ASSET_URL_INVALID")
}
func TestOptionsConfigAndMalformedSignedGrants(t *testing.T) {
	for _, query := range []string{"w=319", "h=1", "fit=cover", "fmt=gif", "w=0320", "h=-1", "unknown=1", "fit=", "w=320&w=320", "w=x"} {
		q, _ := url.ParseQuery(query)
		if _, e := ParseOptions(q); e == nil {
			t.Fatal(query)
		}
	}
	if o, e := ParseOptions(url.Values{"w": {"320"}, "h": {"200"}, "fit": {"cover"}, "fmt": {"avif"}, "changesetId": {"x"}}); e != nil || o.Height != 200 {
		t.Fatal(o, e)
	}
	for _, mutate := range []func(*Config){func(c *Config) { c.PublicURL = "/relative" }, func(c *Config) { c.PublicURL = "https://x/a" }, func(c *Config) { c.ProxyURL = "http://user:secret@x" }, func(c *Config) { c.ProxyURL = "http://x?q=1" }, func(c *Config) { c.Bucket = "bad/key" }, func(c *Config) { c.Key = "x" }, func(c *Config) { c.ProxyKey = "11" }, func(c *Config) { c.ProxySalt = "" }} {
		c := config("http://localhost")
		mutate(&c)
		if _, e := New(c); e == nil {
			t.Fatal(c)
		}
	}
	f := setup(t)
	m := f.mint(t, Options{640, 0, "contain", "png"})
	for _, change := range []func(*url.URL){
		func(u *url.URL) { u.Path = "/bad" }, func(u *url.URL) { u.Path = strings.Replace(u.Path, f.id.String(), "bad", 1) },
		func(u *url.URL) { q := u.Query(); q.Set("exp", "bad"); u.RawQuery = q.Encode() },
		func(u *url.URL) { q := u.Query(); q.Set("exp", "1900000000"); u.RawQuery = q.Encode() },
		func(u *url.URL) { q := u.Query(); q.Set("pk", "unexpected"); u.RawQuery = q.Encode() },
		func(u *url.URL) {
			q := u.Query()
			q.Set("scope", "bad")
			q.Set("pk", f.a.PreviewKeyHash)
			u.RawQuery = q.Encode()
		},
		func(u *url.URL) { q := u.Query(); q.Set("w", "x"); u.RawQuery = q.Encode() },
		func(u *url.URL) { q := u.Query(); q.Set("h", "x"); u.RawQuery = q.Encode() },
		func(u *url.URL) { q := u.Query(); q.Set("fmt", "svg"); u.RawQuery = q.Encode() },
	} {
		u := parse(t, m.Variants[0].URL)
		change(u)
		resign(f.s, u)
		_, e := f.s.Fetch(context.Background(), u)
		if e == nil {
			t.Fatal(u)
		}
	}
	if f.count.Load() != 0 {
		t.Fatal("bad signed grant reached proxy")
	}
	if _, e := f.s.Mint(context.Background(), f.a, f.id, Options{1, 0, "contain", "png"}); e == nil {
		t.Fatal("bad mint")
	}
}
func TestInvalidContentAndBounds(t *testing.T) {
	f := setup(t)
	for _, body := range []any{map[string]any{"managedFile": true}, map[string]any{"managedFile": true, "mimeType": "application/pdf", "width": 1, "height": 1}, map[string]any{"managedFile": true, "mimeType": "image/png", "width": 1, "height": 1, "fileHash": "bad"}, map[string]any{"managedFile": true, "mimeType": "image/png", "width": 40000001, "height": 1}} {
		id := f.e.ContentFixture("asset", "", body, "staging")
		_, e := f.s.Mint(context.Background(), f.a, id, Options{640, 0, "contain", "png"})
		expect(t, e, "ASSET_IMAGE_NOT_FOUND")
	}
	// Filter variants by result height; reject a source ratio with no bounded variant.
	f.e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{width}','1') WHERE object_id=$1`, f.id)
	f.e.Exec(`UPDATE asset_files SET width=1`)
	_, e := f.s.Mint(context.Background(), f.a, f.id, Options{640, 0, "contain", "png"})
	expect(t, e, "ASSET_IMAGE_NOT_FOUND")
	f.e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{width}','320') WHERE object_id=$1`, f.id)
	f.e.Exec(`UPDATE asset_files SET width=320`)
	m := f.mint(t, Options{320, 2560, "cover", "png"})
	if len(m.Variants) != 1 || m.Height != 2560 {
		t.Fatal(m)
	}
}

func TestFileInconsistencyAndReadErrors(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	m := f.mint(t, Options{640, 0, "contain", "png"})
	u := parse(t, m.Variants[0].URL)
	for _, statement := range []string{`UPDATE asset_files SET storage_key='other'`, `UPDATE asset_files SET width=NULL`, `UPDATE asset_files SET mime_type='image/jpeg'`} {
		f.e.Exec(statement)
		_, e := f.s.Mint(ctx, f.a, f.id, Options{640, 0, "contain", "png"})
		expect(t, e, "ASSET_IMAGE_NOT_FOUND")
		f.e.Exec(`UPDATE asset_files SET storage_key='projects/' || project_id::text || '/assets/' || encode(sha256,'hex'),width=320,mime_type='image/png'`)
	}
	f.e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{focalPoint,x}','2') WHERE object_id=$1`, f.id)
	_, e := f.s.Mint(ctx, f.a, f.id, Options{640, 0, "contain", "png"})
	expect(t, e, "ASSET_IMAGE_NOT_FOUND")
	f.e.Exec(`UPDATE object_versions SET body=body-'focalPoint' WHERE object_id=$1`, f.id)
	m = f.mint(t, Options{640, 0, "contain", "png"})
	if m.FocalPoint != (Point{.5, .5}) {
		t.Fatal("default focus")
	}
	f.e.Exec(`ALTER TABLE asset_files RENAME TO unavailable_files`)
	_, e = f.s.Mint(ctx, f.a, f.id, Options{640, 0, "contain", "png"})
	var problem *commandbus.Error
	if e == nil || errors.As(e, &problem) {
		t.Fatal("DB failure disguised as missing", e)
	}
	f.e.Exec(`ALTER TABLE unavailable_files RENAME TO asset_files`)
	q := u.Query()
	q.Set("project", "missing")
	u.RawQuery = q.Encode()
	resign(f.s, u)
	_, e = f.s.Fetch(ctx, u)
	expect(t, e, "ASSET_IMAGE_NOT_FOUND")
	f.e.Pool.Close()
	_, e = f.s.Fetch(ctx, u)
	if e == nil {
		t.Fatal("environment DB failure")
	}
}

func TestPreviewChangesAndSignedBounds(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	env, _ := f.e.Q.GetEnvironmentPreviewKey(ctx, store.GetEnvironmentPreviewKeyParams{Slug: "store", Name: "staging"})
	f.a.Draft = true
	f.a.PreviewKeyHash = fingerprint(env.PreviewKey)
	f.a.ExpiresAt = f.s.Now().Add(TTL)
	var cs struct {
		ID uuid.UUID `json:"id"`
	}
	f.e.Must(f.e.Admin, "create-changeset", map[string]any{"title": "draft image"}, &cs)
	f.a.ChangesetID = &cs.ID
	m := f.mint(t, Options{640, 0, "contain", "png"})
	old := parse(t, m.Variants[0].URL)
	f.e.Must(f.e.Admin, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 0, "operations": []any{map[string]any{"type": "asset.updateMeta", "target": f.id, "payload": map[string]any{"set": map[string]any{"focalPoint": map[string]any{"x": .9, "y": .5}}}}}}, nil)
	_, e := f.s.Fetch(ctx, old)
	expect(t, e, "ASSET_IMAGE_NOT_FOUND")
	m = f.mint(t, Options{640, 0, "contain", "png"})
	working := parse(t, m.Variants[0].URL)
	f.e.Must(f.e.Admin, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 1, "operations": []any{map[string]any{"type": "asset.updateMeta", "target": f.id, "payload": map[string]any{"set": map[string]any{"title": "updated"}}}}}, nil)
	_, e = f.s.Fetch(ctx, working)
	expect(t, e, "ASSET_IMAGE_NOT_FOUND")
	for _, scope := range []string{"invalid", uuid.NewString()} {
		u := parse(t, m.Variants[0].URL)
		q := u.Query()
		q.Set("scope", scope)
		u.RawQuery = q.Encode()
		resign(f.s, u)
		_, e = f.s.Fetch(ctx, u)
		if e == nil {
			t.Fatal("unknown scope", scope)
		}
	}
	u := parse(t, m.Variants[0].URL)
	u.Path = strings.Replace(u.Path, f.id.String(), strings.ToUpper(f.id.String()), 1)
	resign(f.s, u)
	_, e = f.s.Fetch(ctx, u)
	expect(t, e, "ASSET_URL_INVALID")
	// Capability cannot bypass computed output limits even with a valid signature.
	f.e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{width}','1') WHERE object_id=$1`, f.id)
	f.e.Exec(`UPDATE asset_files SET width=1`)
	u = old
	q := u.Query()
	q.Set("scope", "head")
	q.Set("rev", fingerprint([]byte("unused")))
	u.RawQuery = q.Encode()
	resign(f.s, u)
	// Signature parsing is independently fuzzed; content checks still stop this old revision.
	_, e = f.s.Fetch(ctx, u)
	if e == nil {
		t.Fatal("stale capability")
	}
}

func TestProxyRedirectIsNotFollowed(t *testing.T) {
	f := setup(t)
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("bad"))
	}))
	t.Cleanup(target.Close)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	t.Cleanup(proxy.Close)
	s, e := New(config(proxy.URL))
	if e != nil {
		t.Fatal(e)
	}
	s.Pool = f.e.Pool
	s.Now = f.s.Now
	f.s = s
	m := f.mint(t, Options{640, 0, "contain", "png"})
	_, e = s.Fetch(context.Background(), parse(t, m.Variants[0].URL))
	expect(t, e, "ASSET_IMAGE_UPSTREAM")
	if targetHits.Load() != 0 {
		t.Fatal("redirect followed")
	}
}

type roundtrip func(*http.Request) (*http.Response, error)

func (f roundtrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (brokenReader) Close() error             { return nil }
func TestUpstreamFailures(t *testing.T) {
	f := setup(t)
	m := f.mint(t, Options{640, 0, "contain", "png"})
	u := parse(t, m.Variants[0].URL)
	for _, rt := range []roundtrip{
		func(*http.Request) (*http.Response, error) { return nil, io.ErrUnexpectedEOF },
		func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 302, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
		},
		func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader("bad"))}, nil
		},
		func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}}, ContentLength: MaxBody + 1, Body: io.NopCloser(strings.NewReader("bad"))}, nil
		},
		func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}}, Body: brokenReader{}}, nil
		},
		func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		},
		func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(bytes.NewReader(make([]byte, MaxBody+1)))}, nil
		},
	} {
		f.s.Client = &http.Client{Transport: rt}
		_, e := f.s.Fetch(context.Background(), u)
		expect(t, e, "ASSET_IMAGE_UPSTREAM")
	}
	// Missing content/file and infrastructure errors are distinct.
	f.e.Pool.Close()
	_, e := f.s.Mint(context.Background(), f.a, f.id, Options{640, 0, "contain", "png"})
	if e == nil {
		t.Fatal("closed database")
	}
}
func FuzzParseImageOptions(f *testing.F) {
	for _, q := range []string{"", "w=320&fit=cover&h=200", "w=%xx", "w=320&w=640"} {
		f.Add(q)
	}
	f.Fuzz(func(t *testing.T, raw string) { q, _ := url.ParseQuery(raw); _, _ = ParseOptions(q) })
}
