package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/platform/postgres"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Check artifacts of pipeline stages 10–11 (docs/pipeline-visual-lighthouse.md, STU-572…575, 583).
const (
	MaxArtifactSize   = 8 << 20
	maxArtifacts      = 400
	maxArtifactsTotal = 256 << 20
	// ArtifactRetention — STU-583: artifacts of final or replaced checks are kept 90 days.
	ArtifactRetention = 90 * 24 * time.Hour
)

var (
	artifactName  = regexp.MustCompile(`^[a-z0-9._-]{1,128}$`)
	artifactTypes = []string{"image/png", "text/html"}
)

// ArtifactStore — object storage of check artifacts (assets.S3 in production).
type ArtifactStore interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error
	Open(ctx context.Context, key string) (io.ReadCloser, int64, error)
	Remove(ctx context.Context, key string) error
}

type artifactsKey struct{}

// WithArtifacts puts the artifact store into command contexts (commandbus.Bus.WithContext):
// submission queues stage 10 only when screenshots can be kept (STU-572).
func WithArtifacts(s ArtifactStore) func(context.Context) context.Context {
	return func(ctx context.Context) context.Context { return context.WithValue(ctx, artifactsKey{}, s) }
}

func artifactsFrom(ctx context.Context) ArtifactStore {
	s, _ := ctx.Value(artifactsKey{}).(ArtifactStore)
	return s
}

// ArtifactKey — 07 §5: projects/{projectId}/checks/{changesetId}/{checkId}/{name}.
func ArtifactKey(project, changeset, check uuid.UUID, name string) string {
	return "projects/" + project.String() + "/checks/" + changeset.String() + "/" + check.String() + "/" + name
}

// Artifact — a stored file of a check (checks.artifacts, STU-574).
type Artifact struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

// PutArtifact — STU-573: the runner uploads a file of the check it holds the lease of.
func PutArtifact(ctx context.Context, pool *pgxpool.Pool, s ArtifactStore, actor auth.Actor, checkID, leaseID uuid.UUID, name, contentType string, body []byte) error {
	if err := requireRunner(actor); err != nil {
		return err
	}
	if s == nil {
		return checkProblem(http.StatusServiceUnavailable, "CHECK_ARTIFACTS_UNAVAILABLE", "Хранилище артефактов проверок не настроено")
	}
	fields := map[string]string{}
	if !artifactName.MatchString(name) {
		fields["name"] = "[a-z0-9._-], 1…128 символов"
	}
	if !slices.Contains(artifactTypes, contentType) {
		fields["contentType"] = "image/png или text/html"
	}
	if len(fields) > 0 {
		return commandbus.Validation(fields)
	}
	if len(body) > MaxArtifactSize {
		return checkProblem(http.StatusRequestEntityTooLarge, "CHECK_ARTIFACTS_LIMIT", "Артефакт больше 8 МиБ")
	}
	return postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		check, err := q.LockRenderCheck(ctx, store.LockRenderCheckParams{ID: checkID, ProjectID: actor.ProjectID})
		if errors.Is(err, pgx.ErrNoRows) {
			return checkProblem(http.StatusNotFound, "NOT_FOUND", "Проверка не найдена")
		}
		if err != nil {
			return err
		}
		if !leaseHeld(check, leaseID) {
			return checkProblem(http.StatusConflict, "CHECK_LEASE_LOST", "Аренда проверки потеряна")
		}
		if check.Stage != StageVisual && check.Stage != StageLighthouse {
			return commandbus.Validation(map[string]string{"checkId": "артефакты есть только у этапов visual и lighthouse"})
		}
		totals, err := q.CheckArtifactTotals(ctx, store.CheckArtifactTotalsParams{CheckID: check.ID, Name: name})
		if err != nil {
			return err
		}
		if totals.Count+1 > maxArtifacts || totals.Size+int64(len(body)) > maxArtifactsTotal {
			return checkProblem(http.StatusRequestEntityTooLarge, "CHECK_ARTIFACTS_LIMIT", "Больше 400 артефактов или 256 МиБ на проверку")
		}
		sum := sha256.Sum256(body)
		if err := q.UpsertCheckArtifact(ctx, store.UpsertCheckArtifactParams{CheckID: check.ID, Name: name, ContentType: contentType,
			Size: int64(len(body)), Sha256: sum[:]}); err != nil {
			return err
		}
		return s.Put(ctx, ArtifactKey(actor.ProjectID, check.ChangesetID, check.ID, name), bytes.NewReader(body), int64(len(body)), contentType)
	})
}

func leaseHeld(check store.Check, leaseID uuid.UUID) bool {
	return check.Status == "running" && check.LeaseID != nil && *check.LeaseID == leaseID &&
		check.LeaseExpiresAt != nil && check.LeaseExpiresAt.After(time.Now())
}

