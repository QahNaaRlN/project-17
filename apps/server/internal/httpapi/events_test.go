package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
)

func eventScope(t *testing.T, e env) (string, string, string) {
	a := workspaceActor(t, e)
	doc := workspacePage(t, e, a, "/live", "page").String()
	cs := workspaceDraft(t, e, a, "open", "standard", "staging").String()
	return doc, cs, fmt.Sprintf(`"documentId":%q,"changesetId":%q,"environment":"staging"`, doc, cs)
}
func streamOpen(t *testing.T, e env, doc, cs, after string, headers map[string]string) (*http.Response, *bufio.Scanner) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/v1/events?environment=staging&documentId="+doc+"&changesetId="+cs+after, nil)
	for k, v := range e.authed(headers) {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("stream: %v", resp)
	}
	return resp, bufio.NewScanner(resp.Body)
}
func nextEvent(t *testing.T, s *bufio.Scanner, want string) map[string]any {
	t.Helper()
	kind := ""
	raw := ""
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "event: ") {
			kind = strings.TrimPrefix(line, "event: ")
		}
		if strings.HasPrefix(line, "data: ") {
			raw = strings.TrimPrefix(line, "data: ")
		}
		if line == "" && kind == want {
			var out map[string]any
			if err := json.Unmarshal([]byte(raw), &out); err != nil {
				t.Fatal(err)
			}
			return out
		}
	}
	t.Fatalf("missing %s: %v", want, s.Err())
	return nil
}

