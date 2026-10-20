package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	gh "github.com/qahnaarln/project-17/apps/server/internal/capabilitygithub"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Limits of requiredBy groups (STU-335).
const (
	maxRequestAnchors   = 50
	maxChangesetRequest = 20
)

// AttachRequest — payload attach-capability-request (STU-336).
type AttachRequest struct {
	RequestID               uuid.UUID `json:"requestId"`
	ExpectedRequestRevision int64     `json:"expectedRequestRevision"`
	ChangesetID             uuid.UUID `json:"changesetId"`
	ExpectedSeq             int32     `json:"expectedSeq"`
	DocumentID              uuid.UUID `json:"documentId"`
	NodeID                  string    `json:"nodeId"`
	Event                   string    `json:"event"`
	ExpectedManifestHash    string    `json:"expectedManifestHash"`
	ConfirmReplaceExisting  bool      `json:"confirmReplaceExisting,omitempty"`
}

// AnchorAction — payload of the owner's anchor commands (STU-337/344/346).
type AnchorAction struct {
	RequestID              uuid.UUID       `json:"requestId"`
	AnchorID               uuid.UUID       `json:"anchorId"`
	ExpectedAnchorRevision int64           `json:"expectedAnchorRevision"`
	ExpectedSeq            int32           `json:"expectedSeq,omitempty"`
	Handlers               json.RawMessage `json:"handlers,omitempty"`
}

// CheckFulfillment — payload check-capability-fulfillment (decision §11.8).
type CheckFulfillment struct {
	RequestID uuid.UUID `json:"requestId"`
}

func fulfillmentError(code string) error {
	status := http.StatusConflict
	if code == "CAPABILITY_FULFILLMENT_ARGS_INVALID" {
		status = http.StatusUnprocessableEntity
	}
	return commandbus.NewError(status, code, "Выполнение запроса недоступно", code)
}

func registerFulfillment(bus *commandbus.Bus) {
	commandbus.Register(bus, commandbus.Command[AttachRequest, object]{Name: "attach-capability-request", Authorize: func(a auth.Actor, _ AttachRequest) error { return attachRights(a) }, Validate: func(p AttachRequest) error {
		if p.RequestID == uuid.Nil || p.ExpectedRequestRevision < 1 || p.ChangesetID == uuid.Nil || p.DocumentID == uuid.Nil || p.ExpectedSeq < 0 || !codeHash(p.ExpectedManifestHash) || !validAnchor(p.NodeID, p.Event) {
			return fail("CAPABILITY_REQUEST_INVALID")
		}
		return nil
	}, Handle: attach})
	for _, name := range []string{"detach-capability-request", "set-capability-fulfillment-args", "retry-capability-fulfillment"} {
		command := name
		commandbus.Register(bus, commandbus.Command[AnchorAction, object]{Name: command, Authorize: func(a auth.Actor, _ AnchorAction) error { return human(a) }, Validate: func(p AnchorAction) error {
			if p.RequestID == uuid.Nil || p.AnchorID == uuid.Nil || p.ExpectedAnchorRevision < 1 || p.ExpectedSeq < 0 || (command == "set-capability-fulfillment-args") != (len(p.Handlers) > 0) || len(p.Handlers) > 64*1024 {
				return fail("CAPABILITY_REQUEST_INVALID")
			}
			return nil
		}, Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p AnchorAction) (object, error) {
			return anchorAction(ctx, tx, a, p, command)
		}})
	}
	commandbus.Register(bus, commandbus.Command[CheckFulfillment, object]{Name: "check-capability-fulfillment", Authorize: func(a auth.Actor, _ CheckFulfillment) error {
		if a.Kind != auth.ActorHuman || !a.Rights.Has(auth.CodePropose) && !a.Rights.Has(auth.CapabilityRequest) {
			return forbidden()
		}
		return nil
	}, Validate: func(p CheckFulfillment) error {
		if p.RequestID == uuid.Nil {
			return fail("CAPABILITY_REQUEST_INVALID")
		}
		return nil
	}, Handle: checkFulfillment})
}

