package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/platform/postgres"
	"github.com/qahnaarln/project-17/apps/server/internal/previewtoken"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Pipeline stages 8–11 (06 §5.1), run by the check runner (STU-480…499, 570…589).
const (
	StagePreview    = "preview"
	StageAxe        = "axe"
	StageVisual     = "visual"
	StageLighthouse = "lighthouse"

	maxRenderPages     = 50
	maxLighthousePages = 10
	maxCheckDetails    = 256 << 10
	renderAttempts     = 3
	renderStaleAfter   = 15 * time.Minute
)

// renderStages must pass for publication; every stage the runner leases is in leasedStages.
var (
	renderStages = []string{StagePreview, StageAxe}
	leasedStages = []string{StagePreview, StageAxe, StageVisual, StageLighthouse}
)

// RenderPage — a page the runner renders for a check.
type RenderPage struct {
	ObjectID uuid.UUID `json:"objectId"`
	Path     string    `json:"path"`
}

func checkProblem(status int, code, title string) error {
	return commandbus.NewError(status, code, title, "")
}

// renderChecksEnabled — STU-480: stages 8–9 apply unless the project row turns them off.
// No command exposes the switch; command test projects use it (cmstest).
func renderChecksEnabled(ctx context.Context, q *store.Queries, project uuid.UUID) (bool, error) {
	return q.ProjectRenderChecks(ctx, project)
}

// renderPages — STU-482: pages of the Change Set with a route and published pages of the
// environment that use a changed live component, transitively.
func renderPages(ctx context.Context, q *store.Queries, project, env uuid.UUID, versions []store.ChangesetWorkingVersionsRow) ([]RenderPage, error) {
	return validation.CrossChecked(func(all bool) ([]RenderPage, error) { return renderPagesFrom(ctx, q, project, env, versions, all) })
}

// renderPagesFrom reads only the published consumers of changed components through the
// reference index (STU-567); all reads the whole environment for the cross-check.
func renderPagesFrom(ctx context.Context, q *store.Queries, project, env uuid.UUID, versions []store.ChangesetWorkingVersionsRow, all bool) ([]RenderPage, error) {
	type doc struct {
		body map[string]any
		path *string
	}
	var components []uuid.UUID
	for _, v := range versions {
		var body map[string]any
		if !v.Deleted && json.Unmarshal(v.Body, &body) == nil && body["kind"] == "component" {
			components = append(components, v.ObjectID)
		}
	}
	docs := map[uuid.UUID]doc{}
	rows, err := validation.PublishedDependents(ctx, q, project, env, nil, components, all)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.Deleted {
			continue
		}
		var body map[string]any
		if json.Unmarshal(r.Body, &body) == nil {
			docs[r.ObjectID] = doc{body, r.Path}
		}
	}
	changed := map[uuid.UUID]bool{}
	pages := map[uuid.UUID]string{}
	for _, v := range versions {
		delete(docs, v.ObjectID)
		var body map[string]any
		if v.Deleted || json.Unmarshal(v.Body, &body) != nil {
			continue
		}
		switch body["kind"] {
		case "component":
			changed[v.ObjectID] = true
			docs[v.ObjectID] = doc{body, nil}
		case "page":
			if v.Path != nil {
				pages[v.ObjectID] = *v.Path
			}
		}
	}
	uses := func(body map[string]any) bool {
		nodes, _ := body["nodes"].(map[string]any)
		for _, raw := range nodes {
			node, _ := raw.(map[string]any)
			ref, _ := node["ref"].(map[string]any)
			if node["type"] != "Composed" || ref == nil || ref["version"] != nil {
				continue
			}
			if id, err := uuid.Parse(fmt.Sprint(ref["component"])); err == nil && changed[id] {
				return true
			}
		}
		return false
	}
	for grown := len(changed) > 0; grown; {
		grown = false
		for id, d := range docs {
			if changed[id] || !uses(d.body) {
				continue
			}
			switch d.body["kind"] {
			case "component":
				changed[id], grown = true, true
			case "page":
				if d.path != nil {
					pages[id] = *d.path
				}
			}
		}
	}
	out := make([]RenderPage, 0, len(pages))
	for id, path := range pages {
		out = append(out, RenderPage{ObjectID: id, Path: path})
	}
	slices.SortFunc(out, func(a, b RenderPage) int {
		if a.Path != b.Path {
			if a.Path < b.Path {
				return -1
			}
			return 1
		}
		return slices.Compare(a.ObjectID[:], b.ObjectID[:])
	})
	return out, nil
}

