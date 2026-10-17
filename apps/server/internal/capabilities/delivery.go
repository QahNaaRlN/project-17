package capabilities

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/capabilitywebhook"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

type DeliveryService struct {
	Log     *slog.Logger
	Pool    *pgxpool.Pool
	Catalog *capabilitywebhook.Catalog
	Sender  capabilitywebhook.Sender
}

func deliveryError(code string) error {
	status := http.StatusConflict
	switch code {
	case "FORBIDDEN":
		status = 403
	case "NOT_FOUND":
		status = 404
	case "UNAVAILABLE":
		status = 503
	case "LIMIT_EXCEEDED":
		status = 413
	}
	return commandbus.NewError(status, "CAPABILITY_DELIVERY_"+code, "Передача контекста недоступна", code)
}
func deliveryAuthor(a auth.Actor) error {
	if human(a) != nil || !a.Rights.Has(auth.CapabilityDeliver) {
		return deliveryError("FORBIDDEN")
	}
	return nil
}
func deliveryAdmin(a auth.Actor) error {
	if a.Kind != auth.ActorHuman || !a.Rights.Has(auth.ProjectAdmin) {
		return deliveryError("FORBIDDEN")
	}
	return nil
}
func (s *DeliveryService) ValidateCatalog(ctx context.Context) error {
	q := store.New(s.Pool)
	for _, endpoint := range s.Catalog.All() {
		env, err := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: endpoint.ProjectID, Name: endpoint.Environment})
		if err != nil || env.Kind != "standard" {
			return capabilitywebhook.ErrConfig
		}
		pins, err := q.PinnedCapabilityDeliveryKeys(ctx, store.PinnedCapabilityDeliveryKeysParams{ProjectID: endpoint.ProjectID, EndpointID: endpoint.EndpointID, Environment: endpoint.Environment})
		if err != nil {
			return err
		}
		for _, pin := range pins {
			if endpoint.HasKey(pin.KeyID) && endpoint.KeyFingerprint(pin.KeyID) != pin.KeyFingerprint {
				return capabilitywebhook.ErrConfig
			}
		}
	}
	return nil
}
func (s *DeliveryService) endpoint(t store.CapabilityDeliveryTarget) (capabilitywebhook.Endpoint, bool) {
	e, ok := s.Catalog.Find(t.ProjectID, t.Environment, t.EndpointID)
	return e, ok && t.Enabled && e.Fingerprint == t.Fingerprint && e.URL == t.Url
}
func targetProjection(t store.CapabilityDeliveryTarget, ready bool) object {
	why := ""
	if !t.Enabled {
		why = "TARGET_DISABLED"
	} else if !ready {
		why = "UNAVAILABLE"
	}
	return object{"configured": true, "id": t.ID, "environment": t.Environment, "endpointId": t.EndpointID, "url": t.Url, "fingerprint": t.Fingerprint, "enabled": t.Enabled, "revision": t.Revision, "changedBy": t.ChangedBy, "changedAt": t.ChangedAt, "ready": ready, "unavailableReason": why}
}
func deliveryProjection(d store.CapabilityDelivery) object {
	return object{"id": d.ID, "requestId": d.RequestID, "requestRevision": d.RequestRevision, "packRevision": d.PackRevision, "packHash": d.PackHash, "targetId": d.TargetID, "targetRevision": d.TargetRevision, "targetFingerprint": d.TargetFingerprint, "destinationUrl": d.DestinationUrl, "bodyHash": d.BodyHash, "initiatedBy": d.InitiatedBy, "createdAt": d.CreatedAt, "state": d.State, "revision": d.Revision, "cycle": d.Cycle, "attemptsInCycle": d.AttemptsInCycle, "totalAttempts": d.TotalAttempts, "nextAttemptAt": d.NextAttemptAt, "cancelledAt": d.CancelledAt, "deliveredAt": d.DeliveredAt, "lastError": d.LastError, "lastHttpStatus": d.LastHttpStatus, "retryableExhaustion": d.RetryableExhaustion}
}
func deliveryActive(state string) bool {
	return state == "queued" || state == "sending" || state == "retry_wait"
}
func saveDelivery(ctx context.Context, q *store.Queries, d store.CapabilityDelivery) (store.CapabilityDelivery, error) {
	return q.SaveCapabilityDelivery(ctx, store.SaveCapabilityDeliveryParams{ProjectID: d.ProjectID, ID: d.ID, State: d.State, Cycle: d.Cycle, AttemptsInCycle: d.AttemptsInCycle, TotalAttempts: d.TotalAttempts, NextAttemptAt: d.NextAttemptAt, LeaseToken: d.LeaseToken, LeaseUntil: d.LeaseUntil, CancelledAt: d.CancelledAt, DeliveredAt: d.DeliveredAt, LastError: d.LastError, LastHttpStatus: d.LastHttpStatus, RetryableExhaustion: d.RetryableExhaustion})
}
func deliveryAudit(ctx context.Context, q *store.Queries, d store.CapabilityDelivery, event, why string, actor *uuid.UUID) error {
	source := "service"
	if actor != nil {
		source = "human"
	}
	return q.InsertCapabilityDeliveryAudit(ctx, store.InsertCapabilityDeliveryAuditParams{ProjectID: d.ProjectID, DeliveryID: &d.ID, TargetID: d.TargetID, Event: event, Source: source, ActorID: actor, InitiatedBy: &d.InitiatedBy, Reason: why, Revision: d.Revision})
}
func deliveryRead(ctx context.Context, q *store.Queries, a auth.Actor, id uuid.UUID) (store.CapabilityRequest, error) {
	if a.Kind == auth.ActorAgent {
		return store.CapabilityRequest{}, deliveryError("FORBIDDEN")
	}
	r, err := read(ctx, q, a, id)
	if err != nil {
		var p *commandbus.Error
		if errors.As(err, &p) && p.Status == 404 {
			err = deliveryError("NOT_FOUND")
		}
	}
	return r, err
}

