package capabilities

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

type Result struct {
	Request object               `json:"request"`
	Change  *changes.ApplyResult `json:"change,omitempty"`
	Pack    *Pack                `json:"pack,omitempty"`
}

func Register(bus *commandbus.Bus) {
	registerPreviewArgs(bus)
	registerFulfillment(bus)
	commandbus.Register(bus, commandbus.Command[Create, Result]{Name: "create-capability-request", Authorize: func(a auth.Actor, _ Create) error { return author(a) }, Validate: func(p Create) error {
		if p.DocumentID == uuid.Nil || p.ChangesetID == uuid.Nil || !codeHash(p.ExpectedManifestHash) {
			return fail("CAPABILITY_REQUEST_INVALID")
		}
		switch p.Kind {
		case "", KindAction:
			if !keyPattern.MatchString(p.Key) || len(p.Key) > 200 || !validAnchor(p.NodeID, p.Event) || p.Placement != nil || p.InitialProps != nil {
				return fail("CAPABILITY_REQUEST_INVALID")
			}
		case KindComponent, KindDataSource, KindToken:
			key := map[string]bool{KindComponent: componentPattern.MatchString(p.Key), KindDataSource: keyPattern.MatchString(p.Key) && len(p.Key) <= 200, KindToken: tokenKey.MatchString(p.Key)}[p.Kind]
			initial := p.InitialProps == nil || p.Kind != KindToken && len(p.InitialProps) <= 64<<10 && obj(jsonValue(p.InitialProps)) != nil
			if !key || p.NodeID != "" || p.Event != "" || !validPlacementFor(p.Kind, p.Placement) || !initial {
				return fail("CAPABILITY_REQUEST_INVALID")
			}
			if category, _ := tokenParts(p.Key); p.Kind == KindToken && parse(p.ProposedContract)["category"] != category {
				return fail("CAPABILITY_CONTRACT_INVALID")
			}
		default:
			return fail("CAPABILITY_REQUEST_INVALID")
		}
		if e := validTexts(p.Draft); e != nil {
			return e
		}
		return validContract(p.Kind, p.ProposedContract)
	}, Handle: create})
	commandbus.Register(bus, commandbus.Command[Update, Result]{Name: "update-capability-request", Authorize: func(a auth.Actor, _ Update) error {
		if a.Kind != auth.ActorHuman && a.Kind != auth.ActorService {
			return forbidden()
		}
		if a.Rights.Has(auth.CodePropose) || a.Rights.Has(auth.CapabilityRequest) {
			return nil
		}
		return forbidden()
	}, Validate: func(p Update) error {
		if p.ID == uuid.Nil || p.ExpectedRevision < 1 || (p.Draft == nil) == (p.Transition == "") {
			return fail("CAPABILITY_REQUEST_INVALID")
		}
		if p.Draft != nil {
			return validDraft(*p.Draft)
		}
		if len(p.Reason) > 2000 || p.PRURL != "" && !validURL(p.PRURL) {
			return fail("CAPABILITY_REQUEST_INVALID")
		}
		return nil
	}, Handle: update})
	for _, name := range []string{"submit-capability-request", "refresh-capability-context", "resolve-capability-request"} {
		command := name
		commandbus.Register(bus, commandbus.Command[Action, Result]{Name: command, Authorize: func(a auth.Actor, _ Action) error { return human(a) }, Validate: func(p Action) error {
			if p.ID == uuid.Nil || p.ExpectedRevision < 1 || p.ExpectedSeq < 0 || !codeHash(p.ExpectedManifestHash) {
				return fail("CAPABILITY_REQUEST_INVALID")
			}
			if command != "resolve-capability-request" && p.Handlers != nil {
				return fail("CAPABILITY_REQUEST_INVALID")
			}
			if command != "submit-capability-request" && p.ConfirmReplaceExisting {
				return fail("CAPABILITY_REQUEST_INVALID")
			}
			return nil
		}, Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p Action) (Result, error) {
			return act(ctx, tx, a, p, command)
		}})
	}
}
func codeHash(s string) bool {
	return len(s) == 71 && strings.HasPrefix(s, "sha256:") && strings.IndexFunc(s[7:], func(r rune) bool { return r < '0' || r > '9' && r < 'a' || r > 'f' }) < 0
}
func validAnchor(node, event string) bool {
	return len(node) > 0 && len(node) <= 200 && len(event) > 0 && len(event) <= 100 && !strings.ContainsAny(node+event, "\r\n\x00")
}
func reason(ctx context.Context) error {
	r := strings.TrimSpace(commandbus.ReasonFrom(ctx))
	if r == "" || len(r) > 2000 {
		return fail("REASON_REQUIRED")
	}
	return nil
}