func attachRights(a auth.Actor) error {
	if a.Kind != auth.ActorHuman || !a.Rights.Has(auth.CapabilityRequest) || !a.Rights.Has(auth.DesignRead) || !a.Rights.Has(auth.BehaviorUse) {
		return forbidden()
	}
	return nil
}

func anchorView(a store.CapabilityRequestAnchor, own bool, state string) object {
	out := object{"id": a.ID, "state": state, "revision": a.Revision, "original": a.Original, "own": own, "updatedAt": a.UpdatedAt}
	if own {
		out["changesetId"], out["documentId"], out["nodeId"], out["event"] = a.ChangesetID, a.DocumentID, a.NodeID, a.Event
		out["argsSet"] = a.FulfillmentArgs != nil
		out["lastError"] = a.LastError
	}
	return out
}

func fulfillmentAudit(ctx context.Context, q *store.Queries, project, request uuid.UUID, anchor *uuid.UUID, actor *uuid.UUID, event, reason string, data object) error {
	return q.InsertFulfillmentAudit(ctx, store.InsertFulfillmentAuditParams{ID: uuid.Must(uuid.NewV7()), ProjectID: project, RequestID: request, AnchorID: anchor, ActorID: actor, Event: event, Reason: reason, Data: raw(data)})
}

// attach adds an anchor of another (or the same) owner to an open request and
// writes its pending marker in one transaction (STU-336).
func attach(ctx context.Context, tx pgx.Tx, a auth.Actor, p AttachRequest) (object, error) {
	if e := reason(ctx); e != nil {
		return nil, e
	}
	q := store.New(tx)
	r, e := q.GetCapabilityRequest(ctx, store.GetCapabilityRequestParams{ProjectID: a.ProjectID, ID: p.RequestID})
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, notFound()
	}
	if e != nil {
		return nil, e
	}
	// Lock order: environments, Change Set, request (as submit and resolve).
	if e = validation.LockProjectEnvironments(ctx, q, a.ProjectID); e == nil {
		_, e = changes.Lock(ctx, q, a.ProjectID, p.ChangesetID)
	}
	if e == nil {
		r, e = q.LockCapabilityRequest(ctx, store.LockCapabilityRequestParams{ProjectID: a.ProjectID, ID: p.RequestID})
	}
	if e != nil {
		return nil, e
	}
	if r.Status == "draft" || terminal(r.Status) {
		return nil, fail("CAPABILITY_STATE_INVALID")
	}
	if r.Revision != p.ExpectedRequestRevision {
		return nil, fail("CAPABILITY_REVISION_CONFLICT")
	}
	if existing, e := q.AnchorAt(ctx, store.AnchorAtParams{ProjectID: a.ProjectID, RequestID: r.ID, ChangesetID: p.ChangesetID, DocumentID: p.DocumentID, NodeID: p.NodeID, Event: p.Event}); e == nil {
		return object{"anchor": anchorView(store.CapabilityRequestAnchor{ID: existing.ID, State: existing.State, Revision: existing.Revision, ChangesetID: existing.ChangesetID, DocumentID: existing.DocumentID, NodeID: existing.NodeID, Event: existing.Event, UpdatedAt: existing.UpdatedAt}, true, existing.State)}, nil
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return nil, e
	}
	counts, e := q.CountActiveAnchors(ctx, store.CountActiveAnchorsParams{ProjectID: a.ProjectID, RequestID: r.ID, ChangesetID: p.ChangesetID})
	if e != nil {
		return nil, e
	}
	if counts.RequestAnchors >= maxRequestAnchors || counts.ChangesetRequests >= maxChangesetRequest {
		return nil, fulfillmentError("CAPABILITY_ANCHOR_LIMIT")
	}
	target := r
	target.ChangesetID, target.DocumentID, target.NodeID, target.Event, target.OwnerID = p.ChangesetID, p.DocumentID, p.NodeID, p.Event, a.ID
	s, e := loadContext(ctx, q, a, target, p.ExpectedSeq, p.ExpectedManifestHash, true)
	if e != nil {
		return nil, e
	}
	if len(handlerChain(obj(obj(obj(s.doc["nodes"])[p.NodeID])["on"])[p.Event])) > 0 && !p.ConfirmReplaceExisting {
		return nil, fail("CAPABILITY_REPLACE_CONFIRM_REQUIRED")
	}
	anchor, e := q.InsertCapabilityAnchor(ctx, store.InsertCapabilityAnchorParams{ID: uuid.Must(uuid.NewV7()), ProjectID: a.ProjectID, RequestID: r.ID, ChangesetID: p.ChangesetID, DocumentID: p.DocumentID, NodeID: p.NodeID, Event: p.Event, OwnerID: a.ID})
	var change changes.ApplyResult
	if e == nil {
		op := behavior(target, pending(r))
		change, e = changes.ApplyInTx(ctx, tx, a, p.ChangesetID, s.cs.Seq, []changes.OperationInput{{Type: op.Type, Target: &p.DocumentID, Payload: op.Payload}})
	}
	if e == nil {
		e = fulfillmentAudit(ctx, q, a.ProjectID, r.ID, &anchor.ID, &a.ID, "attached", commandbus.ReasonFrom(ctx), object{"changesetId": p.ChangesetID})
	}
	if e != nil {
		return nil, e
	}
	return object{"anchor": anchorView(anchor, true, anchor.State), "change": change}, nil
}

