package capabilities

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/riverqueue/river"
)

// archiveClosedPreviews archives preview environments of a PR when the
// observation just committed reports it closed or merged (STU-326, decision
// §11.6). It runs in the observation transaction under the project environment
// lock; a last-known observation never reaches this path.
func archiveClosedPreviews(ctx context.Context, q *store.Queries, l store.CapabilityGithubLink, facts object) error {
	state, _ := facts["state"].(string)
	repository, _ := facts["repositoryId"].(string)
	if state != "closed" && state != "merged" {
		return nil
	}
	envs, e := q.ActivePreviewBySource(ctx, store.ActivePreviewBySourceParams{ProjectID: l.ProjectID, SourceConfigID: &l.ConfigID, SourceRepositoryID: &repository, SourcePullNumber: &l.PullNumber})
	if e != nil {
		return e
	}
	for _, env := range envs {
		if _, e = projects.Archive(ctx, q, env, "pr_"+state, nil, "github_observation"); e != nil {
			return e
		}
	}
	return nil
}

// PreviewExpiryArgs — periodic MF-032 TTL sweep (STU-326).
type PreviewExpiryArgs struct{}

func (PreviewExpiryArgs) Kind() string                 { return "capability_preview_expiry" }
func (PreviewExpiryArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 1} }

type PreviewExpiryWorker struct {
	river.WorkerDefaults[PreviewExpiryArgs]
	Pool *pgxpool.Pool
}

func (w *PreviewExpiryWorker) Work(ctx context.Context, _ *river.Job[PreviewExpiryArgs]) error {
	return ExpirePreviews(ctx, w.Pool)
}

// ExpirePreviews archives preview environments whose 14-day window passed. The
// expiry is re-read under the environment lock, so a concurrent registration
// that extended the window wins.
func ExpirePreviews(ctx context.Context, pool *pgxpool.Pool) error {
	rows, e := store.New(pool).ExpiredPreviewEnvironments(ctx)
	if e != nil {
		return e
	}
	for _, row := range rows {
		e = inProject(ctx, pool, row.ProjectID, func(q *store.Queries) (bool, error) {
			env, e := q.LockEnvironmentByID(ctx, store.LockEnvironmentByIDParams{ProjectID: row.ProjectID, ID: row.ID})
			if errors.Is(e, pgx.ErrNoRows) {
				return false, nil
			}
			if e != nil || env.ArchivedAt != nil || env.ExpiresAt == nil || env.ExpiresAt.After(deliveryNow()) {
				return false, e
			}
			_, e = projects.Archive(ctx, q, env, "ttl", nil, "ttl")
			return e == nil, e
		})
		if e != nil {
			return e
		}
	}
	return nil
}

// inProject runs fn under the project environment lock and publishes a realtime
// invalidation when it changed something, as commands do.
func inProject(ctx context.Context, pool *pgxpool.Pool, project uuid.UUID, fn func(*store.Queries) (bool, error)) error {
	tx, e := pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)
	if e = validation.LockProjectEnvironments(ctx, q, project); e != nil {
		return e
	}
	changed, e := fn(q)
	if e != nil {
		return e
	}
	if changed {
		if _, e = tx.Exec(ctx, `INSERT INTO project_revisions(project_id,revision) VALUES($1,1)
		 ON CONFLICT(project_id) DO UPDATE SET revision=project_revisions.revision+1`, project); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