// queueRenderChecks records stages 8–11 for the submitted content (STU-480…482, 570) and
// returns the state they impose: "checking", "failed" or "" (no blocking asynchronous checks).
func queueRenderChecks(ctx context.Context, q *store.Queries, cs store.Changeset, ev evaluation, versions []store.ChangesetWorkingVersionsRow) (string, []Check, error) {
	if enabled, err := renderChecksEnabled(ctx, q, cs.ProjectID); err != nil || !enabled {
		return "", nil, err
	}
	policy, err := LoadCheckPolicy(ctx, q, cs.ProjectID)
	if err != nil {
		return "", nil, err
	}
	artifacts := artifactsFrom(ctx) != nil
	var out []Check
	add := func(stage, env, status string, blocking bool, details map[string]any) {
		out = append(out, Check{Stage: stage, Environment: env, Status: status, Blocking: blocking, Details: mustJSON(details)})
	}
	skip := func(stage, env, reason string) { add(stage, env, "skipped", false, map[string]any{"reason": reason}) }
	state := ""
	for _, name := range ev.targets {
		if ev.risk == RiskLow {
			for _, stage := range leasedStages {
				skip(stage, name, "RISK_LOW")
			}
			continue
		}
		env, err := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: cs.ProjectID, Name: name})
		if err != nil {
			return "", nil, err
		}
		pages, err := renderPages(ctx, q, cs.ProjectID, env.ID, versions)
		if err != nil {
			return "", nil, err
		}
		// Stages 10–11 render the same pages as stage 8; without them they have nothing to do.
		later := ""
		switch {
		case env.AppUrl == nil:
			state, later = "failed", "CHECK_APP_URL_MISSING"
			for _, stage := range renderStages {
				add(stage, name, "failed", true, map[string]any{"reason": "CHECK_APP_URL_MISSING", "pages": pages})
			}
		case len(pages) > maxRenderPages:
			state, later = "failed", "PREVIEW_FAILED"
			add(StagePreview, name, "failed", true, map[string]any{"reason": "CHECK_PAGES_LIMIT", "pages": pages[:maxRenderPages], "total": len(pages), "truncated": true})
			add(StageAxe, name, "skipped", true, map[string]any{"reason": "PREVIEW_FAILED"})
		case len(pages) == 0:
			later = "NO_PAGES"
			for _, stage := range renderStages {
				add(stage, name, "skipped", false, map[string]any{"reason": "NO_PAGES", "pages": pages})
			}
		default:
			if state == "" {
				state = "checking"
			}
			for _, stage := range renderStages {
				add(stage, name, "queued", true, map[string]any{"pages": pages})
			}
		}
		// STU-570, 572: stage 10 needs storage for its screenshots and never blocks.
		switch {
		case later != "":
			skip(StageVisual, name, later)
		case !artifacts:
			skip(StageVisual, name, "CHECK_ARTIFACTS_UNAVAILABLE")
		default:
			add(StageVisual, name, "queued", false, map[string]any{"pages": pages})
		}
		// STU-579, 581: stage 11 for high risk, at most 10 pages, blocking by the thresholds.
		switch {
		case ev.risk != RiskHigh:
			skip(StageLighthouse, name, "RISK_LOW")
		case later != "":
			skip(StageLighthouse, name, later)
		default:
			blocking := policy.Lighthouse.Blocking()
			if blocking && state == "" {
				state = "checking"
			}
			details := map[string]any{"pages": pages, "thresholds": policy.Lighthouse}
			if len(pages) > maxLighthousePages {
				details["pages"], details["skipped"] = pages[:maxLighthousePages], pages[maxLighthousePages:]
			}
			add(StageLighthouse, name, "queued", blocking, details)
		}
	}
	for _, c := range out {
		if err := q.InsertRenderCheck(ctx, store.InsertRenderCheckParams{ID: uuid.Must(uuid.NewV7()), ChangesetID: cs.ID, ContentHash: ev.hash,
			Environment: c.Environment, Stage: c.Stage, Status: c.Status, Blocking: c.Blocking, Details: c.Details}); err != nil {
			return "", nil, err
		}
	}
	return state, out, nil
}

