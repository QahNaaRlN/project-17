package httpapi_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestStudioHistoryRoutes(t *testing.T) {
	// STU-050/051/052: public API groups, undoes/redoes and pages complete actions.
	e := setup(t)
	cs := e.command(t, "create-changeset", `{"title":"history","targets":["staging"]}`)["id"].(string)
	created := e.command(t, "apply-operations", `{"changesetId":"`+cs+`","expectedSeq":0,"operations":[{"type":"document.create","payload":{"kind":"page","path":"/history","root":{"id":"n_root","type":"Box"}}}]}`)
	doc := created["operations"].([]any)[0].(map[string]any)["target"].(string)
	e.command(t, "apply-operations", `{"changesetId":"`+cs+`","expectedSeq":1,"group":true,"operations":[{"type":"localContent.set","target":"`+doc+`","payload":{"key":"t_text","type":"text","locale":"ru","value":"private original text"}},{"type":"node.setBinding","target":"`+doc+`","payload":{"nodeId":"n_root","prop":"text","binding":"$local.t_text"}}]}`)
	path := "/api/v1/changesets/" + cs + "/history"
	resp, body := e.do(t, "GET", path, "", e.authed(nil))
	if resp.StatusCode != 200 || body["seq"] != float64(3) || body["canUndo"] != true || body["canRedo"] != false || len(body["items"].([]any)) != 2 {
		t.Fatalf("%d %v", resp.StatusCode, body)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "private original text") || strings.Contains(string(raw), "payload") || strings.Contains(string(raw), "before\"") {
		t.Fatal("raw history leaked", string(raw))
	}
	payload := `{"payload":{"changesetId":"` + cs + `","expectedSeq":3},"reason":"Studio undo"}`
	headers := e.authed(map[string]string{"Idempotency-Key": "history-same-key"})
	for i := 0; i < 2; i++ {
		resp, body = e.do(t, "POST", "/api/v1/commands/undo", payload, headers)
		if resp.StatusCode != 200 || body["result"].(map[string]any)["changeset"].(map[string]any)["seq"] != float64(5) {
			t.Fatalf("idempotent undo: %d %v", resp.StatusCode, body)
		}
	}
	resp, body = e.do(t, "GET", path, "", e.authed(nil))
	if resp.StatusCode != 200 || body["canRedo"] != true {
		t.Fatal(body)
	}
	resp, body = e.do(t, "GET", path+"?beforeSeq=4", "", e.authed(nil))
	if resp.StatusCode != 200 || len(body["items"].([]any)) != 2 {
		t.Fatal(body)
	}
	e.command(t, "redo", `{"changesetId":"`+cs+`","expectedSeq":5}`)
	for _, suffix := range []string{"?beforeSeq=0", "?beforeSeq=-1", "?beforeSeq=bad", "?beforeSeq=2147483648", "?beforeSeq=1&beforeSeq=2", "?afterSeq=0"} {
		resp, body = e.do(t, "GET", path+suffix, "", e.authed(nil))
		expectProblem(t, resp, body, 400, "PARAM_INVALID")
	}
	resp, body = e.do(t, "GET", "/api/v1/changesets/bad/history", "", e.authed(nil))
	expectProblem(t, resp, body, 400, "PARAM_INVALID")
	resp, body = e.do(t, "GET", "/api/v1/changesets/"+uuid.NewString()+"/history", "", e.authed(nil))
	expectProblem(t, resp, body, 404, "NOT_FOUND")
	resp, body = e.do(t, "GET", path, "", nil)
	expectProblem(t, resp, body, 401, "UNAUTHENTICATED")
	if _, err := e.pool.Exec(context.Background(), `UPDATE api_tokens SET scopes=ARRAY['content.read']::text[]`); err != nil {
		t.Fatal(err)
	}
	resp, body = e.do(t, "GET", path, "", e.authed(nil))
	expectProblem(t, resp, body, 403, "FORBIDDEN")
}
func TestStudioHistoryStorageFailure(t *testing.T) {
	e := setup(t)
	cs := e.command(t, "create-changeset", `{"title":"history"}`)["id"].(string)
	if _, err := e.pool.Exec(context.Background(), `ALTER TABLE operations RENAME TO operations_broken`); err != nil {
		t.Fatal(err)
	}
	resp, body := e.do(t, "GET", "/api/v1/changesets/"+cs+"/history", "", e.authed(nil))
	expectProblem(t, resp, body, 500, "INTERNAL")
}
