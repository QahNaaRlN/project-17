package httpapi_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// STU-070/073/074: public review summarizes every object; duplicate publish returns one receipt.
func TestStudioReviewAndIdempotentPublication(t *testing.T) {
	e := setup(t)
	e.command(t, "set-approval-policy", `{"low":0,"medium":0,"high":0}`)
	cs := e.command(t, "create-changeset", `{"title":"Studio publishing","targets":["staging"]}`)["id"].(string)
	result := e.command(t, "apply-operations", `{"changesetId":"`+cs+`","expectedSeq":0,"group":true,"operations":[{"type":"document.create","payload":{"kind":"page","path":"/first","root":{"id":"n_root","type":"Box","name":"private draft"}}},{"type":"document.create","payload":{"kind":"page","path":"/second","root":{"id":"n_root","type":"Box"}}}]}`)
	doc := result["operations"].([]any)[0].(map[string]any)["target"].(string)
	path := "/api/v1/changesets/" + cs + "/review"
	response, body := e.do(t, "GET", path, "", e.authed(nil))
	if response.StatusCode != 200 || len(body["objects"].([]any)) != 2 || body["actions"].(map[string]any)["submit"] != nil {
		t.Fatal(response.StatusCode, body, e.logs.String())
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "private draft") || strings.Contains(string(raw), "\"body\"") {
		t.Fatal(string(raw))
	}
	for _, bad := range []string{path + "?environment=production", "/api/v1/changesets/bad/review"} {
		response, body = e.do(t, "GET", bad, "", e.authed(nil))
		expectProblem(t, response, body, 400, "PARAM_INVALID")
	}
	response, body = e.do(t, "GET", "/api/v1/changesets/"+uuid.NewString()+"/review", "", e.authed(nil))
	expectProblem(t, response, body, 404, "NOT_FOUND")
	response, body = e.do(t, "GET", path, "", nil)
	expectProblem(t, response, body, 401, "UNAUTHENTICATED")
	e.command(t, "submit-changeset", `{"changesetId":"`+cs+`","expectedSeq":2}`)
	payload := `{"payload":{"changesetId":"` + cs + `","environment":"staging","expectedSeq":2},"reason":"Studio publish"}`
	headers := e.authed(map[string]string{"Idempotency-Key": "studio-publication-same-key"})
	var publication string
	for i := 0; i < 2; i++ {
		response, body = e.do(t, "POST", "/api/v1/commands/publish", payload, headers)
		if response.StatusCode != 200 {
			t.Fatal(response.StatusCode, body)
		}
		pub := body["result"].(map[string]any)
		if len(pub["items"].([]any)) != 2 {
			t.Fatal(pub)
		}
		if i == 0 {
			publication = pub["id"].(string)
		} else if pub["id"] != publication {
			t.Fatal(pub)
		}
	}
	response, body = e.do(t, "GET", path, "", e.authed(nil))
	if response.StatusCode != 200 || body["changeset"].(map[string]any)["state"] != "merged" {
		t.Fatal(body)
	}
	response, body = e.do(t, "GET", "/api/v1/documents/"+doc+"?environment=staging", "", e.authed(nil))
	if response.StatusCode != 200 {
		t.Fatal(body)
	}
	response, body = e.do(t, "GET", "/api/v1/publications?environment=staging", "", e.authed(nil))
	if len(body["items"].([]any)) != 1 {
		t.Fatal(body)
	}
}

// STU-070: failing snapshot query rolls back and reports no partial review to the client.
func TestStudioReviewStorageFailure(t *testing.T) {
	e := setup(t)
	cs := e.command(t, "create-changeset", `{"title":"review"}`)["id"].(string)
	if _, err := e.pool.Exec(context.Background(), `ALTER TABLE approvals RENAME TO approvals_broken`); err != nil {
		t.Fatal(err)
	}
	response, body := e.do(t, "GET", "/api/v1/changesets/"+cs+"/review", "", e.authed(nil))
	expectProblem(t, response, body, 500, "INTERNAL")
}
