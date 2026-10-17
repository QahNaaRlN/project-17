package capabilities

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/capabilitywebhook"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/jobs"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

type SetDeliveryTarget struct {
	Environment      string `json:"environment"`
	EndpointID       string `json:"endpointId"`
	Enabled          bool   `json:"enabled"`
	ExpectedRevision int64  `json:"expectedRevision"`
}
type SendContext struct {
	RequestID                 uuid.UUID `json:"requestId"`
	ExpectedRequestRevision   int64     `json:"expectedRequestRevision"`
	ExpectedPackRevision      int64     `json:"expectedPackRevision"`
	ExpectedPackHash          string    `json:"expectedPackHash"`
	ExpectedSeq               int32     `json:"expectedSeq"`
	ExpectedManifestHash      string    `json:"expectedManifestHash"`
	ExpectedTargetRevision    int64     `json:"expectedTargetRevision"`
	ExpectedTargetFingerprint string    `json:"expectedTargetFingerprint"`
	ConfirmExternalShare      bool      `json:"confirmExternalShare"`
}
type RetryDelivery struct {
	DeliveryID               uuid.UUID `json:"deliveryId"`
	ExpectedDeliveryRevision int64     `json:"expectedDeliveryRevision"`
	ExpectedRequestRevision  int64     `json:"expectedRequestRevision"`
	ExpectedSeq              int32     `json:"expectedSeq"`
	ExpectedManifestHash     string    `json:"expectedManifestHash"`
	ExpectedTargetRevision   int64     `json:"expectedTargetRevision"`
	ConfirmExternalShare     bool      `json:"confirmExternalShare"`
}
type CancelDelivery struct {
	DeliveryID               uuid.UUID `json:"deliveryId"`
	ExpectedDeliveryRevision int64     `json:"expectedDeliveryRevision"`
}
type DeliveryResult struct {
	Delivery     object `json:"delivery"`
	Deduplicated bool   `json:"deduplicated"`
}

func (s *DeliveryService) Register(bus *commandbus.Bus) {
	commandbus.Register(bus, commandbus.Command[SetDeliveryTarget, object]{Name: "set-capability-delivery-target", Authorize: func(a auth.Actor, _ SetDeliveryTarget) error { return deliveryAdmin(a) }, Validate: func(p SetDeliveryTarget) error {
		if p.Environment == "" || p.EndpointID == "" || p.ExpectedRevision < 0 {
			return deliveryError("STATE_INVALID")
		}
		return nil
	}, Handle: s.setTarget})
	commandbus.Register(bus, commandbus.Command[SendContext, DeliveryResult]{Name: "send-capability-context", Authorize: func(a auth.Actor, _ SendContext) error { return deliveryAuthor(a) }, Validate: func(p SendContext) error {
		if p.RequestID == uuid.Nil || p.ExpectedRequestRevision < 1 || p.ExpectedPackRevision < 1 || !codeHash(p.ExpectedPackHash) || !codeHash(p.ExpectedManifestHash) || !codeHash(p.ExpectedTargetFingerprint) || p.ExpectedTargetRevision < 1 || p.ExpectedSeq < 0 || !p.ConfirmExternalShare {
			return deliveryError("STATE_INVALID")
		}
		return nil
	}, Handle: s.sendContext})
	commandbus.Register(bus, commandbus.Command[RetryDelivery, DeliveryResult]{Name: "retry-capability-delivery", Authorize: func(a auth.Actor, _ RetryDelivery) error { return deliveryAuthor(a) }, Validate: func(p RetryDelivery) error {
		if p.DeliveryID == uuid.Nil || p.ExpectedDeliveryRevision < 1 || p.ExpectedRequestRevision < 1 || !codeHash(p.ExpectedManifestHash) || p.ExpectedTargetRevision < 1 || p.ExpectedSeq < 0 || !p.ConfirmExternalShare {
			return deliveryError("STATE_INVALID")
		}
		return nil
	}, Handle: s.retryDelivery})
	commandbus.Register(bus, commandbus.Command[CancelDelivery, DeliveryResult]{Name: "cancel-capability-delivery", Authorize: func(a auth.Actor, _ CancelDelivery) error {
		if deliveryAdmin(a) == nil {
			return nil
		}
		return deliveryAuthor(a)
	}, Validate: func(p CancelDelivery) error {
		if p.DeliveryID == uuid.Nil || p.ExpectedDeliveryRevision < 1 {
			return deliveryError("STATE_INVALID")
		}
		return nil
	}, Handle: s.cancelDelivery})
}

