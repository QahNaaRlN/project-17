package capabilities

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

func deliveryEnvironment(ctx context.Context, q *store.Queries, project uuid.UUID, environment string) error {
	env, err := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: project, Name: environment})
	if errors.Is(err, pgx.ErrNoRows) || err == nil && env.Kind != "standard" {
		return deliveryError("NOT_FOUND")
	}
	return err
}
func (s *DeliveryService) DeliveryEndpoints(ctx context.Context, q *store.Queries, a auth.Actor, environment string) (object, error) {
	if err := deliveryAdmin(a); err != nil {
		return nil, err
	}
	if err := deliveryEnvironment(ctx, q, a.ProjectID, environment); err != nil {
		return nil, err
	}
	items := []any{}
	for _, endpoint := range s.Catalog.Endpoints(a.ProjectID, environment) {
		items = append(items, object{"endpointId": endpoint.EndpointID, "url": endpoint.URL, "fingerprint": endpoint.Fingerprint, "ready": true})
	}
	return object{"items": items}, nil
}
func (s *DeliveryService) DeliveryTarget(ctx context.Context, q *store.Queries, a auth.Actor, environment string) (object, error) {
	if a.Kind == auth.ActorAgent || deliveryAdmin(a) != nil && human(a) != nil && !a.Rights.Has(auth.CodePropose) {
		return nil, deliveryError("FORBIDDEN")
	}
	if err := deliveryEnvironment(ctx, q, a.ProjectID, environment); err != nil {
		return nil, err
	}
	t, err := q.GetCapabilityTarget(ctx, store.GetCapabilityTargetParams{ProjectID: a.ProjectID, Environment: environment})
	if errors.Is(err, pgx.ErrNoRows) {
		return object{"configured": false, "revision": int64(0), "ready": false, "unavailableReason": "UNAVAILABLE"}, nil
	}
	if err != nil {
		return nil, err
	}
	_, ready := s.endpoint(t)
	return targetProjection(t, ready), nil
}
func (s *DeliveryService) DeliveryPreview(ctx context.Context, q *store.Queries, a auth.Actor, id uuid.UUID) (object, error) {
	if err := deliveryAuthor(a); err != nil {
		return nil, err
	}
	r, err := deliveryRead(ctx, q, a, id)
	if err != nil {
		return nil, err
	}
	pack, err := freshPack(ctx, q, a, r)
	if err != nil {
		return nil, err
	}
	t, err := q.GetCapabilityTarget(ctx, store.GetCapabilityTargetParams{ProjectID: a.ProjectID, Environment: r.Environment})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, deliveryError("UNAVAILABLE")
	}
	if err != nil {
		return nil, err
	}
	_, ready := s.endpoint(t)
	if !ready {
		return nil, deliveryError("UNAVAILABLE")
	}
	m, err := q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: a.ProjectID, ID: pack.ManifestID})
	if err != nil {
		return nil, err
	}
	return object{"requestId": r.ID, "requestRevision": r.Revision, "packRevision": pack.Revision, "packHash": pack.Hash, "seq": pack.ChangesetSeq, "manifestHash": m.Hash, "contextPack": jsonValue(pack.Body), "target": targetProjection(t, true), "canSend": true}, nil
}
func (s *DeliveryService) DeliveryList(ctx context.Context, q *store.Queries, a auth.Actor, request uuid.UUID, limit int, encoded string) (object, error) {
	if _, err := deliveryRead(ctx, q, a, request); err != nil {
		return nil, err
	}
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 100 {
		return nil, deliveryError("STATE_INVALID")
	}
	p := store.ListCapabilityDeliveriesParams{ProjectID: a.ProjectID, RequestID: request, PageLimit: int32(limit + 1)}
	fingerprint, _ := manifest.Hash(object{"project": a.ProjectID.String(), "request": request.String(), "actor": a.ID.String()})
	if encoded != "" {
		b, err := base64.RawURLEncoding.DecodeString(encoded)
		var c cursor
		if err != nil || len(b) > 1024 || json.Unmarshal(b, &c) != nil || c.Scope != fingerprint || c.ID == uuid.Nil || c.Time.IsZero() {
			return nil, deliveryError("STATE_INVALID")
		}
		p.BeforeTime = &c.Time
		p.BeforeID = &c.ID
	}
	rows, err := q.ListCapabilityDeliveries(ctx, p)
	if err != nil {
		return nil, err
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	items := []any{}
	for _, row := range rows {
		items = append(items, deliveryProjection(row))
	}
	var next any
	if more {
		last := rows[len(rows)-1]
		next = base64.RawURLEncoding.EncodeToString(raw(cursor{Scope: fingerprint, Time: last.CreatedAt, ID: last.ID}))
	}
	return object{"items": items, "nextCursor": next}, nil
}
func (s *DeliveryService) DeliveryGet(ctx context.Context, q *store.Queries, a auth.Actor, id uuid.UUID) (object, error) {
	if a.Kind == auth.ActorAgent {
		return nil, deliveryError("FORBIDDEN")
	}
	d, err := q.GetCapabilityDelivery(ctx, store.GetCapabilityDeliveryParams{ProjectID: a.ProjectID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, deliveryError("NOT_FOUND")
	}
	if err != nil {
		return nil, err
	}
	if deliveryAdmin(a) != nil {
		if _, err = deliveryRead(ctx, q, a, d.RequestID); err != nil {
			return nil, err
		}
	}
	out := deliveryProjection(d)
	attempts, err := q.CapabilityDeliveryAttempts(ctx, store.CapabilityDeliveryAttemptsParams{ProjectID: a.ProjectID, DeliveryID: d.ID})
	if err != nil {
		return nil, err
	}
	safe := []any{}
	for _, attempt := range attempts {
		safe = append(safe, object{"id": attempt.ID, "ordinal": attempt.Ordinal, "cycle": attempt.Cycle, "startedAt": attempt.StartedAt, "finishedAt": attempt.FinishedAt, "httpStatus": attempt.HttpStatus, "outcome": attempt.Outcome, "retryable": attempt.Retryable, "receiptDeliveryId": attempt.ReceiptDeliveryID, "receiptBodyHash": attempt.ReceiptBodyHash, "nextAttemptAt": attempt.NextAttemptAt})
	}
	out["attempts"] = safe
	audit, err := q.CapabilityDeliveryAudit(ctx, store.CapabilityDeliveryAuditParams{ProjectID: a.ProjectID, DeliveryID: &d.ID})
	if err != nil {
		return nil, err
	}
	if audit == nil {
		audit = []store.CapabilityDeliveryAudit{}
	}
	out["audit"] = audit
	out["canSend"] = false
	out["sendReason"] = "ALREADY_CREATED"
	out["canRetry"] = false
	out["canCancel"] = deliveryActive(d.State) && (deliveryAdmin(a) == nil || d.InitiatedBy == a.ID && deliveryAuthor(a) == nil)
	out["cancelReason"] = ""
	if out["canCancel"] == false {
		if deliveryActive(d.State) {
			out["cancelReason"] = "FORBIDDEN"
		} else {
			out["cancelReason"] = "STATE_INVALID"
		}
	}
	why := "FORBIDDEN"
	if deliveryAuthor(a) == nil && d.InitiatedBy == a.ID {
		why = "STATE_INVALID"
		if d.State == "failed" && d.RetryableExhaustion && d.Cycle < 3 {
			r, err := deliveryRead(ctx, q, a, d.RequestID)
			if err != nil {
				return nil, err
			}
			t, err := q.GetCapabilityTarget(ctx, store.GetCapabilityTargetParams{ProjectID: a.ProjectID, Environment: r.Environment})
			if err != nil {
				return nil, err
			}
			_, stop, err := s.workerGate(ctx, q, d, r, t)
			if err != nil {
				return nil, err
			}
			if stop == "" {
				out["canRetry"] = true
				why = ""
			} else {
				why = stop
			}
		} else if d.Cycle >= 3 {
			why = "RETRY_EXHAUSTED"
		}
	}
	out["retryReason"] = why
	return out, nil
}
