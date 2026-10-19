package httpapi_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestComponentExtractPlanRouteSTU401(t *testing.T) {
	e := setup(t)
	cs := e.command(t, "create-changeset", `{"title":"extract","targets":["staging"]}`)["id"].(string)
	created := e.command(t, "apply-operations", `{"changesetId":"`+cs+`","expectedSeq":0,"operations":[{"type":"document.create","payload":{"kind":"page","path":"/extract","root":{"id":"n_root","type":"Box","children":[{"id":"n_block","type":"Stack","children":[{"id":"n_head","type":"Heading","props":{"level":2}}]}]}}}]}`)
	doc := created["operations"].([]any)[0].(map[string]any)["target"].(string)
	path := "/api/v1/documents/" + doc + "/component-extract-plan?changesetId=" + cs + "&nodeId=n_block"
	resp, body := e.do(t, "GET", path, "", e.authed(nil))
	if resp.StatusCode != 200 || len(body["blockers"].([]any)) != 0 || body["effectiveMode"] != "SYSTEM" {
		t.Fatalf("%d %v", resp.StatusCode, body)
	}
	found := false
	for _, c := range body["candidates"].([]any) {
		m := c.(map[string]any)
		found = found || m["nodeId"] == "n_head" && m["prop"] == "level" && m["current"] == "literal"
	}
	if !found {
		t.Fatalf("candidates: %v", body["candidates"])
	}
	resp, body = e.do(t, "GET", "/api/v1/documents/"+doc+"/component-extract-plan?changesetId="+cs+"&nodeId=n_root", "", e.authed(nil))
	if resp.StatusCode != 200 || body["blockers"].([]any)[0].(map[string]any)["code"] != "COMPONENT_EXTRACT_ROOT_DENIED" {
		t.Fatalf("root: %v", body)
	}
	resp, body = e.do(t, "GET", "/api/v1/documents/"+doc+"/component-extract-plan?changesetId=bad", "", e.authed(nil))
	expectProblem(t, resp, body, 400, "PARAM_INVALID")
	resp, body = e.do(t, "GET", "/api/v1/documents/"+doc+"/component-extract-plan?changesetId="+uuid.NewString()+"&nodeId=n_block", "", e.authed(nil))
	expectProblem(t, resp, body, 404, "NOT_FOUND")
	// Без права записи план доступен для чтения, но показывает причину отключения.
	if _, err := e.pool.Exec(context.Background(), `UPDATE api_tokens SET scopes=ARRAY['design.read']::text[]`); err != nil {
		t.Fatal(err)
	}
	resp, body = e.do(t, "GET", path, "", e.authed(nil))
	if resp.StatusCode != 200 || body["blockers"].([]any)[0].(map[string]any)["code"] != "FORBIDDEN" {
		t.Fatalf("read-only: %d %v", resp.StatusCode, body)
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE api_tokens SET scopes=ARRAY['content.read']::text[]`); err != nil {
		t.Fatal(err)
	}
	resp, body = e.do(t, "GET", path, "", e.authed(nil))
	expectProblem(t, resp, body, 403, "FORBIDDEN")
}
