package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/ratelimit"
)

// STU-531: people are limited on all requests and on commands, agents per session on commands.
func TestLimitRequestsByActor(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	d := Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Limiter: ratelimit.New(nil).WithClock(func() time.Time { return now }),
		Limits: ratelimit.Limits{HumanRequests: 3, HumanCommands: 1, AgentCommands: 1}}
	h := limitRequests(d)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	call := func(actor auth.Actor, method, path string) int {
		r := httptest.NewRequest(method, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, actor)))
		return w.Code
	}
	human := auth.Actor{ID: uuid.New(), Kind: auth.ActorHuman}
	if call(human, "POST", "/api/v1/commands/x") != 204 || call(human, "POST", "/api/v1/commands/x") != 429 {
		t.Fatal("human commands")
	}
	if call(human, "GET", "/api/v1/changesets") != 204 || call(human, "GET", "/api/v1/changesets") != 429 {
		t.Fatal("human requests")
	}
	session, other := uuid.New(), uuid.New()
	agent := auth.Actor{ID: uuid.New(), Kind: auth.ActorAgent, AgentSessionID: &session}
	if call(agent, "GET", "/api/v1/changesets") != 204 || call(agent, "POST", "/api/v1/commands/x") != 204 || call(agent, "POST", "/api/v1/commands/x") != 429 {
		t.Fatal("agent session")
	}
	agent.AgentSessionID = &other
	if call(agent, "POST", "/api/v1/commands/x") != 204 {
		t.Fatal("sessions are independent")
	}
	agent.AgentSessionID = nil
	if call(agent, "POST", "/api/v1/commands/x") != 204 || call(agent, "POST", "/api/v1/commands/x") != 429 {
		t.Fatal("agent without session")
	}
}