func create(ctx context.Context, tx pgx.Tx, a auth.Actor, p Create) (Result, error) {
	if e := reason(ctx); e != nil {
		return Result{}, e
	}
	q := store.New(tx)
	if e := validation.LockProjectEnvironments(ctx, q, a.ProjectID); e != nil {
		return Result{}, e
	}
	cs, e := changes.Lock(ctx, q, a.ProjectID, p.ChangesetID)
	if e != nil {
		return Result{}, e
	}
	r := store.CapabilityRequest{ID: uuid.Must(uuid.NewV7()), ProjectID: a.ProjectID, Kind: KindAction, Environment: p.Environment, Key: p.Key, Title: p.Title, Description: p.Description, ProposedContract: p.ProposedContract, Acceptance: raw(p.Acceptance), ChangesetID: p.ChangesetID, DocumentID: p.DocumentID, NodeID: p.NodeID, Event: p.Event, CreatorID: a.ID, OwnerID: cs.OwnerID, AgentSessionID: a.AgentSessionID, Status: "draft"}
	if p.Kind == KindComponent {
		// The placeholder id is fixed now so that the pack and submit agree (STU-360).
		r.Kind, r.NodeID, r.Placement = KindComponent, ir.GenerateNodeID(nil), raw(p.Placement)
		r.InitialProps = raw(object{})
		if p.InitialProps != nil {
			r.InitialProps = p.InitialProps
		}
	}
	if p.Kind == KindDataSource || p.Kind == KindToken {
		// STU-383: a data source anchor is its page key; a token anchor is the
		// node and the design path computed from the page below.
		r.Kind, r.Placement, r.NodeID, r.InitialProps = p.Kind, raw(p.Placement), p.Placement.DataSourceKey, raw(object{})
		if p.Kind == KindToken {
			r.NodeID = p.Placement.NodeID
		}
		if p.InitialProps != nil {
			r.InitialProps = p.InitialProps
		}
	}
	s, e := loadContext(ctx, q, a, r, p.ExpectedSeq, p.ExpectedManifestHash, true)
	if e != nil {
		return Result{}, e
	}
	if e = changes.CheckAgentRight(ctx, q, a, cs, auth.CapabilityRequest, s.path, s.doc, r.NodeID); e != nil {
		return Result{}, e
	}
	if r.Kind == KindToken {
		r.Event, _ = designPath(obj(obj(s.doc["nodes"])[r.NodeID]), placementOf(r))
	}
	if !validInitial(r, s.app) {
		return Result{}, fail("CAPABILITY_PROPS_INVALID")
	}
	r, e = q.InsertCapabilityRequest(ctx, store.InsertCapabilityRequestParams{ID: r.ID, ProjectID: r.ProjectID, EnvironmentID: s.envID, Environment: r.Environment, SourceManifestID: s.m.ID, Key: r.Key, Title: r.Title, Description: r.Description, ProposedContract: r.ProposedContract, Acceptance: r.Acceptance, ChangesetID: r.ChangesetID, DocumentID: r.DocumentID, NodeID: r.NodeID, Event: r.Event, CreatorID: r.CreatorID, OwnerID: r.OwnerID, AgentSessionID: r.AgentSessionID, Kind: r.Kind, Placement: r.Placement, InitialProps: r.InitialProps})
	if errors.Is(e, pgx.ErrNoRows) {
		return Result{}, fail("CAPABILITY_KEY_TAKEN")
	}
	if e != nil {
		return Result{}, e
	}
	if _, e = q.InsertCapabilityAnchor(ctx, store.InsertCapabilityAnchorParams{ID: uuid.Must(uuid.NewV7()), ProjectID: r.ProjectID, RequestID: r.ID, ChangesetID: r.ChangesetID, DocumentID: r.DocumentID, NodeID: r.NodeID, Event: r.Event, OwnerID: r.OwnerID, Original: true, AnchorKind: anchorKind(r.Kind)}); e != nil {
		return Result{}, e
	}
	if e = audit(ctx, q, a, r, "created"); e != nil {
		return Result{}, e
	}
	return Result{Request: projection(r)}, nil
}

