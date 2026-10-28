package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/httpapi"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

type memoryArtifacts struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (m *memoryArtifacts) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	b, err := io.ReadAll(r)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = b
	return err
}

func (m *memoryArtifacts) Open(_ context.Context, key string) (io.ReadCloser, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.objects[key]; ok {
		return io.NopCloser(bytes.NewReader(b)), int64(len(b)), nil
	}
	return nil, 0, errors.New("no such object")
}

func (m *memoryArtifacts) Remove(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

// STU-573, 575: the runner uploads under its lease; a reader of the Change Set reads the
// file with safe headers, an HTML report in a sandbox.
func TestCheckArtifactRoutes(t *testing.T) {
	s := &memoryArtifacts{objects: map[string][]byte{}}
	e := setup(t, func(d *httpapi.Deps) {
		d.Artifacts = s
		d.Bus.WithContext(workflow.WithArtifacts(s))
	})
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `UPDATE project_render_checks SET enabled = true`); err != nil {
		t.Fatal(err)
	}
	e.command(t, "set-environment-app-url", `{"environment":"staging","appUrl":"http://127.0.0.1:18099"}`)
	token, err := projects.CheckRunnerToken(ctx, e.pool, "store", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	runner := func(extra map[string]string) map[string]string {
		h := map[string]string{"Authorization": "Bearer " + token.Token, "X-CMS-Project": "store", "Content-Type": "application/json"}
		for k, v := range extra {
			h[k] = v
		}
		return h
	}
	cs := e.command(t, "create-changeset", `{"title":"visual","targets":["staging"]}`)["id"].(string)
	e.command(t, "apply-operations", `{"changesetId":"`+cs+`","expectedSeq":0,"operations":[{"type":"document.create","payload":{"kind":"page","path":"/shot","root":{"id":"n_root","type":"Box"}}}]}`)
	e.command(t, "submit-changeset", `{"changesetId":"`+cs+`","expectedSeq":1}`)
	var visual map[string]any
	for range 3 {
		resp, body := e.do(t, "POST", "/api/v1/commands/claim-check", `{"payload":{}}`, runner(map[string]string{"Idempotency-Key": uuid.NewString()}))
		if resp.StatusCode != 200 {
			t.Fatal(resp.StatusCode, body)
		}
		visual = body["result"].(map[string]any)["check"].(map[string]any)
		if visual["stage"] == workflow.StageVisual {
			break
		}
	}
	if visual["stage"] != workflow.StageVisual {
		t.Fatal(visual)
	}
	check, lease := visual["checkId"].(string), visual["leaseId"].(string)
	upload := "/api/v1/checks/" + check + "/artifacts/"
	png := runner(map[string]string{"Content-Type": "image/png", "X-CMS-Check-Lease": lease})
	resp, body := e.do(t, "PUT", upload+"shot-375-after.png", "\x89PNG", png)
	if resp.StatusCode != 204 {
		t.Fatal(resp.StatusCode, body)
	}
	html := runner(map[string]string{"Content-Type": "text/html; charset=utf-8", "X-CMS-Check-Lease": lease})
	if resp, body = e.do(t, "PUT", upload+"report.html", "<script>alert(1)</script>", html); resp.StatusCode != 204 {
		t.Fatal(resp.StatusCode, body)
	}
	resp, body = e.do(t, "PUT", upload+"big.png", strings.Repeat("x", workflow.MaxArtifactSize+1), png)
	expectProblem(t, resp, body, 413, "CHECK_ARTIFACTS_LIMIT")
	resp, body = e.do(t, "PUT", upload+"a.png", "x", runner(map[string]string{"Content-Type": "image/png", "X-CMS-Check-Lease": "nope"}))
	expectProblem(t, resp, body, 400, "PARAM_INVALID")
	resp, body = e.do(t, "PUT", "/api/v1/checks/bad/artifacts/a.png", "x", png)
	expectProblem(t, resp, body, 400, "PARAM_INVALID")
	resp, body = e.do(t, "PUT", upload+"a.png", "x", e.authed(map[string]string{"Content-Type": "image/png", "X-CMS-Check-Lease": lease}))
	expectProblem(t, resp, body, 403, "FORBIDDEN")

	read := "/api/v1/changesets/" + cs + "/checks/" + check + "/artifacts/"
	get := func(name string, headers map[string]string) (*http.Response, string) {
		req, _ := http.NewRequest("GET", e.srv.URL+read+name, nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r, string(b)
	}
	r, got := get("shot-375-after.png", e.authed(nil))
	if r.StatusCode != 200 || got != "\x89PNG" || r.Header.Get("Content-Type") != "image/png" || r.Header.Get("X-Content-Type-Options") != "nosniff" ||
		r.Header.Get("Cache-Control") != "private, max-age=3600" || r.Header.Get("Content-Security-Policy") != "" {
		t.Fatal(r.StatusCode, r.Header, got)
	}
	// A local HTML filter (AdGuard) may append to the policy; the server sends "sandbox".
	if r, _ = get("report.html", e.authed(nil)); r.StatusCode != 200 || !strings.HasPrefix(r.Header.Get("Content-Security-Policy"), "sandbox") ||
		r.Header.Get("Content-Disposition") != `attachment; filename="report.html"` {
		t.Fatal(r.StatusCode, r.Header)
	}
	resp, body = e.do(t, "GET", read+"nope.png", "", e.authed(nil))
	expectProblem(t, resp, body, 404, "NOT_FOUND")
	resp, body = e.do(t, "GET", "/api/v1/changesets/"+cs+"/checks/bad/artifacts/a.png", "", e.authed(nil))
	expectProblem(t, resp, body, 400, "PARAM_INVALID")
	resp, body = e.do(t, "GET", "/api/v1/changesets/bad/checks/"+check+"/artifacts/a.png", "", e.authed(nil))
	expectProblem(t, resp, body, 400, "PARAM_INVALID")
	resp, body = e.do(t, "GET", "/api/v1/changesets/"+uuid.NewString()+"/checks/"+check+"/artifacts/report.html", "", e.authed(nil))
	expectProblem(t, resp, body, 404, "NOT_FOUND")
	resp, body = e.do(t, "GET", read+"report.html", "", nil)
	expectProblem(t, resp, body, 401, "UNAUTHENTICATED")
}
