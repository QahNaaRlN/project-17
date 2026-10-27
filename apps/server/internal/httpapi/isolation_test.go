package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/httpapi"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
)

// STU-514: every Query API route with an identifier in its path refuses objects of
// another project; commands on them are refused too. The route list comes from the
// router itself, so a new route joins the sweep automatically.
func TestProjectIsolationSweep(t *testing.T) {
	e := setup(t)
	e.command(t, "set-approval-policy", `{"low":0,"medium":0,"high":0}`)
	cs := e.command(t, "create-changeset", `{"title":"Isolated draft","targets":["staging"]}`)["id"].(string)
	result := e.command(t, "apply-operations", `{"changesetId":"`+cs+`","expectedSeq":0,"operations":[{"type":"document.create","payload":{"kind":"page","path":"/isolated","root":{"id":"n_root","type":"Box","name":"Secret root"}}}]}`)
	doc := result["operations"].([]any)[0].(map[string]any)["target"].(string)
	e.command(t, "submit-changeset", `{"changesetId":"`+cs+`","expectedSeq":1}`)
	publication := e.command(t, "publish", `{"changesetId":"`+cs+`","environment":"staging"}`)["id"].(string)

	other, err := projects.Bootstrap(context.Background(), e.pool, "other", "Other", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	foreign := map[string]string{"Authorization": "Bearer " + other.Token, "X-CMS-Project": "other", "Content-Type": "application/json"}

	router, ok := httpapi.NewRouter(httpapi.Deps{}).(chi.Routes)
	if !ok {
		t.Fatal("router is not walkable")
	}
	checked := 0
	err = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method != http.MethodGet || !strings.HasPrefix(route, "/api/v1/") || !strings.Contains(route, "{") {
			return nil
		}
		for _, id := range []string{cs, doc, publication} {
			path := route
			for _, param := range []string{"{id}", "{objectId}", "{documentId}", "{changesetId}"} {
				path = strings.ReplaceAll(path, param, id)
			}
			if strings.Contains(path, "{") {
				continue
			}
			resp, body := e.do(t, http.MethodGet, path+"?environment=staging", "", foreign)
			checked++
			if resp.StatusCode < 400 {
				t.Errorf("%s with %s of another project: %d %v", route, id, resp.StatusCode, body)
			}
			for _, secret := range []string{"Isolated draft", "Secret root", "/isolated"} {
				for _, v := range body {
					if s, ok := v.(string); ok && strings.Contains(s, secret) {
						t.Errorf("%s leaks %q", route, secret)
					}
				}
			}
		}
		return nil
	})
	if err != nil || checked < 20 {
		t.Fatal(checked, err)
	}
	for name, payload := range map[string]string{
		"apply-operations":  `{"changesetId":"` + cs + `","expectedSeq":1,"operations":[]}`,
		"reopen-changeset":  `{"changesetId":"` + cs + `"}`,
		"rollback":          `{"publicationId":"` + publication + `","resetHead":false}`,
		"promote":           `{"publicationId":"` + publication + `","toEnvironment":"production"}`,
		"create-changeset":  `{"title":"x","targets":["staging"],"documentId":"` + doc + `"}`,
		"submit-changeset":  `{"changesetId":"` + cs + `","expectedSeq":1}`,
		"approve-changeset": `{"changesetId":"` + cs + `"}`,
	} {
		h := map[string]string{"Idempotency-Key": "isolation-" + name}
		for k, v := range foreign {
			h[k] = v
		}
		resp, body := e.do(t, http.MethodPost, "/api/v1/commands/"+name, `{"payload":`+payload+`,"reason":"isolation"}`, h)
		if name == "create-changeset" {
			if resp.StatusCode == 200 && strings.Contains(body["result"].(map[string]any)["title"].(string), "Isolated") {
				t.Errorf("%s leaks", name)
			}
			continue
		}
		if resp.StatusCode < 400 {
			t.Errorf("%s on another project's object: %d %v", name, resp.StatusCode, body)
		}
	}
}
