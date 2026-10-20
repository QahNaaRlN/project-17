package capabilities

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	gh "github.com/qahnaarln/project-17/apps/server/internal/capabilitygithub"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/jobs"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/riverqueue/river"
)

// FulfillmentService evaluates activations and replaces pending markers
// (STU-338…347). GitHub facts come from the existing GitHub service.
type FulfillmentService struct {
	Pool     *pgxpool.Pool
	Github   *GithubService
	Comparer gh.Comparer // GitHub compare; required
	Queue    *jobs.Client
	Now      func() time.Time
}

func (s *FulfillmentService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// FulfillmentArgs — the periodic River tick of the fulfillment outbox.
type FulfillmentArgs struct{}

func (FulfillmentArgs) Kind() string                 { return "capability_fulfillment" }
func (FulfillmentArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 1} }

type FulfillmentWorker struct {
	river.WorkerDefaults[FulfillmentArgs]
	Service *FulfillmentService
}

func (w *FulfillmentWorker) Work(ctx context.Context, _ *river.Job[FulfillmentArgs]) error {
	return w.Service.Run(ctx)
}

// Run processes due evaluation jobs, then due anchor groups.
func (s *FulfillmentService) Run(ctx context.Context) error {
	for range 20 {
		job, e := store.New(s.Pool).ClaimFulfillmentJob(ctx)
		if errors.Is(e, pgx.ErrNoRows) {
			break
		}
		if e != nil {
			return e
		}
		state, next, why := "done", s.now(), ""
		if e = s.Evaluate(ctx, job); e != nil {
			state, why = "queued", "temporary"
			next = s.now().Add(time.Minute)
			if job.Attempts >= 3 {
				state = "done"
			}
		}
		if e := store.New(s.Pool).FinishFulfillmentJob(ctx, store.FinishFulfillmentJobParams{ID: job.ID, State: state, NextAt: next, LastError: why}); e != nil {
			return e
		}
	}
	groups, e := store.New(s.Pool).DueAnchorChangesets(ctx)
	if e != nil {
		return e
	}
	for _, g := range groups {
		if e = s.Replace(ctx, g.ProjectID, g.ChangesetID); e != nil {
			return e
		}
	}
	return nil
}

// decision is the outcome of STU-339 for one request.
type decision struct {
	reason      string
	commit      string
	observation uuid.UUID
	basis       object
}

// Evaluate applies STU-339/340 to the requests a job covers.
func (s *FulfillmentService) Evaluate(ctx context.Context, job store.CapabilityFulfillmentJob) error {
	q := store.New(s.Pool)
	env, e := q.GetEnvironmentByID(ctx, store.GetEnvironmentByIDParams{ProjectID: job.ProjectID, ID: job.EnvironmentID})
	if e != nil {
		return e
	}
	if env.Kind != "standard" || env.ActiveManifestID == nil || *env.ActiveManifestID != job.ManifestID {
		return nil // only the active standard manifest fulfills (STU-338)
	}
	m, e := q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: job.ProjectID, ID: job.ManifestID})
	if e != nil {
		return e
	}
	app := parse(m.Body)
	commit := ""
	if m.CodeIndexKey != nil {
		upload, e := q.GetCodeIndexUpload(ctx, store.GetCodeIndexUploadParams{ProjectID: job.ProjectID, ID: uuid.MustParse(*m.CodeIndexKey)})
		if e != nil {
			return e
		}
		commit = upload.RepoCommit
	}
	requests, e := q.FulfillmentCandidates(ctx, store.FulfillmentCandidatesParams{ProjectID: job.ProjectID, EnvironmentID: env.ID, Request: job.RequestID})
	if e != nil {
		return e
	}
	for _, r := range requests {
		action := definition(app, r)
		if action == nil && job.RequestID == nil {
			continue
		}
		d, e := s.decide(ctx, q, r, action, commit)
		if e != nil {
			return e
		}
		if e = s.record(ctx, r, m, d); e != nil {
			return e
		}
	}
	return nil
}

