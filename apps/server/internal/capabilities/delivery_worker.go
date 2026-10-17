package capabilities

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/capabilitywebhook"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/jobs"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/riverqueue/river"
)

const DeliveryQueue = "capability_delivery"

type DeliveryArgs struct {
	ProjectID  uuid.UUID `json:"projectId"`
	DeliveryID uuid.UUID `json:"deliveryId"`
}

func (DeliveryArgs) Kind() string { return "capability_context_delivery" }
func (DeliveryArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: DeliveryQueue, MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: 30 * time.Second}}
}

type DeliveryWorker struct {
	river.WorkerDefaults[DeliveryArgs]
	Service *DeliveryService
}

func (w *DeliveryWorker) Work(ctx context.Context, job *river.Job[DeliveryArgs]) error {
	return w.Service.Deliver(ctx, job.Args)
}

type DeliveryRecoveryArgs struct{}

func (DeliveryRecoveryArgs) Kind() string                 { return "capability_delivery_recovery" }
func (DeliveryRecoveryArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 1} }

type DeliveryRecoveryWorker struct {
	river.WorkerDefaults[DeliveryRecoveryArgs]
	Service *DeliveryService
	Queue   *jobs.Client
}

func (w *DeliveryRecoveryWorker) Work(ctx context.Context, _ *river.Job[DeliveryRecoveryArgs]) error {
	return w.Service.RecoverDeliveries(ctx, w.Queue)
}

// RecoverDeliveries uses the delivery ledger as authority. A discarded/lost
// River job cannot strand an item; an expired network claim remains uncertain
// and consumes its already-recorded attempt before a new lease is granted.
func (s *DeliveryService) RecoverDeliveries(ctx context.Context, queue *jobs.Client) error {
	if s.Log != nil {
		stats, err := store.New(s.Pool).CapabilityDeliveryQueueStats(ctx)
		if err != nil {
			return err
		}
		s.Log.InfoContext(ctx, "capability delivery queue", "depth", stats.Depth, "oldest_age_seconds", stats.OldestAgeSeconds)
	}
	rows, err := store.New(s.Pool).DueCapabilityDeliveries(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if _, err = queue.Insert(ctx, DeliveryArgs{ProjectID: row.ProjectID, DeliveryID: row.ID}, nil); err != nil {
			return err
		}
	}
	return nil
}

func liveDeliveryHuman(ctx context.Context, q *store.Queries, project, id uuid.UUID) (auth.Actor, error) {
	a := auth.Actor{ID: id, Kind: auth.ActorHuman, ProjectID: project}
	if _, err := q.GetActiveHuman(ctx, id); err != nil {
		return a, err
	}
	caps, err := q.ActorCapabilities(ctx, store.ActorCapabilitiesParams{ProjectID: project, ActorID: id})
	if err != nil {
		return a, err
	}
	a.Rights = auth.EffectiveRights(caps, []string{auth.ScopeAll})
	return a, nil
}

func (s *DeliveryService) workerGate(ctx context.Context, q *store.Queries, d store.CapabilityDelivery, r store.CapabilityRequest, t store.CapabilityDeliveryTarget) (capabilitywebhook.Endpoint, string, error) {
	var zero capabilitywebhook.Endpoint
	a, err := liveDeliveryHuman(ctx, q, d.ProjectID, d.InitiatedBy)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && deliveryAuthor(a) != nil {
		return zero, "AUTH_REVOKED", nil
	}
	if err != nil {
		return zero, "", err
	}
	if terminal(r.Status) || r.Status == "draft" {
		return zero, "REQUEST_TERMINAL", nil
	}
	if !t.Enabled {
		return zero, "TARGET_DISABLED", nil
	}
	if t.ID != d.TargetID || t.Revision != d.TargetRevision || t.Fingerprint != d.TargetFingerprint || t.Url != d.DestinationUrl {
		return zero, "TARGET_CHANGED", nil
	}
	endpoint, ok := s.endpoint(t)
	if !ok {
		return zero, "TARGET_CHANGED", nil
	}
	if !endpoint.HasKey(d.KeyID) || endpoint.KeyFingerprint(d.KeyID) != d.KeyFingerprint {
		return zero, "KEY_UNAVAILABLE", nil
	}
	pack, err := freshPack(ctx, q, a, r)
	if err != nil {
		var problem *commandbus.Error
		if errors.As(err, &problem) {
			return zero, "ANCHOR_CHANGED", nil
		}
		return zero, "", err
	}
	if pack.Revision != d.PackRevision || pack.Hash != d.PackHash {
		return zero, "ANCHOR_CHANGED", nil
	}
	return endpoint, "", nil
}

