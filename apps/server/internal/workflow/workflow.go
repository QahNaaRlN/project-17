// Package workflow — подача Change Set на проверку, проверки, уровень риска и согласование
// (docs/spec/06-changes-publishing.md §3, §5).
package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Уровни риска (§5.2).
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// ApprovalPolicy — число согласований по уровню риска (PUB-001).
type ApprovalPolicy struct {
	Low    int `json:"low"`
	Medium int `json:"medium"`
	High   int `json:"high"`
}

// DefaultApprovalPolicy — значения спецификации по умолчанию.
var DefaultApprovalPolicy = ApprovalPolicy{Low: 0, Medium: 1, High: 2}

// Required — число согласований для уровня риска.
func (p ApprovalPolicy) Required(risk string) int {
	switch risk {
	case RiskLow:
		return p.Low
	case RiskHigh:
		return p.High
	}
	return p.Medium
}

type projectSettings struct {
	Approvals *ApprovalPolicy `json:"approvals,omitempty"`
}

// LoadApprovalPolicy читает политику согласований проекта.
func LoadApprovalPolicy(ctx context.Context, q *store.Queries, projectID uuid.UUID) (ApprovalPolicy, error) {
	raw, err := q.GetProjectSettings(ctx, projectID)
	if err != nil {
		return ApprovalPolicy{}, err
	}
	var s projectSettings
	if err := json.Unmarshal(raw, &s); err != nil {
		return ApprovalPolicy{}, fmt.Errorf("workflow: settings проекта: %w", err)
	}
	if s.Approvals == nil {
		return DefaultApprovalPolicy, nil
	}
	return *s.Approvals, nil
}

// contentOps — операции, меняющие только контент (риск low, §5.2). Они появятся вместе со
// схемами контента; пока все операции — над документами, их риск не ниже medium.
var contentOps = []string{"entity.setFields", "localContent.set", "localContent.remove", "asset.updateMeta"}

// Risk — уровень риска по типам операций Change Set (§5.2). Правила уровня high (зоны STRICT,
// анализ влияния, схемы) добавятся вместе с этими функциями.
func Risk(opTypes []string) string {
	for _, t := range opTypes {
		if !slices.Contains(contentOps, t) {
			return RiskMedium
		}
	}
	return RiskLow
}

// Register регистрирует команды модуля.
func Register(bus *commandbus.Bus) {
	commandbus.Register(bus, commandbus.Command[submitPayload, Review]{
		Name:      "submit-changeset",
		Authorize: func(actor auth.Actor, _ submitPayload) error { return nil }, // владение проверяет Handle
		Handle:    handleSubmit,
	})
	commandbus.Register(bus, commandbus.Command[reviewPayload, Review]{
		Name:  "approve-changeset",
		Right: auth.ContentPublish,
		Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p reviewPayload) (Review, error) {
			return review(ctx, tx, a, p, "approve")
		},
	})
	commandbus.Register(bus, commandbus.Command[reviewPayload, Review]{
		Name:     "request-changes",
		Right:    auth.ContentPublish,
		Validate: requireComment,
		Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p reviewPayload) (Review, error) {
			return review(ctx, tx, a, p, "request_changes")
		},
	})
	commandbus.Register(bus, commandbus.Command[changesetRef, Review]{
		Name:      "reopen-changeset",
		Authorize: func(actor auth.Actor, _ changesetRef) error { return nil }, // владение проверяет Handle
		Handle:    handleReopen,
	})
	commandbus.Register(bus, commandbus.Command[rebasePayload, RebaseOutcome]{
		Name:      "rebase-changeset",
		Authorize: func(actor auth.Actor, _ rebasePayload) error { return nil }, // владение проверяет Handle
		Validate:  validateRebase,
		Handle:    handleRebase,
	})
	commandbus.Register(bus, commandbus.Command[ApprovalPolicy, ApprovalPolicy]{
		Name:     "set-approval-policy",
		Right:    auth.ProjectAdmin,
		Validate: validatePolicy,
		Handle:   handleSetPolicy,
	})
}