func (s *DeliveryService) setTarget(ctx context.Context, tx pgx.Tx, a auth.Actor, p SetDeliveryTarget) (object, error) {
	if err := reason(ctx); err != nil {
		return nil, err
	}
	q := store.New(tx)
	if err := validation.LockProjectEnvironments(ctx, q, a.ProjectID); err != nil {
		return nil, err
	}
	if err := deliveryLive(ctx, q, a, true); err != nil {
		return nil, err
	}
	env, err := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: a.ProjectID, Name: p.Environment})
	if errors.Is(err, pgx.ErrNoRows) || err == nil && env.Kind != "standard" {
		return nil, deliveryError("NOT_FOUND")
	}
	if err != nil {
		return nil, err
	}
	endpoint, ok := s.Catalog.Find(a.ProjectID, p.Environment, p.EndpointID)
	if !ok {
		return nil, deliveryError("UNAVAILABLE")
	}
	old, err := q.LockCapabilityTarget(ctx, store.LockCapabilityTargetParams{ProjectID: a.ProjectID, Environment: p.Environment})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if old.Revision != p.ExpectedRevision {
		return nil, deliveryError("REVISION_CONFLICT")
	}
	id := old.ID
	if id == uuid.Nil {
		id = uuid.Must(uuid.NewV7())
	}
	t, err := q.SetCapabilityTarget(ctx, store.SetCapabilityTargetParams{ID: id, ProjectID: a.ProjectID, EnvironmentID: env.ID, Environment: p.Environment, EndpointID: endpoint.EndpointID, Url: endpoint.URL, Fingerprint: endpoint.Fingerprint, Enabled: p.Enabled, ChangedBy: a.ID})
	if err != nil {
		return nil, err
	}
	err = q.InsertCapabilityDeliveryAudit(ctx, store.InsertCapabilityDeliveryAuditParams{ProjectID: a.ProjectID, TargetID: t.ID, Event: "target_changed", Source: "human", ActorID: &a.ID, Reason: commandbus.ReasonFrom(ctx), Revision: t.Revision})
	return targetProjection(t, p.Enabled), err
}