// advanceChecking moves a locked Change Set out of checking once its blocking checks
// for the current content are decided (STU-483, 571); it returns the resulting state.
func advanceChecking(ctx context.Context, q *store.Queries, cs store.Changeset) (string, error) {
	if cs.State != "checking" {
		return cs.State, nil
	}
	checks, err := ListChecks(ctx, q, cs)
	if err != nil {
		return "", err
	}
	state, pending := "", false
	for _, c := range checks {
		pending = pending || c.Blocking && (c.Status == "queued" || c.Status == "running")
		if c.Blocking && c.Status == "failed" {
			state = "failed"
		}
	}
	switch {
	case state == "failed":
		// The remaining checks of failed content would not change the outcome.
		err = q.SkipOpenRenderChecks(ctx, store.SkipOpenRenderChecksParams{ChangesetID: cs.ID, Reason: "CHANGESET_FAILED"})
	case pending:
		return cs.State, nil
	default:
		var review ReviewDetail
		if review, err = GetReview(ctx, q, cs.ProjectID, cs.ID); err == nil {
			state = "in_review"
			if review.Approvals >= review.RequiredApprovals && len(review.MissingRequiredRoles) == 0 {
				state = "approved"
			}
		}
	}
	if err == nil {
		err = q.SetChangesetState(ctx, store.SetChangesetStateParams{ID: cs.ID, State: state})
	}
	return state, err
}

// RequireRenderChecks — STU-484, 571: publishing into an environment needs its stage 8–9
// checks and every blocking stage 10–11 check of the current content to be decided
// without a blocking failure; non-blocking checks never hold publication.
func RequireRenderChecks(ctx context.Context, q *store.Queries, cs store.Changeset, environment string) error {
	if enabled, err := renderChecksEnabled(ctx, q, cs.ProjectID); err != nil || !enabled || cs.Kind == "schema" {
		return err
	}
	checks, err := ListChecks(ctx, q, cs)
	if err != nil {
		return err
	}
	found := map[string]bool{}
	pending := false
	for _, c := range checks {
		if c.Environment != environment || !slices.Contains(leasedStages, c.Stage) {
			continue
		}
		found[c.Stage] = true
		if c.Blocking && c.Status == "failed" {
			return checkProblem(http.StatusUnprocessableEntity, "CHECKS_FAILED", "Проверки этапов 8–11 не пройдены")
		}
		pending = pending || c.Blocking && (c.Status == "queued" || c.Status == "running")
	}
	if pending || !found[StagePreview] || !found[StageAxe] {
		return checkProblem(http.StatusConflict, "CHECKS_PENDING", "Проверки этапов 8–11 не завершены или не выполнялись для окружения")
	}
	return nil
}

func requireRunner(actor auth.Actor) error {
	if actor.Kind != auth.ActorService || !actor.Rights.Has(auth.ChecksRun) {
		return checkProblem(http.StatusForbidden, "FORBIDDEN", "Проверки выполняет только исполнитель проверок")
	}
	return nil
}

