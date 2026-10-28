package httpapi_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/agents"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/httpapi"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// FR-012: agent operations change the same IR document as Studio.
// API-002: MCP and Studio call the same Command Bus handlers.
// AGT-032: tools refuse operations invalid in the effective mode.
// OPS-011: HTTP, MCP and the orchestrator call the Command Bus the same way.
func TestMCPDelegatedHTTP(t *testing.T) { // AGT-010/011/030/051/052.
	service := &agents.Service{DefaultModel: "test/model"}
	e := cmstest.New(t, service.Register)
	service.Pool = e.Pool
	human := e.Human("Owner", auth.AgentDelegate, auth.DesignRead, auth.DesignCompose)
	var cs changes.Changeset
	e.Must(human, "create-changeset", map[string]any{"title": "MCP", "targets": []string{"staging"}}, &cs)
	var applied changes.ApplyResult
	e.Must(human, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 0, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": "/mcp", "root": map[string]any{"id": "n_root", "type": "Box"}}}}}, &applied)
	doc := applied.Operations[0].Target
	var registered struct{ ID uuid.UUID }
	e.Must(e.Admin, "register-agent", map[string]any{"name": "MCP agent", "description": "test"}, &registered)
	var session agents.Session
	agentSecret, agentHash := auth.NewToken(auth.AgentTokenPrefix)
	e.Must(human, "start-agent-session", map[string]any{"tokenHash": agentHash, "agentId": registered.ID, "changesetIds": []uuid.UUID{cs.ID}, "rights": []string{"design.read", "design.compose"}, "ttlSeconds": 60, "opsQuota": 5}, &session)
	session.Token = agentSecret
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(httpapi.NewRouter(httpapi.Deps{Pool: e.Pool, Bus: e.Bus, Log: logger, Agents: service}))
	defer server.Close()
	request := func(method, path, body string, extra map[string]string) (int, map[string]any) {
		t.Helper()
		r, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+session.Token)
		r.Header.Set("X-CMS-Project", "store")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		for k, v := range extra {
			r.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var value map[string]any
		json.NewDecoder(resp.Body).Decode(&value)
		return resp.StatusCode, value
	}
	base := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	for _, test := range []struct {
		headers map[string]string
		status  int
	}{{map[string]string{"Origin": "https://evil.test"}, 403}, {map[string]string{"Accept": "application/json"}, 406}, {map[string]string{"Content-Type": "text/plain"}, 415}, {map[string]string{"MCP-Protocol-Version": "bad"}, 400}, {map[string]string{"X-CMS-Project": "other"}, 403}} {
		status, _ := request("POST", "/mcp", base, test.headers)
		if status != test.status {
			t.Fatal(status, test)
		}
	}
	status, value := request("POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, nil)
	if status != 200 || value["result"].(map[string]any)["protocolVersion"] != "2025-06-18" {
		t.Fatal(value)
	}
	_, value = request("POST", "/mcp", `{"jsonrpc":"2.0","id":"two","method":"tools/list"}`, nil)
	if len(value["result"].(map[string]any)["tools"].([]any)) < 8 {
		t.Fatal(value)
	}
	for _, body := range []string{base, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"unknown"}}`} {
		_, value = request("POST", "/mcp", body, nil)
		if value["result"] == nil {
			t.Fatal(value)
		}
	}
	for _, body := range []string{`bad`, `[]`, `{"jsonrpc":"1.0","id":1,"method":"ping"}`, `{"jsonrpc":"2.0","method":"ping"}`, `{"jsonrpc":"2.0","id":true,"method":"ping"}`, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":true}`, `{"jsonrpc":"2.0","id":1,"method":"no"}`, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{}}`, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"publish","arguments":{}}}`} {
		_, value = request("POST", "/mcp", body, nil)
		if value["error"] == nil {
			t.Fatal(body, value)
		}
	}
	if status, _ := request("POST", "/mcp", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil); status != 202 {
		t.Fatal(status)
	}
	call := func(name string, args any) map[string]any {
		raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": uuid.NewString(), "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
		_, value := request("POST", "/mcp", string(raw), nil)
		return value["result"].(map[string]any)
	}
	value = call("get_page_structure", map[string]any{"changesetId": cs.ID, "documentId": doc})
	if value["isError"] != false || value["structuredContent"] == nil {
		t.Fatal(value)
	}
	value = call("get_changeset", map[string]any{"changesetId": uuid.New()})
	if value["isError"] != true {
		t.Fatal(value)
	}
	args := map[string]any{"changesetId": cs.ID, "documentId": doc, "expectedSeq": 1, "reason": "semantic edit", "payload": map[string]any{"nodeId": "n_root", "set": map[string]any{"as": "section"}}}
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "retry-1", "method": "tools/call", "params": map[string]any{"name": "update_props", "arguments": args}})
	for i := 0; i < 2; i++ {
		_, value = request("POST", "/mcp", string(raw), nil)
		if value["result"].(map[string]any)["isError"] != false {
			t.Fatal(value)
		}
	}
	var used int
	e.Pool.QueryRow(context.Background(), `SELECT ops_used FROM agent_sessions WHERE id=$1`, session.ID).Scan(&used)
	if used != 1 {
		t.Fatal("HTTP retry charged twice", used)
	}
	if status, _ := request("GET", "/mcp", "", nil); status != 405 {
		t.Fatal(status)
	}
	secret, hash := auth.NewToken(auth.ServiceTokenPrefix)
	e.Exec(`INSERT INTO api_tokens(id,project_id,actor_id,token_hash,scopes,expires_at) VALUES($1,$2,$3,$4,ARRAY['*'],now()+interval '1 hour')`, uuid.New(), human.ProjectID, human.ID, hash)
	headers := map[string]string{"Authorization": "Bearer " + secret}
	if status, _ := request("POST", "/mcp", base, headers); status != 403 {
		t.Fatal(status)
	}
	if status, value := request("GET", "/api/v1/agent-config", "", headers); status != 200 || value["configured"] != false || value["canDelegate"] != true {
		t.Fatal(status, value)
	}
	if status, _ := request("GET", "/api/v1/agent-runs?documentId=bad&changesetId="+cs.ID.String(), "", headers); status != 400 {
		t.Fatal(status)
	}
	if status, _ := request("GET", "/api/v1/agent-runs?documentId="+doc.String()+"&changesetId=bad", "", headers); status != 400 {
		t.Fatal(status)
	}
	if status, value := request("GET", "/api/v1/agent-runs?documentId="+doc.String()+"&changesetId="+cs.ID.String()+"&environment=staging", "", headers); status != 200 || len(value["items"].([]any)) != 0 {
		t.Fatal(status, value)
	}
	if status, _ := request("GET", "/api/v1/agent-runs/"+uuid.NewString(), "", headers); status != 403 {
		t.Fatal(status)
	}
	if status, _ := request("GET", "/api/v1/agent-runs/bad", "", headers); status != 400 {
		t.Fatal(status)
	}
	// Agent step events contain only the current user's signals, never task text.
	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(streamCtx, "GET", server.URL+"/api/v1/events?documentId="+doc.String()+"&changesetId="+cs.ID.String()+"&environment=staging", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("X-CMS-Project", "store")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(resp.Body)
	var event bytes.Buffer
	for scanner.Scan() {
		line := scanner.Text()
		event.WriteString(line)
		if line == "" && strings.Contains(event.String(), "event: agent") {
			break
		}
	}
	resp.Body.Close()
	cancel()
	if !strings.Contains(event.String(), `"items":[]`) {
		t.Fatal(event.String())
	}
}