// freshPack never refreshes the saved pack. Sequence, manifest, page, exact
// pending anchor and effective CODE policy are all checked against that version.
func freshPack(ctx context.Context, q *store.Queries, a auth.Actor, r store.CapabilityRequest) (store.CapabilityContextPack, error) {
	var pack store.CapabilityContextPack
	if deliveryAuthor(a) != nil || r.OwnerID != a.ID {
		return pack, deliveryError("FORBIDDEN")
	}
	if r.Status == "draft" || terminal(r.Status) {
		return pack, deliveryError("STATE_INVALID")
	}
	packs, err := q.CapabilityPacks(ctx, store.CapabilityPacksParams{ProjectID: a.ProjectID, RequestID: r.ID})
	if err != nil {
		return pack, err
	}
	if len(packs) == 0 {
		return pack, deliveryError("CONTEXT_STALE")
	}
	pack, err = q.GetCapabilityPack(ctx, store.GetCapabilityPackParams{ProjectID: a.ProjectID, RequestID: r.ID, Revision: packs[0].Revision})
	if err != nil {
		return pack, err
	}
	m, err := q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: a.ProjectID, ID: pack.ManifestID})
	if err != nil {
		return pack, err
	}
	snapshot, err := loadContext(ctx, q, a, r, pack.ChangesetSeq, m.Hash, true)
	if err != nil {
		var p *commandbus.Error
		if errors.As(err, &p) {
			return pack, deliveryError("CONTEXT_STALE")
		}
		return pack, err
	}
	hash, err := manifest.Hash(snapshot.doc)
	if err != nil {
		return pack, err
	}
	if hash != pack.PageHash || !exactPending(r, snapshot.doc) {
		return pack, deliveryError("CONTEXT_STALE")
	}
	return pack, nil
}

func checkDeliveryPins(ctx context.Context, q *store.Queries, r store.CapabilityRequest, pack store.CapabilityContextPack, revision int64, seq int32, hash string) error {
	if r.Revision != revision {
		return deliveryError("REVISION_CONFLICT")
	}
	m, err := q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: r.ProjectID, ID: pack.ManifestID})
	if err != nil {
		return err
	}
	if seq != pack.ChangesetSeq || hash != m.Hash {
		return deliveryError("CONTEXT_STALE")
	}
	return nil
}

func deliveryNow() time.Time { return time.Now().UTC() }
