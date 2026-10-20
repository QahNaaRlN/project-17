package capabilities

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

func projection(r store.CapabilityRequest) object {
	out := object{"id": r.ID, "kind": r.Kind, "environment": r.Environment, "sourceManifestId": r.SourceManifestID, "key": r.Key, "title": r.Title, "description": r.Description, "proposedContract": jsonValue(r.ProposedContract), "acceptance": jsonValue(r.Acceptance), "changesetId": r.ChangesetID, "documentId": r.DocumentID, "nodeId": r.NodeID, "event": r.Event, "creatorId": r.CreatorID, "ownerId": r.OwnerID, "status": r.Status, "revision": r.Revision, "externalRefs": jsonValue(r.ExternalRefs), "fulfilledManifestId": r.FulfilledManifestID, "createdAt": r.CreatedAt, "updatedAt": r.UpdatedAt}
	if r.Kind == KindComponent {
		out["placement"] = jsonValue(r.Placement) // structure only; values stay with the owner
	}
	return out
}
func owns(a auth.Actor, r store.CapabilityRequest) bool {
	if a.Kind == auth.ActorAgent {
		return a.DelegatorID != nil && *a.DelegatorID == r.OwnerID && auth.AgentAllowsCS(a, r.ChangesetID)
	}
	return a.Kind == auth.ActorHuman && a.ID == r.OwnerID
}
func visible(a auth.Actor, r store.CapabilityRequest) bool {
	return owns(a, r) && author(a) == nil || a.Kind != auth.ActorAgent && a.Rights.Has(auth.CodePropose) && r.Status != "draft"
}
func read(ctx context.Context, q *store.Queries, a auth.Actor, id uuid.UUID) (store.CapabilityRequest, error) {
	r, e := q.GetCapabilityRequest(ctx, store.GetCapabilityRequestParams{ProjectID: a.ProjectID, ID: id})
	if errors.Is(e, pgx.ErrNoRows) {
		return r, notFound()
	}
	if e != nil {
		return r, e
	}
	if !visible(a, r) {
		// STU-337: an owner of an attached anchor sees the request and its own anchors.
		if a.Kind != auth.ActorHuman || r.Status == "draft" || author(a) != nil {
			return r, notFound()
		}
		owns, e := q.OwnsCapabilityAnchor(ctx, store.OwnsCapabilityAnchorParams{ProjectID: a.ProjectID, RequestID: r.ID, OwnerID: a.ID})
		if e != nil {
			return r, e
		}
		if !owns {
			return r, notFound()
		}
	}
	return r, nil
}