// recordArtifacts — STU-574: every artifact named in details must be uploaded; the
// uploaded set is written to checks.artifacts.
func recordArtifacts(ctx context.Context, q *store.Queries, check store.Check, details map[string]any) error {
	rows, err := q.ListCheckArtifacts(ctx, check.ID)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	list := make([]Artifact, len(rows))
	for i, r := range rows {
		have[r.Name] = true
		list[i] = Artifact{Name: r.Name, ContentType: r.ContentType, Size: r.Size, SHA256: hex.EncodeToString(r.Sha256)}
	}
	for _, name := range referencedArtifacts(details) {
		if !have[name] {
			return checkProblem(http.StatusUnprocessableEntity, "CHECK_ARTIFACT_MISSING", "Артефакт "+name+" не загружен")
		}
	}
	return q.SetCheckArtifacts(ctx, store.SetCheckArtifactsParams{ID: check.ID, Artifacts: mustJSON(list)})
}

// referencedArtifacts — names under pages[].widths[].artifacts (visual) and pages[].report
// (lighthouse), §5 of the spec.
func referencedArtifacts(details map[string]any) []string {
	var out []string
	pages, _ := details["pages"].([]any)
	for _, raw := range pages {
		page, _ := raw.(map[string]any)
		if name, ok := page["report"].(string); ok {
			out = append(out, name)
		}
		widths, _ := page["widths"].([]any)
		for _, raw := range widths {
			width, _ := raw.(map[string]any)
			files, _ := width["artifacts"].(map[string]any)
			for _, v := range files {
				if name, ok := v.(string); ok {
					out = append(out, name)
				}
			}
		}
	}
	return out
}

// OpenedArtifact — a stream of a stored artifact for the Review API (STU-575).
type OpenedArtifact struct {
	Body        io.ReadCloser
	Size        int64
	ContentType string
}

// OpenArtifact reads an artifact of a check of the Change Set; the caller has already
// authorised reading the Change Set (authorizeWorkRead).
func OpenArtifact(ctx context.Context, q *store.Queries, s ArtifactStore, project, changeset, check uuid.UUID, name string) (OpenedArtifact, error) {
	row, err := q.GetCheckArtifact(ctx, store.GetCheckArtifactParams{CheckID: check, Name: name, ChangesetID: changeset, ProjectID: project})
	if errors.Is(err, pgx.ErrNoRows) || s == nil {
		return OpenedArtifact{}, checkProblem(http.StatusNotFound, "NOT_FOUND", "Артефакт не найден")
	}
	if err != nil {
		return OpenedArtifact{}, err
	}
	if row.ArtifactsExpiredAt != nil {
		return OpenedArtifact{}, checkProblem(http.StatusGone, "CHECK_ARTIFACT_EXPIRED", "Срок хранения артефакта истёк")
	}
	body, size, err := s.Open(ctx, ArtifactKey(project, changeset, check, name))
	if err != nil {
		return OpenedArtifact{}, err
	}
	return OpenedArtifact{Body: body, Size: size, ContentType: row.ContentType}, nil
}

// ArtifactRetentionArgs — STU-583: the daily job that removes expired artifacts.
type ArtifactRetentionArgs struct{}

// Kind names the River job.
func (ArtifactRetentionArgs) Kind() string { return "check_artifact_retention" }

// ArtifactRetentionWorker removes the objects of expired checks and marks the checks.
type ArtifactRetentionWorker struct {
	river.WorkerDefaults[ArtifactRetentionArgs]
	Pool  *pgxpool.Pool
	Store ArtifactStore
}

// Work handles one batch; the next run continues.
func (w *ArtifactRetentionWorker) Work(ctx context.Context, _ *river.Job[ArtifactRetentionArgs]) error {
	return ExpireArtifacts(ctx, w.Pool, w.Store, time.Now().Add(-ArtifactRetention))
}

// ExpireArtifacts is the job body; tests call it with their own cut-off.
func ExpireArtifacts(ctx context.Context, pool *pgxpool.Pool, s ArtifactStore, before time.Time) error {
	if s == nil {
		return nil
	}
	q := store.New(pool)
	checks, err := q.ExpiringArtifactChecks(ctx, before)
	if err != nil {
		return err
	}
	for _, c := range checks {
		rows, err := q.ListCheckArtifacts(ctx, c.ID)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if err := s.Remove(ctx, ArtifactKey(c.ProjectID, c.ChangesetID, c.ID, r.Name)); err != nil {
				return err
			}
		}
		if err := q.MarkArtifactsExpired(ctx, c.ID); err != nil {
			return err
		}
	}
	return nil
}

// --- extend-check-lease --------------------------------------------------------------