func lockRequest(ctx context.Context, q *store.Queries, a auth.Actor, id uuid.UUID, rev int64) (store.CapabilityRequest, error) {
	r, e := q.GetCapabilityRequest(ctx, store.GetCapabilityRequestParams{ProjectID: a.ProjectID, ID: id})
	if errors.Is(e, pgx.ErrNoRows) {
		return r, notFound()
	}
	if e != nil {
		return r, e
	}
	if !visible(a, r) {
		return r, notFound()
	}
	if e = validation.LockProjectEnvironments(ctx, q, a.ProjectID); e != nil {
		return r, e
	}
	if _, e = changes.Lock(ctx, q, a.ProjectID, r.ChangesetID); e != nil {
		return r, e
	}
	r, e = q.LockCapabilityRequest(ctx, store.LockCapabilityRequestParams{ProjectID: a.ProjectID, ID: id})
	if e != nil {
		return r, e
	}
	if r.Revision != rev {
		return r, fail("CAPABILITY_REVISION_CONFLICT")
	}
	return r, nil
}
func save(ctx context.Context, q *store.Queries, r store.CapabilityRequest) (store.CapabilityRequest, error) {
	return q.UpdateCapabilityRequest(ctx, store.UpdateCapabilityRequestParams{ProjectID: r.ProjectID, ID: r.ID, Title: r.Title, Description: r.Description, ProposedContract: r.ProposedContract, Acceptance: r.Acceptance, Status: r.Status, ExternalRefs: r.ExternalRefs, FulfilledManifestID: r.FulfilledManifestID})
}
func audit(ctx context.Context, q *store.Queries, a auth.Actor, r store.CapabilityRequest, event string, extra ...object) error {
	data := object{"status": r.Status}
	for _, value := range extra {
		for k, v := range value {
			data[k] = v
		}
	}
	return q.InsertCapabilityAudit(ctx, store.InsertCapabilityAuditParams{ID: uuid.Must(uuid.NewV7()), ProjectID: r.ProjectID, RequestID: r.ID, Revision: r.Revision, ActorID: a.ID, OnBehalfOf: a.OnBehalfOf, AgentSessionID: a.AgentSessionID, Event: event, Reason: commandbus.ReasonFrom(ctx), Data: raw(data)})
}