type changesetRef struct {
	ChangesetID uuid.UUID `json:"changesetId"`
}

// Check — результат этапа проверки.
type Check struct {
	Stage    string          `json:"stage"`
	Status   string          `json:"status"`
	Blocking bool            `json:"blocking"`
	Details  json.RawMessage `json:"details"`
}

// Review — состояние Change Set на проверке.
type Review struct {
	Changeset         changes.Changeset `json:"changeset"`
	Risk              *string           `json:"risk"`
	RequiredApprovals int               `json:"requiredApprovals"`
	Approvals         int               `json:"approvals"`
	Checks            []Check           `json:"checks"`
}

// --- submit-changeset ---------------------------------------------------------------

type submitPayload struct {
	ChangesetID uuid.UUID `json:"changesetId"`
	ExpectedSeq int32     `json:"expectedSeq"`
}

func handleSubmit(ctx context.Context, tx pgx.Tx, actor auth.Actor, p submitPayload) (Review, error) {
	q := store.New(tx)
	cs, err := changes.Lock(ctx, q, actor.ProjectID, p.ChangesetID)
	if err != nil {
		return Review{}, err
	}
	if err := changes.RequireOwner(cs, actor); err != nil {
		return Review{}, err
	}
	if err := changes.RequireState(cs, "Подача на проверку", "open"); err != nil {
		return Review{}, err
	}
	if cs.Seq != p.ExpectedSeq {
		return Review{}, commandbus.NewError(http.StatusConflict, "CHANGESET_SEQ_CONFLICT", "Change Set изменился",
			fmt.Sprintf("Ожидался seq %d, текущий %d", p.ExpectedSeq, cs.Seq))
	}
	if cs.HasConflicts {
		return Review{}, commandbus.NewError(http.StatusConflict, "CHANGESET_HAS_CONFLICTS", "Есть неразрешённые конфликты",
			"Разрешите конфликты rebase (rebase-changeset с resolutions) перед подачей")
	}
	if err := requireNoRebase(ctx, q, cs.ID); err != nil {
		return Review{}, err
	}
	objects, err := q.ListChangesetObjects(ctx, cs.ID)
	if err != nil {
		return Review{}, err
	}
	if len(objects) == 0 {
		// Операций нет или все исключены при rebase.
		return Review{}, commandbus.NewError(http.StatusConflict, "CHANGESET_EMPTY", "Change Set пуст",
			"Нечего отправлять на проверку: в Change Set нет изменённых объектов")
	}
	ev, err := evaluate(ctx, q, actor.ProjectID, cs.ID)
	if err != nil {
		return Review{}, err
	}
	state := "in_review"
	if failed(ev.checks) {
		state = "failed"
	} else if ev.required == 0 {
		state = "approved"
	}
	if err := q.SubmitChangeset(ctx, store.SubmitChangesetParams{ID: cs.ID, State: state, Risk: &ev.risk, ContentHash: ev.hash, Targets: []string{}}); err != nil {
		return Review{}, err
	}
	checks, risk, hash, required := ev.checks, ev.risk, ev.hash, ev.required
	cs.State, cs.Risk, cs.ContentHash = state, &risk, hash
	return Review{Changeset: changes.ToChangeset(cs), Risk: &risk, RequiredApprovals: required, Checks: checks}, nil
}

// evaluation — результат pipeline проверок над текущим содержимым Change Set.
type evaluation struct {
	checks   []Check
	hash     []byte
	risk     string
	required int
}

