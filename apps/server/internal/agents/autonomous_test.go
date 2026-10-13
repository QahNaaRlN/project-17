package agents

import (
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"sync"
	"testing"
)

func TestAutonomousSessionDoesNotInventAHumanDelegator(t *testing.T) { // AGT-021/022.
	e, _, _, _, _ := setupAgent(t)
	id := registered(t, e)
	cs, doc := e.Draft(e.Admin, "Service draft")
	session := delegation(t, e, e.Admin, id, cs, 5)
	a := agentActor(t, e, session)
	if a.OnBehalfOf != nil || a.DelegatorID == nil || *a.DelegatorID != e.Admin.ID {
		t.Fatal("invented human attribution", a)
	}
	if _, err := dispatch(e, a, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": 1, "operations": []any{map[string]any{"type": "node.rename", "target": doc, "payload": map[string]any{"nodeId": "n_root", "name": "Autonomous"}}}}, "autonomous-edit", "Service explicitly delegated composition"); err != nil {
		t.Fatal(err)
	}
	var row struct{ Result changes.Changeset }
	response, err := dispatch(e, a, "create-changeset", map[string]any{"title": "Autonomous draft", "targets": []string{"staging"}}, "autonomous-create", "Create scoped agent-owned CS")
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(response.Body, &row)
	if row.Result.OwnerID != id {
		t.Fatal(row)
	}
	current, err := auth.Authenticate(ctx, e.Q, "Bearer "+session.Token)
	if err != nil {
		t.Fatal(err)
	}
	if err = changes.RequireOwner(store.Changeset{ID: row.Result.ID, OwnerID: id}, current); err != nil {
		t.Fatal(err)
	}
	raw, err := e.Q.GetAgentSession(ctx, store.GetAgentSessionParams{ID: session.ID, ProjectID: e.Admin.ProjectID})
	if err != nil {
		t.Fatal(err)
	}
	raw.StartedBy = uuid.New()
	if _, err = auth.SessionActor(ctx, e.Q, raw, false); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("missing delegator accepted", err)
	}
}
func TestConcurrentAgentCommandsCannotOverspendSessionQuota(t *testing.T) { // AGT-023: row lock, not a browser convention.
	e, _, h, cs, doc := setupAgent(t)
	id := registered(t, e)
	session := delegation(t, e, h, id, cs, 1)
	actor := agentActor(t, e, session)
	results := make(chan string, 2)
	var wait sync.WaitGroup
	for n := 0; n < 2; n++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := dispatch(e, actor, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": 1, "operations": []any{map[string]any{"type": "node.rename", "target": doc, "payload": map[string]any{"nodeId": "n_root", "name": "One write"}}}}, uuid.NewString(), "Concurrent delegated edit")
			results <- cmstest.Code(err)
		}()
	}
	wait.Wait()
	close(results)
	success, quota := 0, 0
	for code := range results {
		switch code {
		case "":
			success++
		case "AGENT_QUOTA_EXCEEDED":
			quota++
		default:
			t.Fatal(code)
		}
	}
	if success != 1 || quota != 1 {
		t.Fatal("quota race", success, quota)
	}
	var used, seq int
	e.Pool.QueryRow(ctx, `SELECT ops_used FROM agent_sessions WHERE id=$1`, session.ID).Scan(&used)
	e.Pool.QueryRow(ctx, `SELECT seq FROM changesets WHERE id=$1`, cs).Scan(&seq)
	if used != 1 || seq != 2 {
		t.Fatal(used, seq)
	}
}
func TestAgentWriteRollbackIncludesQuotaAndHistory(t *testing.T) {
	for _, mode := range []string{"quota-write", "session-read", "rights"} {
		t.Run(mode, func(t *testing.T) {
			e, _, h, cs, doc := setupAgent(t)
			id := registered(t, e)
			session := delegation(t, e, h, id, cs, 5)
			a := agentActor(t, e, session)
			switch mode {
			case "quota-write":
				e.Break("agent_sessions", "UPDATE")
			case "session-read":
				e.Break("agent_sessions", "")
			case "rights":
				e.Must(e.Admin, "set-agent-policy", auth.AgentPolicy{Allow: []string{string(auth.DesignRead)}}, nil)
			}
			payload := map[string]any{"changesetId": cs, "expectedSeq": 1, "operations": []any{map[string]any{"type": "node.rename", "target": doc, "payload": map[string]any{"nodeId": "n_root", "name": "Should not persist"}}}}
			if _, err := dispatch(e, a, "apply-operations", payload, "rollback", "Explicit agent edit"); err == nil {
				t.Fatal("failure reported success")
			}
			var seq, ops int
			e.Pool.QueryRow(ctx, `SELECT seq FROM changesets WHERE id=$1`, cs).Scan(&seq)
			e.Pool.QueryRow(ctx, `SELECT count(*) FROM operations WHERE changeset_id=$1`, cs).Scan(&ops)
			if seq != 1 || ops != 1 {
				t.Fatal("failed transaction left history", seq, ops)
			}
		})
	}
}
