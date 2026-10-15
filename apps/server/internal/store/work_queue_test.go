package store

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
	"testing"
	"time"
)

func TestMain(m *testing.M) { pgtest.Main(m) }
func TestQueuePlanAtTenThousandWorks(t *testing.T) {
	pool := pgtest.NewDB(t)
	ctx := context.Background()
	project, actor := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO projects(id,slug,name) VALUES($1,'queue-plan','Queue plan')`, project); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO actors(id,project_id,kind,display_name) VALUES($1,$2,'service','Plan author')`, actor, project); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO changesets(id,project_id,kind,title,owner_id,state,targets,created_at) SELECT gen_random_uuid(),$1,'standard',CASE WHEN n=10000 THEN 'needle' ELSE 'filler' END,$2,'open',ARRAY['staging'],now()-n*interval '1 second' FROM generate_series(1,10000) n`, project, actor); err != nil {
		t.Fatal(err)
	}
	// Explain the exact generated query with the same parameter order as WorkQueue.
	rows, err := pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+workQueue,
		project, "staging", uuid.Nil, actor, false, "all", "mine", true, true, "all", "needle", false, time.Unix(0, 0), uuid.Nil, int32(51))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan json.RawMessage
	if !rows.Next() {
		t.Fatal("missing plan")
	}
	if err := rows.Scan(&plan); err != nil {
		t.Fatal(err)
	}
	var parsed []struct {
		Plan struct {
			ActualRows int `json:"Actual Rows"`
		}
		Time float64 `json:"Execution Time"`
	}
	if err := json.Unmarshal(plan, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 || parsed[0].Plan.ActualRows != 1 {
		t.Fatal(string(plan))
	}
	t.Logf("exact query, 10000 works, oldest match: %.2f ms", parsed[0].Time)
}
