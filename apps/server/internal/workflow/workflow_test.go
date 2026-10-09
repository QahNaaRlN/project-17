package workflow_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

func setup(t *testing.T) *cmstest.Env { return cmstest.New(t, workflow.Register) }

func submit(e *cmstest.Env, cs uuid.UUID, seq int) (workflow.Review, error) {
	var r workflow.Review
	err := e.Do(e.Admin, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": seq}, &r)
	return r, err
}

func TestSubmitRunsChecksAndWaitsForApproval(t *testing.T) {
	e := setup(t)
	cs, _ := e.Draft(e.Admin, "v1")
	r, err := submit(e, cs, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.Changeset.State != "in_review" || *r.Risk != workflow.RiskMedium || r.RequiredApprovals != 1 {
		t.Errorf("review: %+v", r)
	}
	if len(r.Checks) != 2 || r.Checks[0].Stage != "ir" || r.Checks[0].Status != "passed" || r.Checks[1].Stage != "policy" || r.Checks[1].Status != "passed" {
		t.Errorf("проверки: %+v", r.Checks)
	}
	detail, err := workflow.GetReview(context.Background(), e.Q, e.Admin.ProjectID, cs)
	if err != nil || len(detail.Checks) != 2 || detail.RequiredApprovals != 1 {
		t.Errorf("GetReview: %+v %v", detail, err)
	}
	// После подачи операции не принимаются.
	if err := e.Do(e.Admin, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": 1, "operations": []any{
		map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "root": map[string]any{"type": "Box"}}},
	}}, nil); cmstest.Code(err) != "CHANGESET_STATE_INVALID" {
		t.Errorf("операции после подачи: %v", err)
	}
}

func TestSubmitErrors(t *testing.T) {
	e := setup(t)
	cs, _ := e.Draft(e.Admin, "v1")
	other := e.Human("other", auth.ContentWrite)
	var empty struct{ ID uuid.UUID }
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "empty"}, &empty)
	cases := []struct {
		name  string
		actor auth.Actor
		cs    uuid.UUID
		seq   int
		code  string
	}{
		{"чужой", other, cs, 1, "CHANGESET_NOT_OWNER"},
		{"устаревший seq", e.Admin, cs, 0, "CHANGESET_SEQ_CONFLICT"},
		{"пустой", e.Admin, empty.ID, 0, "CHANGESET_EMPTY"},
		{"нет Change Set", e.Admin, uuid.New(), 0, "NOT_FOUND"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := e.Do(tc.actor, "submit-changeset", map[string]any{"changesetId": tc.cs, "expectedSeq": tc.seq}, nil)
			if cmstest.Code(err) != tc.code {
				t.Errorf("код %q, ожидалось %q (%v)", cmstest.Code(err), tc.code, err)
			}
		})
	}
	if _, err := submit(e, cs, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := submit(e, cs, 1); cmstest.Code(err) != "CHANGESET_STATE_INVALID" {
		t.Errorf("повторная подача: %v", err)
	}
}

func TestPolicyCheckFailsWhenAuthorLostRight(t *testing.T) {
	e := setup(t)
	designer := e.Human("designer", auth.DesignCompose, auth.ContentWrite)
	cs, _ := e.Draft(designer, "v1")
	if _, err := e.Pool.Exec(context.Background(), "UPDATE roles SET capabilities = ARRAY['content.write'] WHERE name = 'role-designer'"); err != nil {
		t.Fatal(err)
	}
	var r workflow.Review
	e.Must(designer, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1}, &r)
	if r.Changeset.State != "failed" || r.Checks[1].Status != "failed" || !json.Valid(r.Checks[1].Details) {
		t.Errorf("review: %+v", r)
	}
	var details struct{ Violations []map[string]string }
	_ = json.Unmarshal(r.Checks[1].Details, &details)
	if len(details.Violations) != 1 || details.Violations[0]["right"] != "design.compose" {
		t.Errorf("нарушения: %+v", details)
	}
}

func TestIRCheckFailsOnInvalidWorkingVersion(t *testing.T) {
	e := setup(t)
	cs, doc := e.Draft(e.Admin, "v1")
	// Рабочую версию портим в обход операций — проверка на подаче должна это поймать.
	if _, err := e.Pool.Exec(context.Background(),
		`UPDATE object_versions SET body = jsonb_set(body, '{root}', '"n_missing"') WHERE object_id = $1`, doc); err != nil {
		t.Fatal(err)
	}
	r, err := submit(e, cs, 1)
	if err != nil || r.Changeset.State != "failed" || r.Checks[0].Status != "failed" {
		t.Errorf("review: %+v %v", r, err)
	}
}