func (s *FulfillmentService) decide(ctx context.Context, q *store.Queries, r store.CapabilityRequest, action object, commit string) (decision, error) {
	switch {
	case action == nil:
		return decision{reason: "key_missing"}, nil
	case !contractCompatible(r, action):
		return decision{reason: "contract_incompatible"}, nil
	}
	l, e := q.GetCapabilityGithubLink(ctx, store.GetCapabilityGithubLinkParams{ProjectID: r.ProjectID, RequestID: r.ID})
	if errors.Is(e, pgx.ErrNoRows) || e == nil && !l.Active {
		return decision{reason: "no_github_link"}, nil // decision §11.5: manual resolve only
	}
	if e != nil {
		return decision{}, e
	}
	c, e := q.GetCapabilityGithubConfig(ctx, store.GetCapabilityGithubConfigParams{ProjectID: r.ProjectID, Environment: r.Environment})
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return decision{}, e
	}
	repo, ready := s.Github.repository(c)
	if errors.Is(e, pgx.ErrNoRows) || !ready || l.ConfigID != c.ID {
		return decision{reason: "no_github_link"}, nil
	}
	o, e := q.LatestCapabilityGithubObservation(ctx, store.LatestCapabilityGithubObservationParams{ProjectID: r.ProjectID, RequestID: r.ID})
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return decision{}, e
	}
	current := e == nil && o.Generation == l.Generation && o.ConfigRevision == c.Revision && o.Fingerprint == c.Fingerprint && l.LastError == "" && s.now().Before(o.FreshUntil)
	if !current {
		return decision{reason: "observation_stale"}, s.refresh(ctx, r, l)
	}
	facts := parse(o.Body)
	merge, _ := facts["integrationSha"].(string)
	if facts["state"] != "merged" || facts["integrationType"] != "merged_commit" || merge == "" {
		return decision{reason: "pr_not_merged"}, nil
	}
	for _, v := range arr(facts["results"]) {
		if res := obj(v); res["target"] == "integration" && res["outcome"] != "success" {
			return decision{reason: "ci_not_successful"}, nil // decision §11.4
		}
	}
	if commit == "" {
		return decision{reason: "no_code_index"}, nil
	}
	basis := object{"pullNumber": l.PullNumber, "repositoryId": repo.RepositoryID, "integrationSha": merge, "commit": commit, "observationId": o.ID, "observedAt": o.ObservedAt}
	if commit != merge {
		cmp, e := s.Comparer.Compare(ctx, repo, merge, commit)
		if e != nil {
			return decision{reason: "comparison_unavailable"}, nil
		}
		if !cmp.Descends() {
			return decision{reason: "commit_unrelated"}, nil
		}
		basis["comparison"] = cmp
	}
	return decision{commit: commit, observation: o.ID, basis: basis}, nil
}