func update(ctx context.Context, tx pgx.Tx, a auth.Actor, p Update) (Result, error) {
	if e := reason(ctx); e != nil {
		return Result{}, e
	}
	q := store.New(tx)
	r, e := lockRequest(ctx, q, a, p.ID, p.ExpectedRevision)
	if e != nil {
		return Result{}, e
	}
	if p.Draft != nil || p.Transition == "cancelled" {
		if e = human(a); e != nil {
			return Result{}, e
		}
		if a.ID != r.OwnerID {
			return Result{}, notFound()
		}
		if terminal(r.Status) {
			return Result{}, fail("CAPABILITY_STATE_INVALID")
		}
		if p.Draft != nil {
			if e = validContract(r.Kind, p.Draft.ProposedContract); e != nil {
				return Result{}, e
			}
			cs, e := q.GetChangeset(ctx, store.GetChangesetParams{ProjectID: a.ProjectID, ID: r.ChangesetID})
			if e != nil {
				return Result{}, e
			}
			if cs.State != "open" {
				return Result{}, fail("CAPABILITY_STATE_INVALID")
			}
			if r.Status != "draft" {
				return Result{}, fail("CAPABILITY_STATE_INVALID")
			}
			r.Title = p.Draft.Title
			r.Description = p.Draft.Description
			r.ProposedContract = p.Draft.ProposedContract
			r.Acceptance = raw(p.Draft.Acceptance)
		} else {
			r.Status = "cancelled"
		}
	} else {
		if e = commandbus.Require(a, auth.CodePropose); e != nil {
			return Result{}, e
		}
		if r.Status == "draft" {
			return Result{}, notFound()
		}
		allowed := r.Status == "submitted" && p.Transition == "accepted" || r.Status == "accepted" && p.Transition == "in_progress" || r.Status == "in_progress" && p.Transition == "pr_open" || !terminal(r.Status) && r.Status != "pr_open" && p.Transition == "rejected" && strings.TrimSpace(p.Reason) != ""
		if !allowed {
			return Result{}, fail("CAPABILITY_STATE_INVALID")
		}
		if p.Transition == "pr_open" {
			if !validURL(p.PRURL) {
				return Result{}, fail("CAPABILITY_PR_URL_INVALID")
			}
			refs := arr(jsonValue(r.ExternalRefs))
			if len(refs) >= 8 {
				return Result{}, fail("CAPABILITY_REQUEST_INVALID")
			}
			r.ExternalRefs = raw(append(refs, p.PRURL))
		}
		r.Status = p.Transition
	}
	r, e = save(ctx, q, r)
	if e != nil {
		return Result{}, e
	}
	// A terminal request releases its anchors (STU-335); pending markers stay
	// until their owners remove them explicitly (STU-273).
	if r.Status == "cancelled" || r.Status == "rejected" {
		if e = q.DetachRequestAnchors(ctx, store.DetachRequestAnchorsParams{ProjectID: r.ProjectID, RequestID: r.ID}); e != nil {
			return Result{}, e
		}
	}
	if e = audit(ctx, q, a, r, "updated", object{"transition": p.Transition, "decisionReason": p.Reason, "prUrl": p.PRURL}); e != nil {
		return Result{}, e
	}
	return Result{Request: projection(r)}, nil
}