func TestApprovalRules(t *testing.T) {
	e := setup(t)
	author := e.Human("author", auth.DesignCompose, auth.ContentPublish)
	reviewer := e.Human("reviewer", auth.ContentPublish)
	cs, _ := e.Draft(author, "v1")
	e.Must(author, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1}, nil)

	if err := e.Do(e.Admin, "approve-changeset", map[string]any{"changesetId": cs}, nil); cmstest.Code(err) != "APPROVAL_FORBIDDEN_ACTOR" {
		t.Errorf("сервис не согласует (PUB-003): %v", err)
	}
	if err := e.Do(author, "approve-changeset", map[string]any{"changesetId": cs}, nil); cmstest.Code(err) != "APPROVAL_SELF" {
		t.Errorf("автор не согласует (PUB-002): %v", err)
	}
	noRight := e.Human("viewer", auth.ContentRead)
	if err := e.Do(noRight, "approve-changeset", map[string]any{"changesetId": cs}, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Errorf("без content.publish: %v", err)
	}
	var r workflow.Review
	e.Must(reviewer, "approve-changeset", map[string]any{"changesetId": cs, "comment": "ок"}, &r)
	if r.Changeset.State != "approved" || r.Approvals != 1 {
		t.Errorf("согласование: %+v", r)
	}
	if err := e.Do(reviewer, "approve-changeset", map[string]any{"changesetId": cs}, nil); cmstest.Code(err) != "CHANGESET_STATE_INVALID" {
		t.Errorf("повторное согласование согласованного: %v", err)
	}
}

func TestTwoApproversForHighPolicy(t *testing.T) {
	e := setup(t)
	e.Must(e.Admin, "set-approval-policy", map[string]any{"low": 0, "medium": 2, "high": 3}, nil)
	a, b := e.Human("a", auth.ContentPublish), e.Human("b", auth.ContentPublish)
	cs, _ := e.Draft(e.Admin, "v1")
	r, _ := submit(e, cs, 1)
	if r.RequiredApprovals != 2 {
		t.Fatalf("требуется: %d", r.RequiredApprovals)
	}
	e.Must(a, "approve-changeset", map[string]any{"changesetId": cs}, &r)
	if r.Changeset.State != "in_review" || r.Approvals != 1 {
		t.Errorf("после первого: %+v", r)
	}
	// Повторное согласование того же человека не считается вторым.
	e.Must(a, "approve-changeset", map[string]any{"changesetId": cs}, &r)
	if r.Changeset.State != "in_review" || r.Approvals != 1 {
		t.Errorf("повтор того же согласующего: %+v", r)
	}
	e.Must(b, "approve-changeset", map[string]any{"changesetId": cs}, &r)
	if r.Changeset.State != "approved" || r.Approvals != 2 {
		t.Errorf("после второго: %+v", r)
	}
	detail, err := workflow.GetReview(context.Background(), e.Q, e.Admin.ProjectID, cs)
	if err != nil || detail.Approvals != 2 || len(detail.History) != 3 || !detail.History[2].Valid || detail.Changeset.State != "approved" {
		t.Errorf("GetReview: %+v %v", detail, err)
	}
}

func TestZeroApprovalsGoesStraightToApproved(t *testing.T) {
	e := setup(t)
	e.Must(e.Admin, "set-approval-policy", map[string]any{"low": 0, "medium": 0, "high": 1}, nil)
	cs, _ := e.Draft(e.Admin, "v1")
	if r, _ := submit(e, cs, 1); r.Changeset.State != "approved" || r.RequiredApprovals != 0 {
		t.Errorf("%+v", r)
	}
}