// ClaimedCheck — a leased stage 8–11 check (STU-485); Check is nil for an empty queue.
type ClaimedCheck struct {
	Check *LeasedCheck `json:"check"`
}

// LeasedCheck — what the runner needs to render the pages of one check.
type LeasedCheck struct {
	ID             uuid.UUID    `json:"checkId"`
	LeaseID        uuid.UUID    `json:"leaseId"`
	LeaseExpiresAt time.Time    `json:"leaseExpiresAt"`
	Stage          string       `json:"stage"`
	ChangesetID    uuid.UUID    `json:"changesetId"`
	Environment    string       `json:"environment"`
	AppURL         string       `json:"appUrl"`
	Pages          []RenderPage `json:"pages"`
	PreviewToken   string       `json:"previewToken"`
	// Thresholds of stage 11 recorded at submission (STU-581).
	Thresholds *LighthousePolicy `json:"thresholds,omitempty"`
}

func handleClaimCheck(ctx context.Context, tx pgx.Tx, actor auth.Actor, _ struct{}) (ClaimedCheck, error) {
	if err := requireRunner(actor); err != nil {
		return ClaimedCheck{}, err
	}
	q := store.New(tx)
	row, err := q.ClaimRenderCheck(ctx, store.ClaimRenderCheckParams{LeaseID: uuid.Must(uuid.NewV7()), ProjectID: actor.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ClaimedCheck{}, nil
	}
	if err != nil {
		return ClaimedCheck{}, err
	}
	env, err := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: actor.ProjectID, Name: row.Environment})
	if err != nil {
		return ClaimedCheck{}, err
	}
	var details struct {
		Pages      []RenderPage      `json:"pages"`
		Thresholds *LighthousePolicy `json:"thresholds"`
	}
	// The server wrote the queued details itself (queueRenderChecks).
	_ = json.Unmarshal(row.Details, &details)
	if env.AppUrl == nil {
		// set-environment-app-url never clears an address that submission saw (STU-481).
		return ClaimedCheck{}, checkProblem(http.StatusConflict, "CHECK_APP_URL_MISSING", "У окружения нет адреса приложения")
	}
	token, err := previewtoken.IssueForChangeset(ctx, q, actor.ProjectSlug, row.Environment, row.ChangesetID, actor.ID, time.Now())
	if err != nil {
		return ClaimedCheck{}, err
	}
	return ClaimedCheck{Check: &LeasedCheck{ID: row.ID, LeaseID: *row.LeaseID, LeaseExpiresAt: *row.LeaseExpiresAt, Stage: row.Stage, ChangesetID: row.ChangesetID,
		Environment: row.Environment, AppURL: *env.AppUrl, Pages: details.Pages, PreviewToken: token, Thresholds: details.Thresholds}}, nil
}

type completePayload struct {
	CheckID uuid.UUID       `json:"checkId"`
	LeaseID uuid.UUID       `json:"leaseId"`
	Status  string          `json:"status"`
	Details json.RawMessage `json:"details"`
}

// CompletedCheck — the stored result and the resulting Change Set state (STU-486).
type CompletedCheck struct {
	CheckID        uuid.UUID `json:"checkId"`
	Status         string    `json:"status"`
	ChangesetState string    `json:"changesetState"`
}