func act(ctx context.Context, tx pgx.Tx, a auth.Actor, p Action, name string) (Result, error) {
	if e := reason(ctx); e != nil {
		return Result{}, e
	}
	q := store.New(tx)
	r, e := lockRequest(ctx, q, a, p.ID, p.ExpectedRevision)
	if e != nil {
		return Result{}, e
	}
	if r.OwnerID != a.ID {
		return Result{}, notFound()
	}
	s, e := loadContext(ctx, q, a, r, p.ExpectedSeq, p.ExpectedManifestHash, name == "submit-capability-request")
	if e != nil {
		return Result{}, e
	}
	out := Result{}
	switch name {
	case "submit-capability-request":
		if r.Status != "draft" {
			return out, fail("CAPABILITY_STATE_INVALID")
		}
		if r.Kind == KindAction && len(handlerChain(obj(obj(obj(s.doc["nodes"])[r.NodeID])["on"])[r.Event])) > 0 && !p.ConfirmReplaceExisting {
			return out, fail("CAPABILITY_REPLACE_CONFIRM_REQUIRED")
		}
		if !validInitial(r, s.app) {
			return out, fail("CAPABILITY_PROPS_INVALID")
		}
		r.Status = "submitted"
		r, e = save(ctx, q, r)
		if e != nil {
			return out, e
		}
		op := marker(r, s.doc)
		change, e := changes.ApplyInTx(ctx, tx, a, r.ChangesetID, s.cs.Seq, []changes.OperationInput{{Type: op.Type, Target: &r.DocumentID, Payload: op.Payload}})
		if e != nil {
			return out, e
		}
		out.Change = &change
		s, e = loadContext(ctx, q, a, r, change.Changeset.Seq, p.ExpectedManifestHash, true)
		if e != nil {
			return out, e
		}
	case "refresh-capability-context":
		if r.Status == "draft" || terminal(r.Status) {
			return out, fail("CAPABILITY_STATE_INVALID")
		}
		if !exactPending(r, s.doc) {
			return out, fail("CAPABILITY_ANCHOR_CHANGED")
		}
		r, e = save(ctx, q, r)
		if e != nil {
			return out, e
		}
	case "resolve-capability-request":
		if r.Status != "pr_open" {
			return out, fail("CAPABILITY_STATE_INVALID")
		}
		if !exactPending(r, s.doc) {
			return out, fail("CAPABILITY_ANCHOR_CHANGED")
		}
		actual := definition(s.app, r)
		if actual == nil {
			return out, fail("CAPABILITY_NOT_AVAILABLE")
		}
		var op ops.Op
		if r.Kind != KindAction {
			if p.Handlers != nil {
				return out, fail("CAPABILITY_HANDLERS_INVALID")
			}
			if !contractCompatible(r, actual) {
				return out, fail("CAPABILITY_CONTRACT_MISMATCH")
			}
			// The marker may be removed only for a fulfilled request (STU-370).
			r.Status, r.FulfilledManifestID = "fulfilled", &s.m.ID
			if r, e = save(ctx, q, r); e != nil {
				return out, e
			}
			op = clearMarker(r.NodeID)
			if r.Kind != KindComponent {
				op = dataTokenClear(r, s.doc)
			}
		} else {
			if !contractCompatible(r, actual) {
				return out, fail("CAPABILITY_CONTRACT_MISMATCH")
			}
			calls := arr(jsonValue(p.Handlers))
			if len(calls) != 1 || obj(calls[0])["action"] != r.Key {
				return out, fail("CAPABILITY_HANDLERS_INVALID")
			}
			op = behavior(r, calls)
		}
		change, e := changes.ApplyInTx(ctx, tx, a, r.ChangesetID, s.cs.Seq, []changes.OperationInput{{Type: op.Type, Target: &r.DocumentID, Payload: op.Payload}})
		if e != nil {
			return out, e
		}
		// Ordinary drafts may retain warnings. Fulfillment must reject invalid actual
		// calls/scopes/references in the affected event (or the instance's props
		// against the real component) before recording success.
		if r.Kind != KindAction {
			if placeErrors(r, r.NodeID, r.Event, map[string]map[string][]ir.Diagnostic{r.Environment: change.Warnings[r.Environment]}) {
				return out, fail("CAPABILITY_PROPS_INVALID")
			}
		} else {
			for _, diagnostics := range change.Warnings[r.Environment] {
				for _, d := range diagnostics {
					if d.NodeID == r.NodeID && (strings.Contains(d.Pointer, "/on/") || strings.HasPrefix(string(d.Code), "ACTION_")) {
						return out, fail("CAPABILITY_HANDLERS_INVALID")
					}
				}
			}
			r.Status = "fulfilled"
			r.FulfilledManifestID = &s.m.ID
			r, e = save(ctx, q, r)
			if e != nil {
				return out, e
			}
		}
		// STU-341: the manual path fulfills the request; the other anchors of
		// the group are replaced for their owners by the same mechanism (§6).
		original, e := q.OriginalCapabilityAnchor(ctx, store.OriginalCapabilityAnchorParams{ProjectID: r.ProjectID, RequestID: r.ID})
		if e == nil {
			_, e = q.SaveCapabilityAnchor(ctx, store.SaveCapabilityAnchorParams{ProjectID: r.ProjectID, ID: original.ID, State: "replaced", FulfillmentArgs: original.FulfillmentArgs, NextAt: time.Now()})
		}
		if e == nil {
			e = q.MarkManualFulfillment(ctx, store.MarkManualFulfillmentParams{ProjectID: r.ProjectID, ID: r.ID})
		}
		if e == nil {
			e = q.QueueRequestAnchors(ctx, store.QueueRequestAnchorsParams{ProjectID: r.ProjectID, RequestID: r.ID, Skip: &original.ID})
		}
		if e != nil {
			return out, e
		}
		out.Change = &change
	}
	if name != "resolve-capability-request" {
		pack, e := buildPack(ctx, q, r, s)
		if e != nil {
			return out, e
		}
		if e = q.InsertCapabilityPack(ctx, store.InsertCapabilityPackParams{ProjectID: r.ProjectID, RequestID: r.ID, Revision: r.Revision, ManifestID: pack.ManifestID, IndexID: pack.IndexID, ChangesetSeq: pack.Seq, PageHash: pack.PageHash, Hash: pack.Hash, Body: pack.Body}); e != nil {
			return out, e
		}
		out.Pack = &pack
	}
	if e = audit(ctx, q, a, r, name); e != nil {
		return out, e
	}
	out.Request = projection(r)
	return out, nil
}