func (s *DeliveryService) claimDelivery(ctx context.Context, args DeliveryArgs) (store.CapabilityDelivery, capabilitywebhook.Endpoint, bool, error) {
	var d store.CapabilityDelivery
	var endpoint capabilitywebhook.Endpoint
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return d, endpoint, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := store.New(tx)
	if err = validation.LockProjectEnvironments(ctx, q, args.ProjectID); err != nil {
		return d, endpoint, false, err
	}
	d, err = q.GetCapabilityDelivery(ctx, store.GetCapabilityDeliveryParams{ProjectID: args.ProjectID, ID: args.DeliveryID})
	if errors.Is(err, pgx.ErrNoRows) {
		return d, endpoint, false, nil
	}
	if err != nil {
		return d, endpoint, false, err
	}
	r, err := q.GetCapabilityRequest(ctx, store.GetCapabilityRequestParams{ProjectID: d.ProjectID, ID: d.RequestID})
	if err != nil {
		return d, endpoint, false, err
	}
	if _, err = changes.Lock(ctx, q, d.ProjectID, r.ChangesetID); err != nil {
		return d, endpoint, false, err
	}
	r, err = q.LockCapabilityRequest(ctx, store.LockCapabilityRequestParams{ProjectID: d.ProjectID, ID: r.ID})
	if err != nil {
		return d, endpoint, false, err
	}
	t, err := q.LockCapabilityTarget(ctx, store.LockCapabilityTargetParams{ProjectID: d.ProjectID, Environment: r.Environment})
	if err != nil {
		return d, endpoint, false, err
	}
	d, err = q.LockCapabilityDelivery(ctx, store.LockCapabilityDeliveryParams{ProjectID: d.ProjectID, ID: d.ID})
	if err != nil {
		return d, endpoint, false, err
	}
	now := deliveryNow()
	if !deliveryActive(d.State) || d.LeaseUntil != nil && d.LeaseUntil.After(now) || d.NextAttemptAt.After(now) {
		return d, endpoint, false, nil
	}
	if d.State == "sending" && d.LeaseToken != nil {
		if err = q.FinishCapabilityDeliveryAttempt(ctx, store.FinishCapabilityDeliveryAttemptParams{ProjectID: d.ProjectID, DeliveryID: d.ID, LeaseToken: *d.LeaseToken, Outcome: "NETWORK_UNCERTAIN", Retryable: true}); err != nil {
			return d, endpoint, false, err
		}
		d.LeaseToken = nil
		d.LeaseUntil = nil
	}
	var stop string
	endpoint, stop, err = s.workerGate(ctx, q, d, r, t)
	if err != nil {
		return d, endpoint, false, err
	}
	if stop != "" {
		d.State = "cancelled"
		d.CancelledAt = &now
		d.LastError = stop
		d.LeaseToken = nil
		d.LeaseUntil = nil
		if stop == "KEY_UNAVAILABLE" {
			d.State = "failed"
			d.CancelledAt = nil
		}
		d, err = saveDelivery(ctx, q, d)
		if err == nil {
			err = deliveryAudit(ctx, q, d, d.State, stop, nil)
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
		return d, endpoint, false, err
	}
	if d.AttemptsInCycle >= 6 {
		d.State = "failed"
		d.RetryableExhaustion = true
		d.LastError = "NETWORK_UNCERTAIN"
		d, err = saveDelivery(ctx, q, d)
		if err == nil {
			err = deliveryAudit(ctx, q, d, "failed", d.LastError, nil)
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
		return d, endpoint, false, err
	}
	lease := uuid.Must(uuid.NewV7())
	until := now.Add(30 * time.Second)
	d.State = "sending"
	d.AttemptsInCycle++
	d.TotalAttempts++
	d.LeaseToken = &lease
	d.LeaseUntil = &until
	d, err = saveDelivery(ctx, q, d)
	if err != nil {
		return d, endpoint, false, err
	}
	err = q.InsertCapabilityDeliveryAttempt(ctx, store.InsertCapabilityDeliveryAttemptParams{ID: uuid.Must(uuid.NewV7()), ProjectID: d.ProjectID, DeliveryID: d.ID, Ordinal: d.TotalAttempts, Cycle: d.Cycle, LeaseToken: lease, StartedAt: now})
	if err == nil {
		err = deliveryAudit(ctx, q, d, "sending", "ATTEMPT_CLAIMED", nil)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	return d, endpoint, err == nil, err
}

func (s *DeliveryService) Deliver(ctx context.Context, args DeliveryArgs) error {
	d, endpoint, claimed, err := s.claimDelivery(ctx, args)
	if err != nil || !claimed {
		return err
	}
	sender := s.Sender
	if sender == nil {
		sender = &capabilitywebhook.Transport{Catalog: s.Catalog}
	}
	started := time.Now()
	outcome := sender.Send(ctx, endpoint, capabilitywebhook.Message{DeliveryID: d.ID.String(), KeyID: d.KeyID, Body: d.Body, BodyHash: d.BodyHash})
	if s.Log != nil {
		s.Log.InfoContext(ctx, "capability delivery attempt", "project", d.ProjectID, "delivery", d.ID, "attempt", d.TotalAttempts, "cycle", d.Cycle, "duration_ms", time.Since(started).Milliseconds(), "http_status", outcome.HTTPStatus, "outcome", outcome.Code)
	}
	// Completion uses a fresh bounded context so process shutdown does not throw
	// away an already-received durable receipt. A crash is handled by recovery.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.finishDelivery(finishCtx, d, outcome)
}

func retryDelay(attempt int32, after time.Duration) time.Duration {
	base := []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute, 2 * time.Hour}[min(max(attempt-1, 0), 4)]
	if after > base {
		base = after
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	jitter := time.Duration(binary.LittleEndian.Uint64(b[:]) % uint64(base/5+1))
	return base + jitter
}

func (s *DeliveryService) finishDelivery(ctx context.Context, claimed store.CapabilityDelivery, o capabilitywebhook.Outcome) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := store.New(tx)
	d, err := q.LockCapabilityDelivery(ctx, store.LockCapabilityDeliveryParams{ProjectID: claimed.ProjectID, ID: claimed.ID})
	if err != nil {
		return err
	}
	if d.LeaseToken == nil || *d.LeaseToken != *claimed.LeaseToken {
		return nil
	}
	var status *int32
	if o.HTTPStatus != 0 {
		value := int32(o.HTTPStatus)
		status = &value
	}
	finish := store.FinishCapabilityDeliveryAttemptParams{ProjectID: d.ProjectID, DeliveryID: d.ID, LeaseToken: *claimed.LeaseToken, HttpStatus: status, Outcome: o.Code, Retryable: o.Retryable}
	if o.Acknowledged {
		finish.ReceiptDeliveryID = &d.ID
		finish.ReceiptBodyHash = &d.BodyHash
	}
	if d.State == "cancelled" {
		if o.Acknowledged {
			finish.Outcome = "acknowledged_after_cancel"
		}
		if err = q.FinishCapabilityDeliveryAttempt(ctx, finish); err != nil {
			return err
		}
		if err = deliveryAudit(ctx, q, d, finish.Outcome, "ATTEMPT_FINISHED_AFTER_CANCEL", nil); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if d.State != "sending" || d.Revision != claimed.Revision {
		return nil
	}
	now := deliveryNow()
	d.LeaseToken = nil
	d.LeaseUntil = nil
	d.LastHttpStatus = status
	d.LastError = o.Code
	d.RetryableExhaustion = false
	if o.Acknowledged {
		d.State = "delivered"
		d.DeliveredAt = &now
		d.LastError = ""
	} else if !o.Retryable || d.AttemptsInCycle >= 6 {
		d.State = "failed"
		d.RetryableExhaustion = o.Retryable
	} else {
		d.State = "retry_wait"
		d.NextAttemptAt = now.Add(retryDelay(d.AttemptsInCycle, o.RetryAfter))
		finish.NextAttemptAt = &d.NextAttemptAt
	}
	d, err = saveDelivery(ctx, q, d)
	if err != nil {
		return err
	}
	if err = q.FinishCapabilityDeliveryAttempt(ctx, finish); err != nil {
		return err
	}
	if err = deliveryAudit(ctx, q, d, d.State, o.Code, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