func handleCompleteCheck(ctx context.Context, tx pgx.Tx, actor auth.Actor, p completePayload) (CompletedCheck, error) {
	if err := requireRunner(actor); err != nil {
		return CompletedCheck{}, err
	}
	var details map[string]any
	if !slices.Contains([]string{"passed", "failed", "warning"}, p.Status) || len(p.Details) > maxCheckDetails || json.Unmarshal(p.Details, &details) != nil || details == nil {
		return CompletedCheck{}, commandbus.Validation(map[string]string{"details": "объект JSON до 256 KiB; status: passed, failed или warning"})
	}
	if _, ok := details["pages"].([]any); !ok {
		return CompletedCheck{}, commandbus.Validation(map[string]string{"details": "нужен список pages"})
	}
	q := store.New(tx)
	csID, err := q.RenderCheckChangeset(ctx, store.RenderCheckChangesetParams{ID: p.CheckID, ProjectID: actor.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return CompletedCheck{}, checkProblem(http.StatusNotFound, "NOT_FOUND", "Проверка не найдена")
	}
	if err != nil {
		return CompletedCheck{}, err
	}
	cs, err := changes.Lock(ctx, q, actor.ProjectID, csID)
	if err != nil {
		return CompletedCheck{}, err
	}
	check, err := q.LockRenderCheck(ctx, store.LockRenderCheckParams{ID: p.CheckID, ProjectID: actor.ProjectID})
	if err != nil {
		return CompletedCheck{}, err
	}
	if !leaseHeld(check, p.LeaseID) {
		return CompletedCheck{}, checkProblem(http.StatusConflict, "CHECK_LEASE_LOST", "Аренда проверки потеряна")
	}
	if check.Stage == StageVisual || check.Stage == StageLighthouse {
		if err := recordArtifacts(ctx, q, check, details); err != nil {
			return CompletedCheck{}, err
		}
	}
	if err := q.FinishRenderCheck(ctx, store.FinishRenderCheckParams{ID: check.ID, Status: p.Status, Details: p.Details}); err != nil {
		return CompletedCheck{}, err
	}
	state, err := advanceChecking(ctx, q, cs)
	if err != nil {
		return CompletedCheck{}, err
	}
	return CompletedCheck{CheckID: check.ID, Status: p.Status, ChangesetState: state}, nil
}

// RenderLeaseArgs — STU-487: the periodic job that returns expired leases to the queue.
type RenderLeaseArgs struct{}

// Kind names the River job.
func (RenderLeaseArgs) Kind() string { return "render_check_leases" }

// RenderLeaseWorker requeues checks whose runner lost its lease; the third loss fails it.
type RenderLeaseWorker struct {
	river.WorkerDefaults[RenderLeaseArgs]
	Pool *pgxpool.Pool
}

// Work handles every expired lease in its own transaction.
func (w *RenderLeaseWorker) Work(ctx context.Context, _ *river.Job[RenderLeaseArgs]) error {
	return ExpireRenderLeases(ctx, w.Pool)
}

// ExpireRenderLeases is the job body; tests call it directly.
func ExpireRenderLeases(ctx context.Context, pool *pgxpool.Pool) error {
	expired, err := store.New(pool).ExpiredRenderChecks(ctx)
	if err != nil {
		return err
	}
	for _, e := range expired {
		err := postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
			q := store.New(tx)
			cs, err := changes.Lock(ctx, q, e.ProjectID, e.ChangesetID)
			if err != nil {
				return err
			}
			check, err := q.LockRenderCheck(ctx, store.LockRenderCheckParams{ID: e.ID, ProjectID: e.ProjectID})
			if err != nil {
				return err
			}
			if check.Status != "running" || check.LeaseExpiresAt == nil || check.LeaseExpiresAt.After(time.Now()) {
				return nil
			}
			if check.Attempts < renderAttempts {
				err = q.RequeueRenderCheck(ctx, check.ID)
			} else {
				var details map[string]any
				_ = json.Unmarshal(check.Details, &details)
				details["reason"] = "CHECK_RUNNER_TIMEOUT"
				if err = q.FinishRenderCheck(ctx, store.FinishRenderCheckParams{ID: check.ID, Status: "failed", Details: mustJSON(details)}); err == nil {
					_, err = advanceChecking(ctx, q, cs)
				}
			}
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO project_revisions(project_id,revision) VALUES($1,1)
			 ON CONFLICT(project_id) DO UPDATE SET revision=project_revisions.revision+1`, e.ProjectID)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}