// evaluate выполняет проверки (§5.1), сохраняет их и вычисляет риск и число согласований.
func evaluate(ctx context.Context, q *store.Queries, projectID, changesetID uuid.UUID) (evaluation, error) {
	versions, err := q.ChangesetWorkingVersions(ctx, changesetID)
	if err != nil {
		return evaluation{}, err
	}
	hash := contentHash(versions)
	opActors, err := q.ChangesetOperationActors(ctx, changesetID)
	if err != nil {
		return evaluation{}, err
	}
	checks := []Check{irCheck(versions)}
	policy, err := policyCheck(ctx, q, projectID, opActors)
	if err != nil {
		return evaluation{}, err
	}
	checks = append(checks, policy)
	for _, c := range checks {
		if err := q.InsertCheck(ctx, store.InsertCheckParams{
			ID: uuid.Must(uuid.NewV7()), ChangesetID: changesetID, ContentHash: hash, Stage: c.Stage,
			Status: c.Status, Blocking: c.Blocking, Details: c.Details,
		}); err != nil {
			return evaluation{}, err
		}
	}
	types := make([]string, len(opActors))
	for i, a := range opActors {
		types[i] = a.Type
	}
	risk := Risk(types)
	approvalPolicy, err := LoadApprovalPolicy(ctx, q, projectID)
	if err != nil {
		return evaluation{}, err
	}
	return evaluation{checks: checks, hash: hash, risk: risk, required: approvalPolicy.Required(risk)}, nil
}

func failed(checks []Check) bool {
	for _, c := range checks {
		if c.Blocking && c.Status == "failed" {
			return true
		}
	}
	return false
}

// requireNoRebase — head объектов не менялся с начала работы (§4).
func requireNoRebase(ctx context.Context, q *store.Queries, changesetID uuid.UUID) error {
	stale, err := q.ChangesetBaseMismatches(ctx, changesetID)
	if err != nil {
		return err
	}
	if len(stale) > 0 {
		return commandbus.NewError(http.StatusConflict, "REBASE_REQUIRED", "Требуется rebase",
			"Head объектов изменился после начала работы над Change Set").
			WithParams(map[string]any{"objects": stale})
	}
	return nil
}

// contentHash — SHA-256 пар «объект → хэш рабочей версии» по порядку ID (PUB-004).
func contentHash(versions []store.ChangesetWorkingVersionsRow) []byte {
	h := sha256.New()
	for _, v := range versions {
		h.Write(v.ObjectID[:])
		h.Write(v.BodyHash)
	}
	return h.Sum(nil)
}

func decode(raw []byte) any {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		panic(err) // тела версий записывает сервер
	}
	return v
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// irCheck — этап 2 pipeline: валидация документов (пока уровень L1).
func irCheck(versions []store.ChangesetWorkingVersionsRow) Check {
	problems := map[string][]ir.Diagnostic{}
	for _, v := range versions {
		if res := ir.ValidateDocument(decode(v.Body)); !res.Valid {
			problems[v.ObjectID.String()] = res.Diagnostics
		}
	}
	status := "passed"
	if len(problems) > 0 {
		status = "failed"
	}
	return Check{Stage: "ir", Status: status, Blocking: true, Details: mustJSON(map[string]any{"documents": problems})}
}

// policyCheck — этап 5: у каждого автора операций есть право на её тип на момент подачи.
func policyCheck(ctx context.Context, q *store.Queries, projectID uuid.UUID, opActors []store.ChangesetOperationActorsRow) (Check, error) {
	var violations []map[string]string
	rights := map[uuid.UUID]auth.RightSet{}
	for _, oa := range opActors {
		set, ok := rights[oa.ActorID]
		if !ok {
			caps, err := q.ActorCapabilities(ctx, store.ActorCapabilitiesParams{ProjectID: projectID, ActorID: oa.ActorID})
			if err != nil {
				return Check{}, err
			}
			set = auth.EffectiveRights(caps, []string{auth.ScopeAll})
			rights[oa.ActorID] = set
		}
		right, _ := changes.OperationRight(oa.Type)
		if !set.Has(right) {
			violations = append(violations, map[string]string{"actorId": oa.ActorID.String(), "operation": oa.Type, "right": string(right)})
		}
	}
	status := "passed"
	if len(violations) > 0 {
		status = "failed"
	}
	return Check{Stage: "policy", Status: status, Blocking: true, Details: mustJSON(map[string]any{"violations": violations})}, nil
}