// Get never returns live page content to an engineer. Only an author may compare
// the current scoped context; all engineer context is immutable exported data.
func Get(ctx context.Context, q *store.Queries, a auth.Actor, id uuid.UUID) (object, error) {
	r, e := read(ctx, q, a, id)
	if e != nil {
		return nil, e
	}
	out := projection(r)
	if r.Status != "draft" {
		link, err := q.GetCapabilityGithubLink(ctx, store.GetCapabilityGithubLinkParams{ProjectID: a.ProjectID, RequestID: id})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			summary := object{"link": githubLinkView(link), "verification": "last_known_only", "lastKnown": nil}
			observation, readErr := q.LatestCapabilityGithubObservation(ctx, store.LatestCapabilityGithubObservationParams{ProjectID: a.ProjectID, RequestID: id})
			if readErr != nil && !errors.Is(readErr, pgx.ErrNoRows) {
				return nil, readErr
			}
			if readErr == nil {
				stored := parse(observation.Body)
				facts := object{}
				for _, key := range []string{"repositoryId", "prId", "number", "state", "draft", "headSha", "baseSha", "integrationSha", "integrationType", "ci", "policyDigest"} {
					if value, ok := stored[key]; ok {
						facts[key] = value
					}
				}
				summary["lastKnown"] = object{"id": observation.ID, "generation": observation.Generation, "configRevision": observation.ConfigRevision, "observedAt": observation.ObservedAt, "freshUntil": observation.FreshUntil, "facts": facts}
			}
			out["github"] = summary
		}
	}
	// STU-351: short fulfillment summary for the card and the agent, without IR.
	out["fulfillment"] = object{"state": r.FulfillmentState, "reason": r.FulfillmentReason, "mode": r.FulfillmentMode, "commit": r.FulfillmentCommit, "fulfilledAt": r.FulfilledAt}
	packs, e := q.CapabilityPacks(ctx, store.CapabilityPacksParams{ProjectID: a.ProjectID, RequestID: id})
	if e != nil {
		return nil, e
	}
	out["packs"] = packs
	audits, e := q.CapabilityAudit(ctx, store.CapabilityAuditParams{ProjectID: a.ProjectID, RequestID: id})
	if e != nil {
		return nil, e
	}
	history := []any{}
	for _, v := range audits {
		history = append(history, object{"revision": v.Revision, "actorId": v.ActorID, "event": v.Event, "reason": v.Reason, "data": jsonValue(v.Data), "createdAt": v.CreatedAt})
	}
	out["audit"] = history
	transitions := []string{}
	if owns(a, r) && human(a) == nil && !terminal(r.Status) {
		transitions = append(transitions, "cancelled")
		if r.Status == "draft" {
			transitions = append(transitions, "edit", "submit")
		} else {
			transitions = append(transitions, "refresh")
			if r.Status == "pr_open" {
				transitions = append(transitions, "resolve")
			}
		}
	}
	if a.Kind != auth.ActorAgent && a.Rights.Has(auth.CodePropose) {
		switch r.Status {
		case "submitted":
			transitions = append(transitions, "accepted", "rejected")
		case "accepted":
			transitions = append(transitions, "in_progress", "rejected")
		case "in_progress":
			transitions = append(transitions, "pr_open", "rejected")
		}
	}
	out["transitions"] = transitions
	if owns(a, r) && author(a) == nil {
		cs, e := q.GetChangeset(ctx, store.GetChangesetParams{ProjectID: a.ProjectID, ID: r.ChangesetID})
		if e != nil {
			return nil, e
		}
		out["changesetState"] = cs.State
		if r.Kind == KindComponent {
			out["initialProps"] = jsonValue(r.InitialProps)
		}
		out["seq"] = cs.Seq
		doc, e := changes.GetDocument(ctx, q, a.ProjectID, r.DocumentID, &r.ChangesetID)
		if e != nil {
			var problem *commandbus.Error
			if !errors.As(e, &problem) || problem.Code != "NOT_FOUND" {
				return nil, e
			}
			out["anchorChanged"] = true
			out["stale"] = true
		} else {
			d := parse(doc.Body)
			version, e := q.GetVersion(ctx, doc.VersionID)
			if e != nil {
				return nil, e
			}
			out["anchorChanged"] = version.Deleted || !exactPending(r, d)
			if r.Status == "fulfilled" && r.Kind == KindComponent {
				n := obj(obj(d["nodes"])[r.NodeID])
				out["fulfillmentReverted"] = n == nil || n["pending"] != nil || n["type"] != r.Key
			} else if r.Status == "fulfilled" && r.Kind != KindAction {
				out["fulfillmentReverted"] = dataTokenPending(r, d)
			} else if r.Status == "fulfilled" {
				value := obj(obj(obj(d["nodes"])[r.NodeID])["on"])[r.Event]
				calls := arr(value)
				if calls == nil {
					calls = []any{value}
				}
				out["fulfillmentReverted"] = len(calls) != 1 || obj(calls[0])["action"] != r.Key
			}
			if len(packs) > 0 {
				h, e := manifest.Hash(d)
				if e != nil {
					return nil, e
				}
				env, e := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: a.ProjectID, Name: r.Environment})
				if e != nil {
					return nil, e
				}
				out["stale"] = h != packs[0].PageHash || cs.Seq != packs[0].ChangesetSeq || env.ActiveManifestID == nil || *env.ActiveManifestID != packs[0].ManifestID
			}
		}
	}
	return out, nil
}
func Preview(ctx context.Context, q *store.Queries, a auth.Actor, id uuid.UUID) (Pack, error) {
	r, e := read(ctx, q, a, id)
	if e != nil {
		return Pack{}, e
	}
	if !owns(a, r) || human(a) != nil {
		return Pack{}, notFound()
	}
	cs, e := q.GetChangeset(ctx, store.GetChangesetParams{ProjectID: a.ProjectID, ID: r.ChangesetID})
	if e != nil {
		return Pack{}, e
	}
	env, e := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: a.ProjectID, Name: r.Environment})
	if e != nil {
		return Pack{}, e
	}
	if env.ActiveManifestID == nil {
		return Pack{}, fail("CAPABILITY_CONTEXT_STALE")
	}
	m, e := q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: a.ProjectID, ID: *env.ActiveManifestID})
	if e != nil {
		return Pack{}, e
	}
	s, e := loadContext(ctx, q, a, r, cs.Seq, m.Hash, r.Status == "draft")
	if e != nil {
		return Pack{}, e
	}
	if r.Status == "draft" {
		s, e = projectPending(r, s)
		if e != nil {
			return Pack{}, e
		}
	} else {
		if terminal(r.Status) || !exactPending(r, s.doc) {
			return Pack{}, fail("CAPABILITY_ANCHOR_CHANGED")
		}
	}
	r.Revision++ // the next immutable version, identical to submit/refresh inputs
	return buildPack(ctx, q, r, s)
}
func Download(ctx context.Context, q *store.Queries, a auth.Actor, id uuid.UUID, revision int64) (Pack, error) {
	if _, e := read(ctx, q, a, id); e != nil {
		return Pack{}, e
	}
	p, e := q.GetCapabilityPack(ctx, store.GetCapabilityPackParams{ProjectID: a.ProjectID, RequestID: id, Revision: revision})
	if errors.Is(e, pgx.ErrNoRows) {
		return Pack{}, notFound()
	}
	if e != nil {
		return Pack{}, e
	}
	b, e := manifest.CanonicalJSON(jsonValue(p.Body))
	return Pack{Body: b, Hash: p.Hash, ManifestID: p.ManifestID, IndexID: p.IndexID, Seq: p.ChangesetSeq, PageHash: p.PageHash}, e
}

