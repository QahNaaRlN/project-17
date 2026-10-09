package workflow_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

// Сбои БД на каждом шаге команд прерывают их ошибкой, а не доменным ответом.
func TestSubmitDatabaseFaults(t *testing.T) {
	for name, brk := range map[string]func(*cmstest.Env){
		"changeset_objects": func(e *cmstest.Env) { e.Break("changeset_objects", "") },
		"object_versions":   func(e *cmstest.Env) { e.Break("object_versions", "") },
		"operations":        func(e *cmstest.Env) { e.Break("operations", "") },
		"role_bindings":     func(e *cmstest.Env) { e.Break("role_bindings", "") },
		"checks":            func(e *cmstest.Env) { e.Break("checks", "INSERT") },
		"settings":          func(e *cmstest.Env) { e.Exec("UPDATE projects SET settings = '[]'") },
		"changesets":        func(e *cmstest.Env) { e.Break("changesets", "UPDATE") },
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			cs, _ := e.Draft(e.Admin, "v1")
			brk(e)
			_, err := submit(e, cs, 1)
			cmstest.ExpectDBError(t, err)
		})
	}
}

func TestReviewDatabaseFaults(t *testing.T) {
	for name, brk := range map[string]func(*cmstest.Env){
		"operations": func(e *cmstest.Env) { e.Break("operations", "") },
		"approvals":  func(e *cmstest.Env) { e.Break("approvals", "INSERT") },
		"settings":   func(e *cmstest.Env) { e.Exec("UPDATE projects SET settings = '[]'") },
		"count":      func(e *cmstest.Env) { e.Exec("ALTER TABLE approvals RENAME COLUMN invalidated_at TO broken") },
		"changesets": func(e *cmstest.Env) { e.Break("changesets", "UPDATE") },
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			reviewer := e.Human("reviewer", auth.ContentPublish)
			cs, _ := e.Draft(e.Admin, "v1")
			submit(e, cs, 1)
			brk(e)
			cmstest.ExpectDBError(t, e.Do(reviewer, "approve-changeset", map[string]any{"changesetId": cs}, nil))
		})
	}
}

func TestReopenDatabaseFaults(t *testing.T) {
	for _, table := range []string{"approvals", "changesets"} {
		t.Run(table, func(t *testing.T) {
			e := setup(t)
			cs, _ := e.Draft(e.Admin, "v1")
			submit(e, cs, 1)
			e.Break(table, "UPDATE")
			cmstest.ExpectDBError(t, e.Do(e.Admin, "reopen-changeset", map[string]any{"changesetId": cs}, nil))
		})
	}
}

func TestSetPolicyDatabaseFaults(t *testing.T) {
	for name, brk := range map[string]func(*cmstest.Env){
		"read":     func(e *cmstest.Env) { e.Break("projects", "") },
		"write":    func(e *cmstest.Env) { e.Break("projects", "UPDATE") },
		"settings": func(e *cmstest.Env) { e.Exec("UPDATE projects SET settings = '[]'") },
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			brk(e)
			cmstest.ExpectDBError(t, e.Do(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 0, "high": 0}, nil))
		})
	}
}

func TestGetReviewDatabaseFaults(t *testing.T) {
	for name, brk := range map[string]func(*cmstest.Env){
		"changesets": func(e *cmstest.Env) { e.Break("changesets", "") },
		"checks":     func(e *cmstest.Env) { e.Break("checks", "") },
		"settings":   func(e *cmstest.Env) { e.Exec("UPDATE projects SET settings = '[]'") },
		"approvals":  func(e *cmstest.Env) { e.Break("approvals", "") },
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			cs, _ := e.Draft(e.Admin, "v1")
			submit(e, cs, 1)
			brk(e)
			_, err := workflow.GetReview(context.Background(), e.Q, e.Admin.ProjectID, cs)
			cmstest.ExpectDBError(t, err)
		})
	}
	e := setup(t)
	e.Pool.Close()
	if _, err := workflow.LoadApprovalPolicy(context.Background(), e.Q, uuid.New()); err == nil {
		t.Error("закрытый пул")
	}
}