// --- approve-changeset / request-changes ---------------------------------------------

type reviewPayload struct {
	ChangesetID uuid.UUID `json:"changesetId"`
	Comment     *string   `json:"comment"`
}

func requireComment(p reviewPayload) error {
	if p.Comment == nil || *p.Comment == "" {
		return commandbus.Validation(map[string]string{"comment": "опишите, что нужно изменить"})
	}
	return nil
}

func review(ctx context.Context, tx pgx.Tx, actor auth.Actor, p reviewPayload, decision string) (Review, error) {
	// PUB-003: согласуют только люди.
	if actor.Kind != auth.ActorHuman {
		return Review{}, commandbus.NewError(http.StatusForbidden, "APPROVAL_FORBIDDEN_ACTOR", "Согласует только человек",
			fmt.Sprintf("Акторы вида %s не согласуют Change Set (PUB-003)", actor.Kind))
	}
	q := store.New(tx)
	cs, err := changes.Lock(ctx, q, actor.ProjectID, p.ChangesetID)
	if err != nil {
		return Review{}, err
	}
	if err := changes.RequireState(cs, "Согласование", "in_review"); err != nil {
		return Review{}, err
	}
	// PUB-002: согласующий не автор ни одной операции, в том числе как onBehalfOf агента.
	authors, err := q.ChangesetAuthors(ctx, cs.ID)
	if err != nil {
		return Review{}, err
	}
	if slices.Contains(authors, actor.ID) || cs.OwnerID == actor.ID {
		return Review{}, commandbus.NewError(http.StatusForbidden, "APPROVAL_SELF", "Нельзя согласовать свои изменения",
			"Согласующий не должен быть автором операций Change Set (PUB-002)")
	}
	if err := q.InsertApproval(ctx, store.InsertApprovalParams{
		ID: uuid.Must(uuid.NewV7()), ChangesetID: cs.ID, ApproverID: actor.ID, Decision: decision,
		ContentHash: cs.ContentHash, Comment: p.Comment,
	}); err != nil {
		return Review{}, err
	}

	policy, err := LoadApprovalPolicy(ctx, q, actor.ProjectID)
	if err != nil {
		return Review{}, err
	}
	required := policy.Required(deref(cs.Risk))
	count, err := q.CountValidApprovals(ctx, store.CountValidApprovalsParams{ChangesetID: cs.ID, ContentHash: cs.ContentHash})
	if err != nil {
		return Review{}, err
	}
	switch {
	case decision == "request_changes":
		cs.State = "changes_requested"
	case int(count) >= required:
		cs.State = "approved"
	}
	if err := q.SetChangesetState(ctx, store.SetChangesetStateParams{ID: cs.ID, State: cs.State}); err != nil {
		return Review{}, err
	}
	return Review{Changeset: changes.ToChangeset(cs), Risk: cs.Risk, RequiredApprovals: required, Approvals: int(count)}, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// --- reopen-changeset ----------------------------------------------------------------

func handleReopen(ctx context.Context, tx pgx.Tx, actor auth.Actor, p changesetRef) (Review, error) {
	q := store.New(tx)
	cs, err := changes.Lock(ctx, q, actor.ProjectID, p.ChangesetID)
	if err != nil {
		return Review{}, err
	}
	if err := changes.RequireOwner(cs, actor); err != nil {
		return Review{}, err
	}
	if err := changes.RequireState(cs, "Возврат в работу", "failed", "changes_requested", "in_review", "approved"); err != nil {
		return Review{}, err
	}
	// Правки меняют содержимое — полученные согласования больше не действуют (PUB-004).
	if err := reopen(ctx, q, &cs); err != nil {
		return Review{}, err
	}
	return Review{Changeset: changes.ToChangeset(cs), Risk: cs.Risk}, nil
}

// --- set-approval-policy -------------------------------------------------------------

func validatePolicy(p ApprovalPolicy) error {
	fields := map[string]string{}
	for name, v := range map[string]int{"low": p.Low, "medium": p.Medium, "high": p.High} {
		if v < 0 || v > 5 {
			fields[name] = "0…5"
		}
	}
	if len(fields) > 0 {
		return commandbus.Validation(fields)
	}
	if p.Low > p.Medium || p.Medium > p.High {
		return commandbus.Validation(map[string]string{"high": "требования не должны убывать с ростом риска: low ≤ medium ≤ high"})
	}
	return nil
}

func handleSetPolicy(ctx context.Context, tx pgx.Tx, actor auth.Actor, p ApprovalPolicy) (ApprovalPolicy, error) {
	q := store.New(tx)
	raw, err := q.GetProjectSettings(ctx, actor.ProjectID)
	if err != nil {
		return ApprovalPolicy{}, err
	}
	settings := map[string]any{}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return ApprovalPolicy{}, err
	}
	settings["approvals"] = p
	if err := q.SetProjectSettings(ctx, store.SetProjectSettingsParams{ID: actor.ProjectID, Settings: mustJSON(settings)}); err != nil {
		return ApprovalPolicy{}, err
	}
	return p, nil
}