type Filter struct {
	Environment, Status, Kind, Key, Cursor string
	Limit                                  int
}
type cursor struct {
	Scope string    `json:"scope"`
	Time  time.Time `json:"time"`
	ID    uuid.UUID `json:"id"`
}

func List(ctx context.Context, q *store.Queries, a auth.Actor, f Filter) (object, error) {
	if a.Kind == auth.ActorAgent || author(a) != nil && !a.Rights.Has(auth.CodePropose) {
		return nil, forbidden()
	}
	if f.Limit == 0 {
		f.Limit = 25
	}
	if f.Limit < 1 || f.Limit > 100 || f.Kind != "" && f.Kind != "action" || len(f.Key) > 200 {
		return nil, fail("CAPABILITY_FILTER_INVALID")
	}
	if f.Status != "" && f.Status != "draft" && f.Status != "submitted" && f.Status != "accepted" && f.Status != "in_progress" && f.Status != "pr_open" && !terminal(f.Status) {
		return nil, fail("CAPABILITY_FILTER_INVALID")
	}
	actorID := uuid.Nil
	if author(a) == nil {
		actorID = a.ID
	}
	p := store.ListCapabilityRequestsParams{ProjectID: a.ProjectID, ActorID: actorID, Engineer: a.Rights.Has(auth.CodePropose), Environment: f.Environment, Status: f.Status, Key: f.Key, PageLimit: int32(f.Limit + 1)}
	fingerprint, _ := manifest.Hash(object{"project": a.ProjectID.String(), "actor": a.ID.String(), "environment": f.Environment, "status": f.Status, "key": f.Key, "kind": f.Kind})
	if f.Cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(f.Cursor)
		var c cursor
		if e != nil || len(b) > 1024 || json.Unmarshal(b, &c) != nil || c.Scope != fingerprint || c.ID == uuid.Nil || c.Time.IsZero() {
			return nil, fail("CAPABILITY_CURSOR_INVALID")
		}
		p.BeforeTime = &c.Time
		p.BeforeID = &c.ID
	}
	rows, e := q.ListCapabilityRequests(ctx, p)
	if e != nil {
		return nil, e
	}
	more := len(rows) > f.Limit
	if more {
		rows = rows[:f.Limit]
	}
	items := []any{}
	for _, r := range rows {
		if visible(a, r) {
			items = append(items, projection(r))
		}
	}
	var next any
	if more {
		last := rows[len(rows)-1]
		next = base64.RawURLEncoding.EncodeToString(raw(cursor{Scope: fingerprint, Time: last.UpdatedAt, ID: last.ID}))
	}
	return object{"items": items, "nextCursor": next}, nil
}

func Filename(id uuid.UUID, version int64) string {
	return fmt.Sprintf("capability-%s-v%d.json", id, version)
}