func (s *DeliveryService) sendContext(ctx context.Context, tx pgx.Tx, a auth.Actor, p SendContext) (DeliveryResult, error) {
	var result DeliveryResult
	if err := reason(ctx); err != nil {
		return result, err
	}
	q := store.New(tx)
	r, err := lockRequest(ctx, q, a, p.RequestID, p.ExpectedRequestRevision)
	if err != nil {
		return result, deliveryCommandError(err)
	}
	if r.OwnerID != a.ID {
		return result, deliveryError("NOT_FOUND")
	}
	if err := deliveryLive(ctx, q, a, false); err != nil {
		return result, err
	}
	t, err := q.LockCapabilityTarget(ctx, store.LockCapabilityTargetParams{ProjectID: a.ProjectID, Environment: r.Environment})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, deliveryError("UNAVAILABLE")
	}
	if err != nil {
		return result, err
	}
	if t.Revision != p.ExpectedTargetRevision || t.Fingerprint != p.ExpectedTargetFingerprint {
		return result, deliveryError("TARGET_CHANGED")
	}
	existing, err := q.FindCapabilityDelivery(ctx, store.FindCapabilityDeliveryParams{ProjectID: a.ProjectID, RequestID: r.ID, PackRevision: p.ExpectedPackRevision, TargetID: t.ID, TargetRevision: t.Revision})
	if err == nil {
		return DeliveryResult{Delivery: deliveryProjection(existing), Deduplicated: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	endpoint, ok := s.endpoint(t)
	if !ok {
		return result, deliveryError("UNAVAILABLE")
	}
	pack, err := freshPack(ctx, q, a, r)
	if err != nil {
		return result, err
	}
	if pack.Revision != p.ExpectedPackRevision || pack.Hash != p.ExpectedPackHash {
		return result, deliveryError("CONTEXT_STALE")
	}
	if err = checkDeliveryPins(ctx, q, r, pack, p.ExpectedRequestRevision, p.ExpectedSeq, p.ExpectedManifestHash); err != nil {
		return result, err
	}
	id := uuid.Must(uuid.NewV7())
	now := deliveryNow().Truncate(time.Microsecond)
	body, err := manifest.CanonicalJSON(jsonValue(raw(object{"version": "1.0", "event": "capability.context.delivery.v1", "deliveryId": id.String(), "projectId": a.ProjectID.String(), "environment": r.Environment, "requestId": r.ID.String(), "requestRevision": r.Revision, "packRevision": pack.Revision, "packHash": pack.Hash, "targetId": t.ID.String(), "targetRevision": t.Revision, "createdAt": now.Format(time.RFC3339Nano), "contextPack": jsonValue(pack.Body)})))
	if err != nil {
		return result, err
	}
	if len(body) > 640<<10 {
		return result, deliveryError("LIMIT_EXCEEDED")
	}
	d, err := q.InsertCapabilityDelivery(ctx, store.InsertCapabilityDeliveryParams{ID: id, ProjectID: a.ProjectID, RequestID: r.ID, RequestRevision: r.Revision, PackRevision: pack.Revision, PackHash: pack.Hash, TargetID: t.ID, TargetRevision: t.Revision, TargetFingerprint: t.Fingerprint, DestinationUrl: t.Url, EndpointID: t.EndpointID, KeyID: endpoint.ActiveKeyID(), KeyFingerprint: endpoint.KeyFingerprint(endpoint.ActiveKeyID()), InitiatedBy: a.ID, Body: body, BodyHash: capabilitywebhook.Hash(body), CreatedAt: now})
	if err != nil {
		return result, err
	}
	if err = deliveryAudit(ctx, q, d, "queued", commandbus.ReasonFrom(ctx), &a.ID); err != nil {
		return result, err
	}
	if err = jobs.Enqueue(ctx, tx, DeliveryArgs{ProjectID: a.ProjectID, DeliveryID: id}); err != nil {
		return result, err
	}
	return DeliveryResult{Delivery: deliveryProjection(d)}, nil
}

func deliveryCommandError(err error) error {
	var p *commandbus.Error
	if errors.As(err, &p) {
		switch p.Code {
		case "CAPABILITY_REVISION_CONFLICT":
			return deliveryError("REVISION_CONFLICT")
		case "NOT_FOUND":
			return deliveryError("NOT_FOUND")
		}
	}
	return err
}

func (s *DeliveryService) retryDelivery(ctx context.Context, tx pgx.Tx, a auth.Actor, p RetryDelivery) (DeliveryResult, error) {
	var result DeliveryResult
	if err := reason(ctx); err != nil {
		return result, err
	}
	q := store.New(tx)
	d, err := q.GetCapabilityDelivery(ctx, store.GetCapabilityDeliveryParams{ProjectID: a.ProjectID, ID: p.DeliveryID})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, deliveryError("NOT_FOUND")
	}
	if err != nil {
		return result, err
	}
	if d.InitiatedBy != a.ID {
		return result, deliveryError("NOT_FOUND")
	}
	r, err := lockRequest(ctx, q, a, d.RequestID, p.ExpectedRequestRevision)
	if err != nil {
		return result, deliveryCommandError(err)
	}
	if err := deliveryLive(ctx, q, a, false); err != nil {
		return result, err
	}
	t, err := q.LockCapabilityTarget(ctx, store.LockCapabilityTargetParams{ProjectID: a.ProjectID, Environment: r.Environment})
	if err != nil {
		return result, err
	}
	d, err = q.LockCapabilityDelivery(ctx, store.LockCapabilityDeliveryParams{ProjectID: a.ProjectID, ID: d.ID})
	if err != nil {
		return result, err
	}
	if d.Revision != p.ExpectedDeliveryRevision {
		return result, deliveryError("REVISION_CONFLICT")
	}
	if d.State != "failed" || !d.RetryableExhaustion {
		return result, deliveryError("STATE_INVALID")
	}
	if d.Cycle >= 3 {
		return result, deliveryError("RETRY_EXHAUSTED")
	}
	if t.Revision != p.ExpectedTargetRevision || t.Revision != d.TargetRevision || t.Fingerprint != d.TargetFingerprint {
		return result, deliveryError("TARGET_CHANGED")
	}
	endpoint, ok := s.endpoint(t)
	if !ok || !endpoint.HasKey(d.KeyID) || endpoint.KeyFingerprint(d.KeyID) != d.KeyFingerprint {
		return result, deliveryError("UNAVAILABLE")
	}
	pack, err := freshPack(ctx, q, a, r)
	if err != nil {
		return result, err
	}
	if pack.Revision != d.PackRevision || pack.Hash != d.PackHash {
		return result, deliveryError("CONTEXT_STALE")
	}
	if err = checkDeliveryPins(ctx, q, r, pack, p.ExpectedRequestRevision, p.ExpectedSeq, p.ExpectedManifestHash); err != nil {
		return result, err
	}
	d.State = "queued"
	d.Cycle++
	d.AttemptsInCycle = 0
	d.NextAttemptAt = deliveryNow()
	d.RetryableExhaustion = false
	d.LastError = ""
	d.LeaseToken = nil
	d.LeaseUntil = nil
	d, err = saveDelivery(ctx, q, d)
	if err != nil {
		return result, err
	}
	if err = deliveryAudit(ctx, q, d, "retried", commandbus.ReasonFrom(ctx), &a.ID); err != nil {
		return result, err
	}
	err = jobs.Enqueue(ctx, tx, DeliveryArgs{ProjectID: a.ProjectID, DeliveryID: d.ID})
	return DeliveryResult{Delivery: deliveryProjection(d)}, err
}