// ListChecks — проверки Change Set для текущего содержимого.
func ListChecks(ctx context.Context, q *store.Queries, cs store.Changeset) ([]Check, error) {
	if cs.ContentHash == nil {
		return []Check{}, nil
	}
	rows, err := q.ListChecks(ctx, store.ListChecksParams{ChangesetID: cs.ID, ContentHash: cs.ContentHash})
	if err != nil {
		return nil, err
	}
	out := make([]Check, len(rows))
	for i, r := range rows {
		out[i] = Check{Stage: r.Stage, Status: r.Status, Blocking: r.Blocking, Details: r.Details}
	}
	return out, nil
}

// Approval — решение согласующего.
type Approval struct {
	ApproverID uuid.UUID `json:"approverId"`
	Decision   string    `json:"decision"`
	Comment    *string   `json:"comment"`
	Valid      bool      `json:"valid"` // относится к текущему содержимому и не сброшено
	CreatedAt  string    `json:"createdAt"`
}

// ReviewDetail — проверки и согласования Change Set.
type ReviewDetail struct {
	Review
	History []Approval `json:"history"`
}

// GetReview возвращает проверки и согласования Change Set.
func GetReview(ctx context.Context, q *store.Queries, projectID, id uuid.UUID) (ReviewDetail, error) {
	cs, err := q.GetChangeset(ctx, store.GetChangesetParams{ID: id, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ReviewDetail{}, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Change Set не найден",
			fmt.Sprintf("Change Set %s не найден в проекте", id))
	}
	if err != nil {
		return ReviewDetail{}, err
	}
	checks, err := ListChecks(ctx, q, cs)
	if err != nil {
		return ReviewDetail{}, err
	}
	policy, err := LoadApprovalPolicy(ctx, q, projectID)
	if err != nil {
		return ReviewDetail{}, err
	}
	rows, err := q.ListApprovals(ctx, id)
	if err != nil {
		return ReviewDetail{}, err
	}
	history := make([]Approval, len(rows))
	count := map[uuid.UUID]bool{}
	for i, r := range rows {
		valid := r.InvalidatedAt == nil && cs.ContentHash != nil && string(r.ContentHash) == string(cs.ContentHash)
		if valid && r.Decision == "approve" {
			count[r.ApproverID] = true
		}
		history[i] = Approval{ApproverID: r.ApproverID, Decision: r.Decision, Comment: r.Comment, Valid: valid,
			CreatedAt: r.CreatedAt.Format("2006-01-02T15:04:05.000Z07:00")}
	}
	required := 0
	if cs.Risk != nil {
		required = policy.Required(*cs.Risk)
	}
	return ReviewDetail{
		Review:  Review{Changeset: changes.ToChangeset(cs), Risk: cs.Risk, RequiredApprovals: required, Approvals: len(count), Checks: checks},
		History: history,
	}, nil
}