// anchorAction handles detach, fulfillment arguments and retry for the
// anchor's owner (STU-337, STU-344, STU-346).
func anchorAction(ctx context.Context, tx pgx.Tx, a auth.Actor, p AnchorAction, name string) (object, error) {
	if e := reason(ctx); e != nil {
		return nil, e
	}
	q := store.New(tx)
	r, e := q.GetCapabilityRequest(ctx, store.GetCapabilityRequestParams{ProjectID: a.ProjectID, ID: p.RequestID})
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, notFound()
	}
	if e != nil {
		return nil, e
	}
	var anchor store.CapabilityRequestAnchor
	if e = validation.LockProjectEnvironments(ctx, q, a.ProjectID); e == nil {
		anchor, e = q.LockCapabilityAnchor(ctx, store.LockCapabilityAnchorParams{ProjectID: a.ProjectID, ID: p.AnchorID})
	}
	if errors.Is(e, pgx.ErrNoRows) || e == nil && (anchor.RequestID != r.ID || anchor.OwnerID != a.ID) {
		return nil, notFound()
	}
	if e != nil {
		return nil, e
	}
	if anchor.Revision != p.ExpectedAnchorRevision {
		return nil, fail("CAPABILITY_REVISION_CONFLICT")
	}
	if anchor.State == "detached" || anchor.State == "changeset_closed" {
		return nil, fail("CAPABILITY_STATE_INVALID")
	}
	save := func(state string, args []byte, errText string) (store.CapabilityRequestAnchor, error) {
		return q.SaveCapabilityAnchor(ctx, store.SaveCapabilityAnchorParams{ProjectID: a.ProjectID, ID: anchor.ID, State: state, FulfillmentArgs: args, Attempts: 0, NextAt: time.Now(), LastError: errText, ReplacedBatchID: anchor.ReplacedBatchID})
	}
	out := object{}
	next, args := "queued", anchor.FulfillmentArgs
	switch name {
	case "detach-capability-request":
		if anchor.Original || anchor.State == "replaced" {
			return nil, fail("CAPABILITY_STATE_INVALID")
		}
		target := r
		target.NodeID, target.Event = anchor.NodeID, anchor.Event
		op := behavior(target, nil)
		var change changes.ApplyResult
		if _, e = changes.Lock(ctx, q, a.ProjectID, anchor.ChangesetID); e == nil {
			change, e = changes.ApplyInTx(ctx, tx, a, anchor.ChangesetID, p.ExpectedSeq, []changes.OperationInput{{Type: op.Type, Target: &anchor.DocumentID, Payload: op.Payload}})
		}
		if e != nil {
			return nil, e
		}
		next, out["change"] = "detached", change
	case "set-capability-fulfillment-args":
		if !a.Rights.Has(auth.BehaviorUse) || anchor.State == "replaced" || terminal(r.Status) && r.Status != "fulfilled" {
			return nil, fail("CAPABILITY_STATE_INVALID")
		}
		calls := arr(jsonValue(p.Handlers))
		if len(calls) != 1 || obj(calls[0])["action"] != r.Key {
			return nil, fulfillmentError("CAPABILITY_FULFILLMENT_ARGS_INVALID")
		}
		args = raw(calls[0])
		if r.Status != "fulfilled" {
			next = anchor.State
		}
	default: // retry-capability-fulfillment
		if r.Status != "fulfilled" {
			return nil, fail("CAPABILITY_STATE_INVALID")
		}
		if anchor.State == "replaced" {
			doc, e := changes.GetDocument(ctx, q, a.ProjectID, anchor.DocumentID, &anchor.ChangesetID)
			if e != nil {
				return nil, e
			}
			if !anchorPending(r, anchor, parse(doc.Body)) {
				return nil, fail("CAPABILITY_STATE_INVALID")
			}
		}
	}
	if anchor, e = save(next, args, ""); e == nil {
		e = fulfillmentAudit(ctx, q, a.ProjectID, r.ID, &anchor.ID, &a.ID, name, commandbus.ReasonFrom(ctx), object{"state": anchor.State})
	}
	if e != nil {
		return nil, e
	}
	out["anchor"] = anchorView(anchor, true, anchor.State)
	return out, nil
}

