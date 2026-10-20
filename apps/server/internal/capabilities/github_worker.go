package capabilities

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	gh "github.com/qahnaarln/project-17/apps/server/internal/capabilitygithub"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/jobs"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/riverqueue/river"
)

type GithubArgs struct {
	ProjectID uuid.UUID `json:"projectId"`
	RequestID uuid.UUID `json:"requestId"`
}

func (GithubArgs) Kind() string { return "capability_github_sync" }
func (GithubArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "capability_github", MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: 30 * time.Second}}
}

type GithubWorker struct {
	river.WorkerDefaults[GithubArgs]
	Service *GithubService
}

func (w *GithubWorker) Work(ctx context.Context, j *river.Job[GithubArgs]) error {
	return w.Service.SyncGithub(ctx, j.Args)
}

type GithubRecoveryArgs struct{}

func (GithubRecoveryArgs) Kind() string                 { return "capability_github_recovery" }
func (GithubRecoveryArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 1} }

type GithubRecoveryWorker struct {
	river.WorkerDefaults[GithubRecoveryArgs]
	Service *GithubService
	Queue   *jobs.Client
}

func (w *GithubRecoveryWorker) Work(ctx context.Context, _ *river.Job[GithubRecoveryArgs]) error {
	rows, e := store.New(w.Service.Pool).DueCapabilityGithubLinks(ctx)
	if e != nil {
		return e
	}
	for _, r := range rows {
		if _, e = w.Queue.Insert(ctx, GithubArgs{ProjectID: r.ProjectID, RequestID: r.RequestID}, nil); e != nil {
			return e
		}
	}
	return nil
}
func saveGithub(ctx context.Context, q *store.Queries, l store.CapabilityGithubLink) (store.CapabilityGithubLink, error) {
	return q.SaveCapabilityGithubSync(ctx, store.SaveCapabilityGithubSyncParams{ProjectID: l.ProjectID, RequestID: l.RequestID, State: l.State, LastError: l.LastError, Attempts: l.Attempts, PollsLeft: l.PollsLeft, NextAt: l.NextAt, LeaseToken: l.LeaseToken, LeaseUntil: l.LeaseUntil, SyncID: l.SyncID})
}
func (s *GithubService) lockGithub(ctx context.Context, tx pgx.Tx, args GithubArgs) (store.CapabilityGithubLink, store.CapabilityGithubConfig, gh.Repository, string, error) {
	var l store.CapabilityGithubLink
	var c store.CapabilityGithubConfig
	var repo gh.Repository
	q := store.New(tx)
	if e := validation.LockProjectEnvironments(ctx, q, args.ProjectID); e != nil {
		return l, c, repo, "", e
	}
	r, e := q.LockCapabilityRequest(ctx, store.LockCapabilityRequestParams{ProjectID: args.ProjectID, ID: args.RequestID})
	if e != nil {
		return l, c, repo, "", e
	}
	c, e = q.LockCapabilityGithubConfig(ctx, store.LockCapabilityGithubConfigParams{ProjectID: args.ProjectID, Environment: r.Environment})
	if e != nil {
		return l, c, repo, "", e
	}
	l, e = q.LockCapabilityGithubLink(ctx, store.LockCapabilityGithubLinkParams{ProjectID: args.ProjectID, RequestID: args.RequestID})
	if e != nil {
		return l, c, repo, "", e
	}
	if !l.Active || r.Status == "draft" || terminal(r.Status) {
		return l, c, repo, "request_inactive", nil
	}
	repo, ok := s.repository(c)
	if !ok || l.ConfigID != c.ID || l.ConfigRevision != c.Revision || l.Fingerprint != c.Fingerprint {
		return l, c, repo, "config_changed", nil
	}
	a, e := liveDeliveryHuman(ctx, q, args.ProjectID, l.InitiatedBy)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && (githubAuthor(a) != nil || !visible(a, r)) {
		return l, c, repo, "auth_revoked", nil
	}
	return l, c, repo, "", e
}
func stopGithub(ctx context.Context, q *store.Queries, l store.CapabilityGithubLink, why string) error {
	l.State = "stopped"
	l.LastError = why
	l.LeaseToken = nil
	l.LeaseUntil = nil
	if _, e := saveGithub(ctx, q, l); e != nil {
		return e
	}
	return q.InsertCapabilityGithubAudit(ctx, store.InsertCapabilityGithubAuditParams{ID: uuid.Must(uuid.NewV7()), ProjectID: l.ProjectID, RequestID: &l.RequestID, Event: "sync_stopped", Generation: l.Generation, SyncID: &l.SyncID, Attempt: l.Attempts, Reason: why})
}
func (s *GithubService) SyncGithub(ctx context.Context, args GithubArgs) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)
	l, _, repo, why, e := s.lockGithub(ctx, tx, args)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if why != "" {
		if e = stopGithub(ctx, q, l, why); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	now := time.Now()
	if l.State == "idle" || l.State == "stopped" || l.State == "sending" && l.LeaseUntil != nil && now.Before(*l.LeaseUntil) || l.State != "sending" && now.Before(l.NextAt) {
		return nil
	}
	if now.After(l.WindowUntil) || l.Attempts >= 3 {
		if e = stopGithub(ctx, q, l, "exhausted"); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	if e = q.EnsureCapabilityGithubCredential(ctx, repo.CredentialID); e != nil {
		return e
	}
	credential, e := q.LockCapabilityGithubCredential(ctx, repo.CredentialID)
	if e != nil {
		return e
	}
	wait := credential.BlockedUntil
	if credential.LeaseUntil != nil && credential.LeaseUntil.After(wait) {
		wait = *credential.LeaseUntil
	}
	if now.Before(wait) {
		l.State = "retry_wait"
		l.NextAt = wait
		l.LeaseToken = nil
		l.LeaseUntil = nil
		if _, e = saveGithub(ctx, q, l); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	token := uuid.Must(uuid.NewV7())
	until := now.Add(90 * time.Second)
	l.State = "sending"
	l.Attempts++
	l.LeaseToken = &token
	l.LeaseUntil = &until
	if _, e = saveGithub(ctx, q, l); e != nil {
		return e
	}
	if e = q.SaveCapabilityGithubCredential(ctx, store.SaveCapabilityGithubCredentialParams{CredentialID: repo.CredentialID, BlockedUntil: credential.BlockedUntil, LeaseToken: &token, LeaseUntil: &until}); e != nil {
		return e
	}
	if e = q.InsertCapabilityGithubAudit(ctx, store.InsertCapabilityGithubAuditParams{ID: uuid.Must(uuid.NewV7()), ProjectID: l.ProjectID, RequestID: &l.RequestID, Event: "attempt_started", Generation: l.Generation, SyncID: &l.SyncID, Attempt: l.Attempts, Reason: "github_sync"}); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	fetcher := s.Fetcher
	if fetcher == nil {
		fetcher = gh.NewClient()
	}
	networkCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	observation, fetchErr := fetcher.Observe(networkCtx, repo, l.PullNumber)
	cancel()
	// A separate bounded commit context lets a completed request release its lease.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer finishCancel()
	return s.finishGithub(finishCtx, args, l, repo, observation, fetchErr)
}
func (s *GithubService) finishGithub(ctx context.Context, args GithubArgs, claimed store.CapabilityGithubLink, repo gh.Repository, observation gh.Observation, fetchErr error) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)
	l, _, _, why, e := s.lockGithub(ctx, tx, args)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	credential, e := q.LockCapabilityGithubCredential(ctx, repo.CredentialID)
	if e != nil {
		return e
	}
	sameCredential := credential.LeaseToken != nil && claimed.LeaseToken != nil && *credential.LeaseToken == *claimed.LeaseToken
	var f *gh.Failure
	if fetchErr != nil && !errors.As(fetchErr, &f) {
		f = &gh.Failure{Code: "temporary_unavailable", Retryable: true}
	}
	if sameCredential {
		credential.LeaseToken = nil
		credential.LeaseUntil = nil
		if f != nil && f.Code == "rate_limited" && f.RetryAt.After(credential.BlockedUntil) {
			credential.BlockedUntil = f.RetryAt
		}
		if e = q.SaveCapabilityGithubCredential(ctx, store.SaveCapabilityGithubCredentialParams{CredentialID: repo.CredentialID, BlockedUntil: credential.BlockedUntil}); e != nil {
			return e
		}
	}
	same := l.Active && l.Generation == claimed.Generation && l.ConfigRevision == claimed.ConfigRevision && l.SyncID == claimed.SyncID && l.State == "sending" && l.LeaseToken != nil && claimed.LeaseToken != nil && *l.LeaseToken == *claimed.LeaseToken && l.LeaseUntil != nil && time.Now().Before(*l.LeaseUntil)
	if !same {
		if e = q.InsertCapabilityGithubAudit(ctx, store.InsertCapabilityGithubAuditParams{ID: uuid.Must(uuid.NewV7()), ProjectID: claimed.ProjectID, RequestID: &claimed.RequestID, Event: "late_result_ignored", Generation: claimed.Generation, SyncID: &claimed.SyncID, Attempt: claimed.Attempts, Reason: "generation_or_lease_changed"}); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	if why != "" {
		if e = stopGithub(ctx, q, l, why); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	now := time.Now()
	l.LeaseToken = nil
	l.LeaseUntil = nil
	event := "observed"
	if fetchErr == nil {
		b := raw(observation)
		if e = q.InsertCapabilityGithubObservation(ctx, store.InsertCapabilityGithubObservationParams{ID: uuid.Must(uuid.NewV7()), ProjectID: l.ProjectID, RequestID: l.RequestID, LinkID: l.ID, Generation: l.Generation, ConfigRevision: l.ConfigRevision, Fingerprint: l.Fingerprint, SyncID: l.SyncID, Body: b, Digest: gh.Digest(observation)}); e != nil {
			return e
		}
		if e = archiveClosedPreviews(ctx, q, l, parse(b)); e != nil {
			return e
		}
		l.LastError = ""
		l.State = "idle"
		l.Attempts = 0
		if l.PollsLeft > 0 && now.Add(5*time.Minute).Before(l.WindowUntil) {
			count, err := q.CapabilityGithubRefreshCount(ctx, l.ProjectID)
			if err != nil {
				return err
			}
			if count < 20 {
				l.PollsLeft--
				l.State = "queued"
				l.NextAt = now.Add(5 * time.Minute)
				l.SyncID = uuid.Must(uuid.NewV7())
				if e = q.InsertCapabilityGithubAudit(ctx, store.InsertCapabilityGithubAuditParams{ID: uuid.Must(uuid.NewV7()), ProjectID: l.ProjectID, RequestID: &l.RequestID, Event: "automatic_refresh", Generation: l.Generation, SyncID: &l.SyncID, Reason: "bounded_poll"}); e != nil {
					return e
				}
			}
		}
	} else {
		event = "sync_failed"
		l.LastError = f.Code
		l.State = "stopped"
		if f.Retryable && l.Attempts < 3 {
			delay := time.Minute
			if l.Attempts > 1 {
				delay = 5 * time.Minute
			}
			l.NextAt = now.Add(delay + time.Duration(now.Nanosecond()%1000)*time.Millisecond)
			if f.RetryAt.After(l.NextAt) {
				l.NextAt = f.RetryAt
			}
			if l.NextAt.Before(l.WindowUntil) {
				l.State = "retry_wait"
			}
		}
	}
	if _, e = saveGithub(ctx, q, l); e != nil {
		return e
	}
	if e = q.InsertCapabilityGithubAudit(ctx, store.InsertCapabilityGithubAuditParams{ID: uuid.Must(uuid.NewV7()), ProjectID: l.ProjectID, RequestID: &l.RequestID, Event: event, Generation: l.Generation, SyncID: &claimed.SyncID, Attempt: claimed.Attempts, Reason: l.LastError}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