// refresh asks the GitHub service for fresh facts within its budgets (STU-340).
func (s *FulfillmentService) refresh(ctx context.Context, r store.CapabilityRequest, l store.CapabilityGithubLink) error {
	if l.State == "queued" || l.State == "sending" || l.State == "retry_wait" || s.now().Before(l.RefreshAfter) {
		return nil
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)
	count, e := q.CapabilityGithubRefreshCount(ctx, r.ProjectID)
	if e != nil || count >= 20 {
		return e
	}
	if _, e = q.SetCapabilityGithubLink(ctx, store.SetCapabilityGithubLinkParams{ID: l.ID, ProjectID: l.ProjectID, RequestID: l.RequestID, ConfigID: l.ConfigID, ConfigRevision: l.ConfigRevision, Fingerprint: l.Fingerprint, PullNumber: l.PullNumber, InitiatedBy: l.InitiatedBy, SyncID: uuid.Must(uuid.NewV7())}); e == nil {
		e = q.InsertCapabilityGithubAudit(ctx, store.InsertCapabilityGithubAuditParams{ID: uuid.Must(uuid.NewV7()), ProjectID: r.ProjectID, RequestID: &r.ID, Event: "refresh", Generation: l.Generation + 1, Reason: "fulfillment_check"})
	}
	if e == nil && s.Queue != nil {
		_, e = s.Queue.InsertTx(ctx, tx, GithubArgs{ProjectID: r.ProjectID, RequestID: r.ID}, nil)
	}
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// record commits the decision for one request in a short transaction (STU-341).
func (s *FulfillmentService) record(ctx context.Context, r store.CapabilityRequest, m store.Manifest, d decision) error {
	return inProject(ctx, s.Pool, r.ProjectID, func(q *store.Queries) (bool, error) {
		current, e := q.LockCapabilityRequest(ctx, store.LockCapabilityRequestParams{ProjectID: r.ProjectID, ID: r.ID})
		var env store.Environment
		if e == nil {
			env, e = q.GetEnvironmentByID(ctx, store.GetEnvironmentByIDParams{ProjectID: r.ProjectID, ID: r.EnvironmentID})
		}
		if e != nil {
			return false, e
		}
		if current.Revision != r.Revision || terminal(current.Status) || env.ActiveManifestID == nil || *env.ActiveManifestID != m.ID {
			return false, nil // changed meanwhile; a later event re-evaluates
		}
		if d.reason != "" {
			if current.FulfillmentReason == d.reason {
				return false, nil
			}
			return true, q.SetFulfillmentWaiting(ctx, store.SetFulfillmentWaitingParams{ProjectID: r.ProjectID, ID: r.ID, FulfillmentReason: d.reason})
		}
		mode := "automatic"
		fulfilled, e := q.FulfillCapabilityRequest(ctx, store.FulfillCapabilityRequestParams{ProjectID: r.ProjectID, ID: r.ID, Revision: r.Revision, FulfilledManifestID: &m.ID, FulfillmentMode: &mode, FulfillmentCommit: &d.commit, FulfillmentObservationID: &d.observation, FulfillmentBasis: raw(d.basis)})
		if e == nil {
			e = q.QueueRequestAnchors(ctx, store.QueueRequestAnchorsParams{ProjectID: r.ProjectID, RequestID: r.ID})
		}
		if e == nil {
			e = q.InsertCapabilityAudit(ctx, store.InsertCapabilityAuditParams{ID: uuid.Must(uuid.NewV7()), ProjectID: r.ProjectID, RequestID: r.ID, Revision: fulfilled.Revision, ActorID: mustSystem(ctx, q, r.ProjectID), Event: "fulfilled_automatically", Reason: "manifest " + m.Hash, Data: raw(object{"status": fulfilled.Status, "basis": d.basis})})
		}
		if e == nil {
			e = fulfillmentAudit(ctx, q, r.ProjectID, r.ID, nil, nil, "fulfilled", "automatic", d.basis)
		}
		return e == nil, e
	})
}

func mustSystem(ctx context.Context, q *store.Queries, project uuid.UUID) uuid.UUID {
	row, e := q.GetSystemActor(ctx, &project)
	if e != nil {
		return uuid.Nil
	}
	return row.ID
}

// Replace performs the per-Change-Set group replacement (STU-342…347).
func (s *FulfillmentService) Replace(ctx context.Context, project, cs uuid.UUID) error {
	for range 4 {
		bad, e := s.replaceOnce(ctx, project, cs)
		if e != nil || len(bad) == 0 {
			return e
		}
		// One invalid anchor rolled the group back; mark it and retry the rest.
		e = inProject(ctx, s.Pool, project, func(q *store.Queries) (bool, error) {
			for id, why := range bad {
				a, e := q.LockCapabilityAnchor(ctx, store.LockCapabilityAnchorParams{ProjectID: project, ID: id})
				if e == nil {
					_, e = q.SaveCapabilityAnchor(ctx, store.SaveCapabilityAnchorParams{ProjectID: project, ID: id, State: why, FulfillmentArgs: a.FulfillmentArgs, Attempts: a.Attempts + 1, NextAt: s.now(), LastError: why, ReplacedBatchID: a.ReplacedBatchID})
				}
				if e == nil {
					e = fulfillmentAudit(ctx, q, project, a.RequestID, &a.ID, nil, "replacement_refused", why, object{})
				}
				if e != nil {
					return false, e
				}
			}
			return true, nil
		})
		if e != nil {
			return e
		}
	}
	return nil
}

type pendingReplacement struct {
	anchor  store.CapabilityRequestAnchor
	call    object
	refused string // anchor state when the group refuses this replacement
	request store.CapabilityRequest
}

func (s *FulfillmentService) replaceOnce(ctx context.Context, project, csID uuid.UUID) (map[uuid.UUID]string, error) {
	bad := map[uuid.UUID]string{}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)
	var cs store.Changeset
	var anchors []store.CapabilityRequestAnchor
	if e = validation.LockProjectEnvironments(ctx, q, project); e == nil {
		cs, e = changes.Lock(ctx, q, project, csID)
	}
	if e == nil {
		e = q.RequeueReviewedAnchors(ctx, store.RequeueReviewedAnchorsParams{ProjectID: project, ChangesetID: csID})
	}
	if e == nil {
		anchors, e = q.ChangesetCapabilityAnchors(ctx, store.ChangesetCapabilityAnchorsParams{ProjectID: project, ChangesetID: csID})
	}
	if e != nil || len(anchors) == 0 {
		return nil, e
	}
	mark := func(a store.CapabilityRequestAnchor, state, why string) error {
		_, e := q.SaveCapabilityAnchor(ctx, store.SaveCapabilityAnchorParams{ProjectID: project, ID: a.ID, State: state, FulfillmentArgs: a.FulfillmentArgs, Attempts: a.Attempts, NextAt: s.now(), LastError: why, ReplacedBatchID: a.ReplacedBatchID})
		return e
	}
	// STU-347: merged or abandoned Change Sets never change; any other state
	// that is not open (review, approval, failed checks) waits for a reopen.
	if cs.State != "open" || cs.Kind != "standard" {
		state := "changeset_in_review"
		if cs.State == "merged" || cs.State == "abandoned" || cs.Kind != "standard" {
			state = "changeset_closed"
		}
		for _, a := range anchors {
			if e = mark(a, state, ""); e != nil {
				return nil, e
			}
		}
		return nil, tx.Commit(ctx)
	}
	var replacements []pendingReplacement
	working := map[uuid.UUID]object{}
	var operations []changes.OperationInput
	for _, a := range anchors {
		r, e := q.GetCapabilityRequest(ctx, store.GetCapabilityRequestParams{ProjectID: project, ID: a.RequestID})
		if e != nil {
			return nil, e
		}
		if r.Status != "fulfilled" {
			continue
		}
		env, e := q.GetEnvironmentByID(ctx, store.GetEnvironmentByIDParams{ProjectID: project, ID: r.EnvironmentID})
		var action object
		if e == nil && env.ActiveManifestID != nil {
			var m store.Manifest
			m, e = q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: project, ID: *env.ActiveManifestID})
			action = definition(parse(m.Body), r)
		}
		var doc changes.Document
		if e == nil {
			doc, e = changes.GetDocument(ctx, q, project, a.DocumentID, &csID)
		}
		if e != nil && !isNotFound(e) {
			return nil, e
		}
		state, why, call := "", "", object{"action": r.Key}
		switch {
		case action == nil:
			state, why = "failed", "key_missing"
		case e != nil || !anchorPending(r, a, parse(doc.Body)):
			state = "anchor_changed"
		case a.FulfillmentArgs != nil:
			call = parse(a.FulfillmentArgs)
		case needsArgs(action):
			state = "awaiting_args"
		}
		if state != "" {
			if e = mark(a, state, why); e != nil {
				return nil, e
			}
			continue
		}
		target := r
		target.NodeID, target.Event = a.NodeID, a.Event
		op, refused := behavior(target, []any{call}), "invalid_args"
		switch r.Kind {
		case KindComponent:
			op, refused = clearMarker(a.NodeID), "invalid_props" // STU-368/370
		case KindDataSource, KindToken:
			// Later replacements in the same document see the earlier ones (STU-390).
			if working[a.DocumentID] == nil {
				working[a.DocumentID] = parse(doc.Body)
			}
			op, refused = dataTokenClear(target, working[a.DocumentID]), "invalid_props"
			if _, e := ops.Apply(working[a.DocumentID], op, nil); e != nil {
				return nil, e
			}
		}
		operations = append(operations, changes.OperationInput{Type: op.Type, Target: &a.DocumentID, Payload: op.Payload})
		replacements = append(replacements, pendingReplacement{anchor: a, call: call, refused: refused, request: target})
	}
	if len(operations) == 0 {
		return nil, tx.Commit(ctx)
	}
	system, e := systemActor(ctx, q, project, cs.OwnerID)
	if e != nil {
		return nil, e
	}
	change, e := changes.ApplyInTx(ctx, tx, system, csID, cs.Seq, operations)
	if e != nil {
		for _, p := range replacements {
			bad[p.anchor.ID] = p.refused
		}
		if len(replacements) > 1 {
			// Find the offending anchor by failing alone next time.
			p := replacements[operationIndex(e)]
			bad = map[uuid.UUID]string{p.anchor.ID: p.refused}
		}
		return bad, nil
	}
	for _, diagnostics := range change.Warnings {
		for _, list := range diagnostics {
			for _, d := range list {
				for _, p := range replacements {
					if p.refused == "invalid_props" && placeErrors(p.request, p.anchor.NodeID, p.anchor.Event, map[string]map[string][]ir.Diagnostic{"": {"": {d}}}) || d.NodeID == p.anchor.NodeID && p.refused == "invalid_args" && (strings.Contains(d.Pointer, "/on/") || strings.HasPrefix(string(d.Code), "ACTION_")) {
						bad[p.anchor.ID] = p.refused
					}
				}
			}
		}
	}
	if len(bad) > 0 {
		return bad, nil // roll the group back (STU-342)
	}
	// The first operation of the group identifies the system replacement in history.
	var batch *uuid.UUID
	if len(change.Operations) > 0 {
		batch = &change.Operations[0].ID
	}
	for _, p := range replacements {
		if _, e = q.SaveCapabilityAnchor(ctx, store.SaveCapabilityAnchorParams{ProjectID: project, ID: p.anchor.ID, State: "replaced", FulfillmentArgs: p.anchor.FulfillmentArgs, Attempts: p.anchor.Attempts, NextAt: s.now(), LastError: "", ReplacedBatchID: batch}); e == nil {
			e = fulfillmentAudit(ctx, q, project, p.anchor.RequestID, &p.anchor.ID, &system.ID, "replaced", "automatic", object{"changesetId": csID, "seq": change.Changeset.Seq})
		}
		if e != nil {
			return nil, e
		}
	}
	if _, e = tx.Exec(ctx, `INSERT INTO project_revisions(project_id,revision) VALUES($1,1)
	 ON CONFLICT(project_id) DO UPDATE SET revision=project_revisions.revision+1`, project); e != nil {
		return nil, e
	}
	return nil, tx.Commit(ctx)
}

func isNotFound(e error) bool {
	var problem *commandbus.Error
	return errors.As(e, &problem) && problem.Code == "NOT_FOUND"
}

// operationIndex — the failing operation of a refused group, or 0.
func operationIndex(e error) int {
	var problem *commandbus.Error
	if errors.As(e, &problem) {
		if i, ok := problem.Params["operationIndex"].(int); ok {
			return i
		}
	}
	return 0
}
