package httpapi_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/httpapi"
	"github.com/qahnaarln/project-17/apps/server/internal/imagedelivery"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

// CNT-043/050/051, API-033/040: browser image URLs respect publication, preview and cache boundaries.
func TestImageHTTP(t *testing.T) {
	e := setup(t)
	actor, err := auth.Authenticate(context.Background(), store.New(e.pool), "Bearer "+e.token)
	if err != nil {
		t.Fatal(err)
	}
	f := cmstest.Env{T: t, Pool: e.pool, Q: store.New(e.pool), Admin: actor}
	hash := sha256.Sum256([]byte("original"))
	hex := fmt.Sprintf("%x", hash)
	f.Exec(`INSERT INTO asset_files(project_id,sha256,storage_key,mime_type,size_bytes,width,height,status) VALUES($1,$2,$3,'image/png',8,320,200,'ready')`, actor.ProjectID, hash[:], "projects/"+actor.ProjectID.String()+"/assets/"+hex)
	id := f.ContentFixture("asset", "", map[string]any{"fileHash": hex, "mimeType": "image/png", "managedFile": true, "width": 320, "height": 200}, "staging")
	key := e.command(t, "create-delivery-key", `{"environment":"staging","name":"images"}`)["key"].(string)
	path := "/delivery/v1/store/staging/asset/" + id.String() + "/image"
	resp, body := e.do(t, "GET", path, "", bearer(key))
	expectProblem(t, resp, body, 503, "ASSET_IMAGES_UNAVAILABLE")
	resp, body = e.do(t, "GET", "/assets/v1/staging/"+id.String()+"/"+hex, "", nil)
	expectProblem(t, resp, body, 503, "ASSET_IMAGES_UNAVAILABLE")
	cs := e.command(t, "create-changeset", `{"title":"image preview"}`)["id"].(string)
	preview := e.command(t, "create-preview-token", `{"environment":"staging","changesetId":"`+cs+`"}`)["token"].(string)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/webp")
		w.Header().Set("Set-Cookie", "secret=upstream")
		w.Header().Set("Location", "http://private")
		_, _ = w.Write([]byte("binary-image"))
	}))
	t.Cleanup(upstream.Close)
	images, err := imagedelivery.New(imagedelivery.Config{PublicURL: "https://cdn.example.test", ProxyURL: upstream.URL, Bucket: "assets", Key: strings.Repeat("11", 32), ProxyKey: strings.Repeat("22", 32), ProxySalt: strings.Repeat("33", 16)})
	if err != nil {
		t.Fatal(err)
	}
	images.Pool = e.pool
	srv := httptest.NewServer(httpapi.NewRouter(httpapi.Deps{Pool: e.pool, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Images: images}))
	t.Cleanup(srv.Close)
	e.srv = srv
	resp, body = e.do(t, "GET", path, "", bearer(key))
	if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(resp.StatusCode, body)
	}
	raw := body["variants"].([]any)[0].(map[string]any)["url"].(string)
	u, _ := url.Parse(raw)
	get := func(headers map[string]string) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+u.RequestURI(), nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, b
	}
	resp, binary := get(nil)
	if resp.StatusCode != 200 || string(binary) != "binary-image" || !strings.Contains(resp.Header.Get("Cache-Control"), "s-maxage=300") || resp.Header.Get("Surrogate-Key") != "store:staging:"+id.String() || resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Set-Cookie") != "" || resp.Header.Get("Location") != "" {
		t.Fatal(resp.Header, string(binary))
	}
	resp, binary = get(map[string]string{"If-None-Match": resp.Header.Get("ETag")})
	if resp.StatusCode != 304 || len(binary) != 0 {
		t.Fatal(resp.StatusCode, string(binary))
	}
	for _, suffix := range []string{"?w=319", "?fit=cover", "?fmt=svg", "?w=320&w=640", "?fmt=webp;unknown", "?extra=1"} {
		resp, body = e.do(t, "GET", path+suffix, "", bearer(key))
		expectProblem(t, resp, body, 400, "PARAM_INVALID")
	}
	resp, body = e.do(t, "GET", strings.Replace(path, id.String(), "bad", 1), "", bearer(key))
	expectProblem(t, resp, body, 400, "PARAM_INVALID")
	resp, body = e.do(t, "GET", path, "", nil)
	expectProblem(t, resp, body, 401, "UNAUTHENTICATED")
	resp, body = e.do(t, "GET", u.RequestURI()+"&w=2560", "", nil)
	expectProblem(t, resp, body, 403, "ASSET_URL_INVALID")
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("cached error")
	}
	resp, body = e.do(t, "GET", path+"?changesetId="+cs, "", map[string]string{"Authorization": "Preview " + preview})
	if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(resp.StatusCode, body)
	}
	u, _ = url.Parse(body["variants"].([]any)[0].(map[string]any)["url"].(string))
	resp, _ = get(nil)
	if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("Surrogate-Key") != "" {
		t.Fatal(resp.Header)
	}
	f.Exec(`UPDATE environments SET preview_key=decode(repeat('aa',32),'hex') WHERE project_id=$1 AND name='staging'`, actor.ProjectID)
	resp, body = e.do(t, "GET", u.RequestURI(), "", nil)
	expectProblem(t, resp, body, 403, "ASSET_URL_INVALID")
}