// CHG-042: presence of other users on the document.
func TestEventsCatchupAndPresence(t *testing.T) {
	e := setup(t)
	doc, cs, scope := eventScope(t, e)
	lease1, lease2 := uuid.NewString(), uuid.NewString()
	// Tabs deduplicate by actor/CS; one tab leaving never removes the other lease.
	for _, lease := range []string{lease1, lease2} {
		e.command(t, "presence-heartbeat", `{`+scope+`,"leaseId":"`+lease+`"}`)
	}
	var revision int
	e.pool.QueryRow(context.Background(), `SELECT COALESCE((SELECT revision FROM project_revisions),0)`).Scan(&revision)
	if revision != 0 {
		t.Fatal("heartbeat invalidated content")
	}
	resp, scan := streamOpen(t, e, doc, cs, "", nil)
	if nextEvent(t, scan, "invalidate")["revision"] != "0" {
		t.Fatal("initial reload")
	}
	items := nextEvent(t, scan, "presence")["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["changesetId"] != cs {
		t.Fatal(items)
	}
	e.command(t, "create-changeset", `{"title":"Live","targets":["staging"]}`)
	if nextEvent(t, scan, "invalidate")["revision"] != "1" {
		t.Fatal("committed change missing")
	}
	resp.Body.Close()
	e.command(t, "presence-leave", `{"leaseId":"`+lease1+`"}`)
	resp, scan = streamOpen(t, e, doc, cs, "", map[string]string{"Last-Event-ID": "1"})
	if nextEvent(t, scan, "sync")["revision"] != "1" {
		t.Fatal("resume same revision")
	}
	if len(nextEvent(t, scan, "presence")["items"].([]any)) != 1 {
		t.Fatal("removed other tab")
	}
	resp.Body.Close()
	workspaceExec(t, e, `UPDATE presence_leases SET expires_at=now()-interval '1 second'`)
	resp, scan = streamOpen(t, e, doc, cs, "&after=0", nil)
	if nextEvent(t, scan, "invalidate")["revision"] != "1" {
		t.Fatal("catchup")
	}
	if len(nextEvent(t, scan, "presence")["items"].([]any)) != 0 {
		t.Fatal("expired lease exposed")
	}
	resp.Body.Close()
	e.command(t, "presence-heartbeat", `{`+scope+`,"leaseId":"`+lease1+`"}`)
	var count int
	e.pool.QueryRow(context.Background(), `SELECT count(*) FROM presence_leases`).Scan(&count)
	if count != 1 {
		t.Fatal("expired lease not cleaned")
	}
}
func TestEventsScopeAndCommands(t *testing.T) {
	e := setup(t)
	doc, cs, scope := eventScope(t, e)
	base := "/api/v1/events?environment=staging&documentId=" + doc + "&changesetId=" + cs
	for _, suffix := range []string{"&after=-1", "&after=x", "&after=9223372036854775808", "&actorId=" + doc, "&documentId=" + doc} {
		resp, out := e.do(t, "GET", base+suffix, "", e.authed(nil))
		expectProblem(t, resp, out, 400, "PARAM_INVALID")
	}
	for _, query := range []string{"", "?environment=missing&documentId=" + doc + "&changesetId=" + cs, "?environment=staging&documentId=bad&changesetId=" + cs, "?environment=staging&documentId=" + doc + "&changesetId=bad"} {
		resp, out := e.do(t, "GET", "/api/v1/events"+query, "", e.authed(nil))
		expectProblem(t, resp, out, 400, "PARAM_INVALID")
	}
	resp, out := e.do(t, "GET", base, "", nil)
	expectProblem(t, resp, out, 401, "UNAUTHENTICATED")
	resp, out = e.do(t, "GET", strings.Replace(base, "environment=staging", "environment=production", 1), "", e.authed(nil))
	expectProblem(t, resp, out, 403, "PRESENCE_SCOPE_FORBIDDEN")
	foreign, err := projects.Bootstrap(context.Background(), e.pool, "foreign", "Foreign", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	resp, out = e.do(t, "GET", base, "", map[string]string{"Authorization": "Bearer " + foreign.Token, "X-CMS-Project": "foreign"})
	expectProblem(t, resp, out, 404, "NOT_FOUND")
	component := workspacePage(t, e, workspaceActor(t, e), "/component", "component").String()
	resp, out = e.do(t, "GET", strings.Replace(base, doc, component, 1), "", e.authed(nil))
	expectProblem(t, resp, out, 400, "PARAM_INVALID")
	for _, payload := range []string{`{"leaseId":"00000000-0000-0000-0000-000000000000"}`, `{` + scope + `,"leaseId":"00000000-0000-0000-0000-000000000000"}`, `{"leaseId":"` + uuid.NewString() + `"}`} {
		resp, out = e.do(t, "POST", "/api/v1/commands/presence-heartbeat", `{"payload":`+payload+`}`, e.authed(map[string]string{"Idempotency-Key": uuid.NewString()}))
		expectProblem(t, resp, out, 400, "PARAM_INVALID")
	}
	workspaceExec(t, e, `UPDATE roles SET capabilities=ARRAY['content.read']::text[]`)
	resp, out = e.do(t, "GET", base, "", e.authed(nil))
	expectProblem(t, resp, out, 403, "FORBIDDEN")
	resp, out = e.do(t, "POST", "/api/v1/commands/presence-heartbeat", `{"payload":{`+scope+`,"leaseId":"`+uuid.NewString()+`"}}`, e.authed(map[string]string{"Idempotency-Key": uuid.NewString()}))
	expectProblem(t, resp, out, 403, "FORBIDDEN")
}
func TestEventsLifetimeReauth(t *testing.T) {
	e := setup(t)
	doc, cs, _ := eventScope(t, e)
	resp, scan := streamOpen(t, e, doc, cs, "&after=0", nil)
	nextEvent(t, scan, "sync")
	workspaceExec(t, e, `UPDATE api_tokens SET revoked_at=now()`)
	started := time.Now()
	for scan.Scan() {
	}
	if scan.Err() != nil || time.Since(started) > 30*time.Second {
		t.Fatal("unbounded stream", scan.Err())
	}
	resp.Body.Close()
	response, out := e.do(t, "GET", "/api/v1/events?environment=staging&documentId="+doc+"&changesetId="+cs, "", e.authed(nil))
	expectProblem(t, response, out, 401, "UNAUTHENTICATED")
}