// anchorPending — the anchor's event holds exactly this request's marker.
func anchorPending(r store.CapabilityRequest, a store.CapabilityRequestAnchor, doc object) bool {
	target := r
	target.NodeID, target.Event = a.NodeID, a.Event
	return exactPending(target, doc)
}

// checkFulfillment queues an explicit evaluation of one request (decision §11.8).
func checkFulfillment(ctx context.Context, tx pgx.Tx, a auth.Actor, p CheckFulfillment) (object, error) {
	q := store.New(tx)
	r, e := read(ctx, q, a, p.RequestID)
	if e != nil {
		return nil, e
	}
	if r.Status == "draft" || terminal(r.Status) {
		return nil, fail("CAPABILITY_STATE_INVALID")
	}
	env, e := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: a.ProjectID, Name: r.Environment})
	if e != nil {
		return nil, e
	}
	if env.ActiveManifestID == nil {
		return nil, fail("CAPABILITY_ENVIRONMENT_INVALID")
	}
	if e = q.EnqueueFulfillmentCheck(ctx, store.EnqueueFulfillmentCheckParams{ID: uuid.Must(uuid.NewV7()), ProjectID: a.ProjectID, EnvironmentID: env.ID, ManifestID: *env.ActiveManifestID, RequestID: &r.ID}); e != nil {
		return nil, e
	}
	return object{"queued": true}, nil
}

// Anchor states shown to people; a replaced anchor whose marker is back after
// an undo is reported as reverted (STU-346).
func shownState(ctx context.Context, q *store.Queries, r store.CapabilityRequest, a store.CapabilityRequestAnchor) (string, error) {
	if a.State != "replaced" {
		return a.State, nil
	}
	doc, e := changes.GetDocument(ctx, q, r.ProjectID, a.DocumentID, &a.ChangesetID)
	if e != nil {
		var problem *commandbus.Error
		if errors.As(e, &problem) && problem.Code == "NOT_FOUND" {
			return a.State, nil
		}
		return "", e
	}
	if anchorPending(r, a, parse(doc.Body)) {
		return "reverted", nil
	}
	return a.State, nil
}