func TestRequestChangesAndReopen(t *testing.T) {
	e := setup(t)
	e.Must(e.Admin, "set-approval-policy", map[string]any{"low": 0, "medium": 2, "high": 2}, nil)
	reviewer, second := e.Human("reviewer", auth.ContentPublish), e.Human("second", auth.ContentPublish)
	cs, _ := e.Draft(e.Admin, "v1")
	submit(e, cs, 1)
	e.Must(second, "approve-changeset", map[string]any{"changesetId": cs}, nil)

	if err := e.Do(reviewer, "request-changes", map[string]any{"changesetId": cs}, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
		t.Errorf("комментарий обязателен: %v", err)
	}
	var r workflow.Review
	e.Must(reviewer, "request-changes", map[string]any{"changesetId": cs, "comment": "поправьте заголовок"}, &r)
	if r.Changeset.State != "changes_requested" {
		t.Errorf("%+v", r)
	}
	if err := e.Do(reviewer, "reopen-changeset", map[string]any{"changesetId": cs}, nil); cmstest.Code(err) != "CHANGESET_NOT_OWNER" {
		t.Errorf("возвращает в работу только владелец: %v", err)
	}
	e.Must(e.Admin, "reopen-changeset", map[string]any{"changesetId": cs}, &r)
	if r.Changeset.State != "open" {
		t.Errorf("%+v", r)
	}
	// Согласования сброшены (PUB-004): после повторной подачи нужны оба заново.
	r, _ = submit(e, cs, 1)
	if r.Changeset.State != "in_review" {
		t.Fatalf("%+v", r)
	}
	detail, _ := workflow.GetReview(context.Background(), e.Q, e.Admin.ProjectID, cs)
	if detail.Approvals != 0 || len(detail.History) != 2 || detail.History[0].Valid {
		t.Errorf("согласования после возврата: %+v", detail)
	}
	if err := e.Do(e.Admin, "reopen-changeset", map[string]any{"changesetId": uuid.New()}, nil); cmstest.Code(err) != "NOT_FOUND" {
		t.Errorf("%v", err)
	}
	e.Must(e.Admin, "reopen-changeset", map[string]any{"changesetId": cs}, nil)
	if err := e.Do(e.Admin, "reopen-changeset", map[string]any{"changesetId": cs}, nil); cmstest.Code(err) != "CHANGESET_STATE_INVALID" {
		t.Errorf("открытый Change Set не возвращается: %v", err)
	}
}

func TestReviewNotFound(t *testing.T) {
	e := setup(t)
	if err := e.Do(e.Human("r", auth.ContentPublish), "approve-changeset", map[string]any{"changesetId": uuid.New()}, nil); cmstest.Code(err) != "NOT_FOUND" {
		t.Errorf("%v", err)
	}
	if _, err := workflow.GetReview(context.Background(), e.Q, e.Admin.ProjectID, uuid.New()); cmstest.Code(err) != "NOT_FOUND" {
		t.Errorf("%v", err)
	}
	cs, _ := e.Draft(e.Admin, "v1")
	detail, err := workflow.GetReview(context.Background(), e.Q, e.Admin.ProjectID, cs)
	if err != nil || detail.Risk != nil || len(detail.Checks) != 0 || detail.RequiredApprovals != 0 {
		t.Errorf("до подачи: %+v %v", detail, err)
	}
}

func TestApprovalPolicy(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if p, err := workflow.LoadApprovalPolicy(ctx, e.Q, e.Admin.ProjectID); err != nil || p != workflow.DefaultApprovalPolicy {
		t.Errorf("по умолчанию: %+v %v", p, err)
	}
	for _, bad := range []map[string]int{{"low": -1, "medium": 1, "high": 2}, {"low": 0, "medium": 6, "high": 6}, {"low": 1, "medium": 0, "high": 2}, {"low": 0, "medium": 2, "high": 1}} {
		if err := e.Do(e.Admin, "set-approval-policy", bad, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
			t.Errorf("%v: %v", bad, err)
		}
	}
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 1, "medium": 1, "high": 5}, nil)
	p, _ := workflow.LoadApprovalPolicy(ctx, e.Q, e.Admin.ProjectID)
	if p.Required(workflow.RiskLow) != 1 || p.Required(workflow.RiskMedium) != 1 || p.Required(workflow.RiskHigh) != 5 {
		t.Errorf("%+v", p)
	}
	if err := e.Do(e.Human("x", auth.ContentPublish), "set-approval-policy", map[string]int{"low": 0, "medium": 0, "high": 0}, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Errorf("политику меняет администратор: %v", err)
	}
	if _, err := e.Pool.Exec(ctx, `UPDATE projects SET settings = '[]'`); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.LoadApprovalPolicy(ctx, e.Q, e.Admin.ProjectID); err == nil {
		t.Error("повреждённые настройки должны давать ошибку")
	}
}

func TestRisk(t *testing.T) {
	if workflow.Risk([]string{"entity.setFields", "localContent.set"}) != workflow.RiskLow {
		t.Error("только контент — low")
	}
	if workflow.Risk([]string{"entity.setFields", "node.rename"}) != workflow.RiskMedium {
		t.Error("структура — medium")
	}
	if workflow.Risk(nil) != workflow.RiskLow {
		t.Error("пусто — low")
	}
}