func (s *DeliveryService) cancelDelivery(ctx context.Context, tx pgx.Tx, a auth.Actor, p CancelDelivery) (DeliveryResult, error) {
	var result DeliveryResult
	if err := reason(ctx); err != nil {
		return result, err
	}
	q := store.New(tx)
	if err := validation.LockProjectEnvironments(ctx, q, a.ProjectID); err != nil {
		return result, err
	}
	live, err := liveDeliveryHuman(ctx, q, a.ProjectID, a.ID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && deliveryAdmin(live) != nil && deliveryAuthor(live) != nil {
		return result, deliveryError("FORBIDDEN")
	}
	if err != nil {
		return result, err
	}
	d, err := q.LockCapabilityDelivery(ctx, store.LockCapabilityDeliveryParams{ProjectID: a.ProjectID, ID: p.DeliveryID})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, deliveryError("NOT_FOUND")
	}
	if err != nil {
		return result, err
	}
	if deliveryAdmin(live) != nil && (d.InitiatedBy != live.ID || deliveryAuthor(live) != nil) {
		return result, deliveryError("NOT_FOUND")
	}
	if d.Revision != p.ExpectedDeliveryRevision {
		return result, deliveryError("REVISION_CONFLICT")
	}
	if !deliveryActive(d.State) {
		return result, deliveryError("STATE_INVALID")
	}
	now := deliveryNow()
	d.State = "cancelled"
	d.CancelledAt = &now
	d.LastError = "CANCELLED_BY_HUMAN"
	// Preserve a current lease so a late receipt can be recorded without reviving
	// the item. No future claim can pass the terminal-state gate.
	d, err = saveDelivery(ctx, q, d)
	if err != nil {
		return result, err
	}
	err = deliveryAudit(ctx, q, d, "cancelled", commandbus.ReasonFrom(ctx), &a.ID)
	return DeliveryResult{Delivery: deliveryProjection(d)}, err
}

func deliveryLive(ctx context.Context, q *store.Queries, a auth.Actor, admin bool) error {
	live, err := liveDeliveryHuman(ctx, q, a.ProjectID, a.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return deliveryError("FORBIDDEN")
	}
	if err != nil {
		return err
	}
	if admin {
		return deliveryAdmin(live)
	}
	return deliveryAuthor(live)
}