// FulfillmentGet — STU-351: the decision, its basis and the anchors visible to
// the reader. Reading never calls GitHub.
func FulfillmentGet(ctx context.Context, q *store.Queries, a auth.Actor, id uuid.UUID) (object, error) {
	r, e := read(ctx, q, a, id)
	if e != nil {
		return nil, e
	}
	if r.Status == "draft" {
		return nil, notFound()
	}
	rows, e := q.ListCapabilityAnchors(ctx, store.ListCapabilityAnchorsParams{ProjectID: a.ProjectID, RequestID: r.ID})
	if e != nil {
		return nil, e
	}
	anchors := []any{}
	count := 0
	for _, row := range rows {
		if row.State == "detached" {
			continue
		}
		count++
		own := a.Kind == auth.ActorHuman && row.OwnerID == a.ID
		if !own && !a.Rights.Has(auth.CodePropose) {
			continue
		}
		state, e := shownState(ctx, q, r, row)
		if e != nil {
			return nil, e
		}
		anchors = append(anchors, anchorView(row, own, state))
	}
	out := object{"requestId": r.ID, "status": r.Status, "state": r.FulfillmentState, "reason": r.FulfillmentReason, "mode": r.FulfillmentMode, "commit": r.FulfillmentCommit, "fulfilledAt": r.FulfilledAt, "basis": nil, "anchorCount": count, "anchors": anchors, "canCheck": !terminal(r.Status)}
	if r.FulfillmentBasis != nil {
		out["basis"] = jsonValue(r.FulfillmentBasis)
	}
	return out, nil
}

// OpenRequestByKey lets a designer find an open request to attach to instead
// of a duplicate (STU-336). Only the public summary is returned.
func OpenRequestByKey(ctx context.Context, q *store.Queries, a auth.Actor, environment, key string) (object, error) {
	if attachRights(a) != nil {
		return nil, forbidden()
	}
	r, e := q.OpenCapabilityRequestByKey(ctx, store.OpenCapabilityRequestByKeyParams{ProjectID: a.ProjectID, Environment: environment, Key: key})
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, notFound()
	}
	if e != nil {
		return nil, e
	}
	return object{"id": r.ID, "key": r.Key, "title": r.Title, "status": r.Status, "revision": r.Revision, "environment": r.Environment, "proposedContract": jsonValue(r.ProposedContract)}, nil
}

// systemActor builds cms-system acting for a Change Set owner (STU-345).
func systemActor(ctx context.Context, q *store.Queries, project uuid.UUID, owner uuid.UUID) (auth.Actor, error) {
	row, e := q.GetSystemActor(ctx, &project)
	if e != nil {
		return auth.Actor{}, e
	}
	return auth.Actor{ID: row.ID, Kind: auth.ActorService, DisplayName: row.DisplayName, ProjectID: project, OnBehalfOf: &owner, System: true, Rights: auth.RightSet{auth.BehaviorUse: true, auth.DesignRead: true}}, nil
}

// mergedCheck queues a re-evaluation when a verified observation reports the
// linked PR merged (decision §11.8); it runs in the observation transaction.
func mergedCheck(ctx context.Context, q *store.Queries, l store.CapabilityGithubLink, o gh.Observation) error {
	if o.State != "merged" {
		return nil
	}
	r, e := q.GetCapabilityRequest(ctx, store.GetCapabilityRequestParams{ProjectID: l.ProjectID, ID: l.RequestID})
	if e != nil {
		return e
	}
	env, e := q.GetEnvironmentByID(ctx, store.GetEnvironmentByIDParams{ProjectID: r.ProjectID, ID: r.EnvironmentID})
	if e != nil || env.ActiveManifestID == nil {
		return e
	}
	return q.EnqueueFulfillmentCheck(ctx, store.EnqueueFulfillmentCheckParams{ID: uuid.Must(uuid.NewV7()), ProjectID: r.ProjectID, EnvironmentID: env.ID, ManifestID: *env.ActiveManifestID, RequestID: &r.ID})
}
