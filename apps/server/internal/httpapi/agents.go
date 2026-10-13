package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/agents"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func agentRoutes(r chi.Router, d Deps) {
	if d.Agents == nil {
		return
	}
	r.Get("/agent-config", func(w http.ResponseWriter, r *http.Request) {
		value, err := d.Agents.Config(r.Context(), actorFrom(r.Context()))
		respond(w, r, d, value, err)
	})
	r.Get("/agent-runs", func(w http.ResponseWriter, r *http.Request) {
		doc, err := uuid.Parse(r.URL.Query().Get("documentId"))
		if err != nil {
			writeError(w, r, d.Log, badParam("documentId", "UUID"))
			return
		}
		cs, err := uuid.Parse(r.URL.Query().Get("changesetId"))
		if err != nil {
			writeError(w, r, d.Log, badParam("changesetId", "UUID"))
			return
		}
		value, err := d.Agents.List(r.Context(), actorFrom(r.Context()), agents.Scope{DocumentID: doc, ChangesetID: cs, Environment: r.URL.Query().Get("environment")})
		respond(w, r, d, map[string]any{"items": value}, err)
	})
	r.Get("/agent-runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := uuidParam(w, r, d, "id")
		if !ok {
			return
		}
		value, err := d.Agents.Detail(r.Context(), actorFrom(r.Context()), id)
		respond(w, r, d, value, err)
	})
}
func mcpHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host != r.Host || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
				w.WriteHeader(403)
				return
			}
		}
		a := actorFrom(r.Context())
		if a.Kind != auth.ActorAgent || d.Agents == nil {
			w.WriteHeader(403)
			return
		}
		accept := r.Header.Get("Accept")
		if !strings.Contains(accept, "application/json") || !strings.Contains(accept, "text/event-stream") {
			w.WriteHeader(406)
			return
		}
		if version := r.Header.Get("MCP-Protocol-Version"); version != "" && version != "2025-03-26" && version != "2025-06-18" {
			w.WriteHeader(400)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			w.WriteHeader(415)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxCommandBody)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(413)
			return
		}
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		w.Header().Set("Cache-Control", "no-store")
		fail := func(code int, message string) {
			id := req.ID
			if len(id) == 0 {
				id = json.RawMessage(`null`)
			}
			writeJSON(w, 200, map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
		}
		if json.Unmarshal(raw, &req) != nil || req.JSONRPC != "2.0" || req.Method == "" {
			fail(-32600, "Invalid Request")
			return
		}
		if len(req.ID) == 0 {
			if strings.HasPrefix(req.Method, "notifications/") {
				w.WriteHeader(202)
				return
			}
			fail(-32600, "Request id required")
			return
		}
		if string(req.ID) != "null" {
			var id any
			if json.Unmarshal(req.ID, &id) != nil {
				fail(-32600, "Invalid id")
				return
			}
			switch id.(type) {
			case string, float64:
			default:
				fail(-32600, "Invalid id")
				return
			}
		}
		var result any
		switch req.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			if json.Unmarshal(req.Params, &p) != nil {
				fail(-32602, "Invalid params")
				return
			}
			version := p.ProtocolVersion
			if version != "2025-03-26" && version != "2025-06-18" {
				version = "2025-06-18"
			}
			result = map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "project-17", "version": d.Version}}
		case "ping":
			result = map[string]any{}
		case "tools/list":
			result = map[string]any{"tools": agents.Tools()}
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if json.Unmarshal(req.Params, &p) != nil || p.Name == "" {
				fail(-32602, "Invalid params")
				return
			}
			known := false
			for _, t := range agents.Tools() {
				if t.Name == p.Name {
					known = true
				}
			}
			if !known {
				fail(-32602, "Unknown tool")
				return
			}
			key := sha256.Sum256(append(append([]byte(p.Name), req.ID...), p.Arguments...))
			value, err := d.Agents.Call(r.Context(), a, p.Name, p.Arguments, fmt.Sprintf("mcp:%x", key), nil)
			isError := err != nil
			if isError {
				value = map[string]any{"code": "TOOL_FAILED"}
				var problem *commandbus.Error
				if errors.As(err, &problem) {
					value = map[string]any{"code": problem.Code, "diagnostics": problem.Params}
				}
			}
			data, _ := json.Marshal(value)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(data)}}, "structuredContent": value, "isError": isError}
		default:
			fail(-32601, "Method not found")
			return
		}
		writeJSON(w, 200, map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
}
