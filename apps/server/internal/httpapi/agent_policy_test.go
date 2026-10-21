package httpapi_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/agents"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/httpapi"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

// STU-431/433/434: policy Query routes and the refusal an MCP client receives.
func TestSTU431AgentPolicyHTTP(t *testing.T) {
	service := &agents.Service{DefaultModel: "test/model"}
	e := cmstest.New(t, service.Register)
	service.Pool = e.Pool
	human := e.Human("Owner", auth.AgentDelegate, auth.DesignRead, auth.DesignCompose)
	var cs changes.Changeset
	e.Must(human, "create-changeset", map[string]any{"title": "Policy", "targets": []string{"staging"}}, &cs)
	var applied changes.ApplyResult
	e.Must(human, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 0, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": "/checkout", "root": map[string]any{"id": "n_root", "type": "Box"}}}}}, &applied)
	var registered struct{ ID uuid.UUID }
	e.Must(e.Admin, "register-agent", map[string]any{"name": "copy", "description": "test"}, &registered)
	agentSecret, agentHash := auth.NewToken(auth.AgentTokenPrefix)
	e.Must(human, "start-agent-session", map[string]any{"tokenHash": agentHash, "agentId": registered.ID, "changesetIds": []uuid.UUID{cs.ID}, "rights": []string{"design.read", "design.compose"}, "ttlSeconds": 60, "opsQuota": 5}, nil)
	adminSecret, adminHash := auth.NewToken(auth.ServiceTokenPrefix)
	e.Exec(`INSERT INTO api_tokens(id,project_id,actor_id,token_hash,scopes,expires_at) VALUES($1,$2,$3,$4,ARRAY['*'],now()+interval '1 hour')`, uuid.New(), e.Admin.ProjectID, e.Admin.ID, adminHash)
	e.Exec(`INSERT INTO agent_policy_versions(project_id,version,body,hash,reason) VALUES($1,1,$2,'sha256:x','fixture')`, e.Admin.ProjectID,
		`{"version":1,"rules":[{"id":"no-checkout","subject":{"agent":"*"},"deny":["design.compose"],"when":{"pathPrefix":["/checkout"]}}],"risk":[],"approvals":[]}`)
	server := httptest.NewServer(httpapi.NewRouter(httpapi.Deps{Pool: e.Pool, Bus: e.Bus, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Agents: service}))
	defer server.Close()
	request := func(method, path, body, token string) (int, map[string]any) {
		t.Helper()
		r, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-CMS-Project", "store")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var value map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&value)
		return resp.StatusCode, value
	}
	if status, value := request("GET", "/api/v1/agent-policy", "", adminSecret); status != 200 || value["version"] != float64(1) || value["known"] == nil {
		t.Fatal(status, value)
	}
	if status, value := request("GET", "/api/v1/agent-policy/versions", "", adminSecret); status != 200 || len(value["items"].([]any)) != 1 {
		t.Fatal(status, value)
	}
	if status, value := request("POST", "/api/v1/agent-policy/explain", `{"situation":{"agent":"copy","right":"design.compose","path":"/checkout"}}`, adminSecret); status != 200 || value["decision"].(map[string]any)["ruleId"] != "no-checkout" {
		t.Fatal(status, value)
	}
	if status, value := request("POST", "/api/v1/agent-policy/explain", `{`, adminSecret); status != 400 || value["code"] != "PAYLOAD_INVALID" {
		t.Fatal(status, value)
	}
	if status, _ := request("GET", "/api/v1/agent-policy", "", agentSecret); status != 403 {
		t.Fatal("agents never read the policy", status)
	}
	call, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "update_props", "arguments": map[string]any{
		"changesetId": cs.ID, "documentId": applied.Operations[0].Target, "expectedSeq": 1, "reason": "edit", "payload": map[string]any{"nodeId": "n_root", "set": map[string]any{"as": "section"}}}}})
	_, value := request("POST", "/mcp", string(call), agentSecret)
	result := value["result"].(map[string]any)["structuredContent"].(map[string]any)
	if result["code"] != "POLICY_DENIED" || !strings.Contains(result["detail"].(string), "no-checkout") {
		t.Fatal("the client sees the rule", value)
	}
}