type extendPayload struct {
	CheckID uuid.UUID `json:"checkId"`
	LeaseID uuid.UUID `json:"leaseId"`
}

// ExtendedLease — the new expiry of a lease (STU-585).
type ExtendedLease struct {
	LeaseExpiresAt time.Time `json:"leaseExpiresAt"`
}

// stageLimits — STU-498, 585: the longest a check of the stage may run.
var stageLimits = map[string]time.Duration{
	StagePreview: 10 * time.Minute, StageAxe: 10 * time.Minute,
	StageVisual: 15 * time.Minute, StageLighthouse: 15 * time.Minute,
}

func handleExtendLease(ctx context.Context, tx pgx.Tx, actor auth.Actor, p extendPayload) (ExtendedLease, error) {
	if err := requireRunner(actor); err != nil {
		return ExtendedLease{}, err
	}
	q := store.New(tx)
	check, err := q.LockRenderCheck(ctx, store.LockRenderCheckParams{ID: p.CheckID, ProjectID: actor.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ExtendedLease{}, checkProblem(http.StatusNotFound, "NOT_FOUND", "Проверка не найдена")
	}
	if err != nil {
		return ExtendedLease{}, err
	}
	if !leaseHeld(check, p.LeaseID) {
		return ExtendedLease{}, checkProblem(http.StatusConflict, "CHECK_LEASE_LOST", "Аренда проверки потеряна")
	}
	until, err := q.ExtendRenderLease(ctx, store.ExtendRenderLeaseParams{ID: check.ID, LeaseID: &p.LeaseID, LimitSeconds: int32(stageLimits[check.Stage] / time.Second)})
	if err != nil {
		return ExtendedLease{}, err
	}
	return ExtendedLease{LeaseExpiresAt: *until}, nil
}

// --- set-check-policy ----------------------------------------------------------------

// Threshold — a Lighthouse category threshold (STU-581).
type Threshold struct {
	Min      float64 `json:"min"`
	Blocking bool    `json:"blocking"`
}

// LighthousePolicy — thresholds of stage 11.
type LighthousePolicy struct {
	Performance   Threshold `json:"performance"`
	Accessibility Threshold `json:"accessibility"`
}

// Blocking — the check blocks when any category does.
func (p LighthousePolicy) Blocking() bool { return p.Performance.Blocking || p.Accessibility.Blocking }

// CheckPolicy — settings.checks of the project.
type CheckPolicy struct {
	Lighthouse LighthousePolicy `json:"lighthouse"`
}

// DefaultCheckPolicy — decision §11.4: accessibility blocks, performance only warns.
var DefaultCheckPolicy = CheckPolicy{Lighthouse: LighthousePolicy{
	Performance:   Threshold{Min: 0.5},
	Accessibility: Threshold{Min: 0.9, Blocking: true},
}}

// LoadCheckPolicy reads settings.checks; without it the defaults apply.
func LoadCheckPolicy(ctx context.Context, q *store.Queries, project uuid.UUID) (CheckPolicy, error) {
	raw, err := q.GetProjectSettings(ctx, project)
	if err != nil {
		return CheckPolicy{}, err
	}
	var s struct {
		Checks *CheckPolicy `json:"checks"`
	}
	if err := json.Unmarshal(raw, &s); err != nil || s.Checks == nil {
		return DefaultCheckPolicy, err
	}
	return *s.Checks, nil
}

func validateCheckPolicy(p CheckPolicy) error {
	fields := map[string]string{}
	for name, t := range map[string]Threshold{"lighthouse.performance.min": p.Lighthouse.Performance, "lighthouse.accessibility.min": p.Lighthouse.Accessibility} {
		if t.Min < 0 || t.Min > 1 {
			fields[name] = "0…1"
		}
	}
	if len(fields) > 0 {
		return commandbus.Validation(fields)
	}
	return nil
}

// handleSetCheckPolicy — STU-582: applies to the next submissions only.
func handleSetCheckPolicy(ctx context.Context, tx pgx.Tx, actor auth.Actor, p CheckPolicy) (CheckPolicy, error) {
	q := store.New(tx)
	// Settings are part of the validation basis: serialise with submissions (as set-approval-policy).
	if err := validation.LockProjectEnvironments(ctx, q, actor.ProjectID); err != nil {
		return CheckPolicy{}, err
	}
	raw, err := q.GetProjectSettings(ctx, actor.ProjectID)
	if err != nil {
		return CheckPolicy{}, err
	}
	settings := map[string]any{}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return CheckPolicy{}, err
	}
	settings["checks"] = p
	return p, q.SetProjectSettings(ctx, store.SetProjectSettingsParams{ID: actor.ProjectID, Settings: mustJSON(settings)})
}
