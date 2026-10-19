package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/bindingdoc"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Preview mapping states (STU-319). They are Query fields, never command errors.
const (
	PreviewReady                = "ready"
	PreviewReadyLastKnown       = "ready_last_known"
	PreviewCommitBehind         = "commit_behind"
	PreviewCommitUnverified     = "commit_unverified"
	PreviewKeyMissing           = "key_missing"
	PreviewContractIncompatible = "contract_incompatible"
	PreviewNotRegistered        = "not_registered"
	PreviewNoGithubLink         = "no_github_link"
	PreviewEnvironmentExpired   = "environment_expired"
)

func previewError(status int, code string) error {
	return commandbus.NewError(status, code, "Preview недоступен", code)
}

// PreviewSource resolves the enabled GitHub configuration of a standard
// environment for create-environment (STU-315). The repository is never taken
// from the payload.
func (s *GithubService) PreviewSource(ctx context.Context, q *store.Queries, project uuid.UUID, base string) (projects.Source, error) {
	invalid := previewError(http.StatusConflict, "PREVIEW_SOURCE_INVALID")
	env, e := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: project, Name: base})
	if errors.Is(e, pgx.ErrNoRows) || e == nil && env.Kind != "standard" {
		return projects.Source{}, invalid
	}
	if e != nil {
		return projects.Source{}, e
	}
	c, e := q.GetCapabilityGithubConfig(ctx, store.GetCapabilityGithubConfigParams{ProjectID: project, Environment: base})
	if errors.Is(e, pgx.ErrNoRows) {
		return projects.Source{}, invalid
	}
	if e != nil {
		return projects.Source{}, e
	}
	repo, ok := s.repository(c)
	if !ok {
		return projects.Source{}, invalid
	}
	return projects.Source{BaseEnvironmentID: env.ID, ConfigID: c.ID, RepositoryID: repo.RepositoryID}, nil
}

// previewMatch is the computed request ↔ preview relation (STU-318/319). It
// performs no network calls and stores nothing.
type previewMatch struct {
	State        string
	Registration *store.PreviewRegistration
	Observation  *store.CapabilityGithubObservation
	HeadSHA      string
	Hash         string
	Action       object
}

func matchPreview(ctx context.Context, q *store.Queries, r store.CapabilityRequest, env store.Environment, now time.Time) (previewMatch, error) {
	m := previewMatch{State: PreviewNoGithubLink}
	if env.ArchivedAt != nil {
		m.State = PreviewEnvironmentExpired
		return m, nil
	}
	if env.SourcePullNumber == nil || *env.SourceBaseEnvironmentID != r.EnvironmentID {
		return m, nil
	}
	l, e := q.GetCapabilityGithubLink(ctx, store.GetCapabilityGithubLinkParams{ProjectID: r.ProjectID, RequestID: r.ID})
	if errors.Is(e, pgx.ErrNoRows) {
		return m, nil
	}
	if e != nil {
		return m, e
	}
	if !l.Active || l.ConfigID != *env.SourceConfigID || l.PullNumber != *env.SourcePullNumber {
		return m, nil
	}
	reg, e := q.CurrentPreviewRegistration(ctx, store.CurrentPreviewRegistrationParams{ProjectID: r.ProjectID, EnvironmentID: env.ID})
	if errors.Is(e, pgx.ErrNoRows) {
		m.State = PreviewNotRegistered
		return m, nil
	}
	if e != nil {
		return m, e
	}
	m.Registration = &reg
	o, e := q.LatestCapabilityGithubObservation(ctx, store.LatestCapabilityGithubObservationParams{ProjectID: r.ProjectID, RequestID: r.ID})
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return m, e
	}
	if errors.Is(e, pgx.ErrNoRows) || o.Generation != l.Generation {
		m.State = PreviewCommitUnverified
		return m, nil
	}
	m.Observation = &o
	facts := parse(o.Body)
	if facts["repositoryId"] != *env.SourceRepositoryID {
		m.State = PreviewNoGithubLink
		return m, nil
	}
	m.HeadSHA, _ = facts["headSha"].(string)
	if m.HeadSHA != reg.Commit {
		m.State = PreviewCommitUnverified
		if o.ObservedAt.After(reg.RegisteredAt) {
			m.State = PreviewCommitBehind
		}
		return m, nil
	}
	man, e := q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: r.ProjectID, ID: reg.ManifestID})
	if e != nil {
		return m, e
	}
	m.Hash = man.Hash
	m.Action = obj(obj(parse(man.Body)["actions"])[r.Key])
	if m.Action == nil {
		m.State = PreviewKeyMissing
		return m, nil
	}
	if !contractCompatible(r, m.Action) {
		m.State = PreviewContractIncompatible
		return m, nil
	}
	// Decision §11.2: last-known is enough for viewing, with its age shown.
	m.State = PreviewReadyLastKnown
	if now.Before(o.FreshUntil) && l.LastError == "" {
		m.State = PreviewReady
	}
	return m, nil
}

// contractCompatible repeats the resolve rule (STU-268) against another manifest.
func contractCompatible(r store.CapabilityRequest, action object) bool {
	proposed := obj(parse(r.ProposedContract)["args"])
	args := obj(action["args"])
	for key, field := range proposed {
		if !bindingdoc.SimpleContentCompatible(obj(field), obj(args[key])) {
			return false
		}
	}
	for key, field := range args {
		if proposed[key] == nil && obj(field)["required"] == true && obj(field)["default"] == nil {
			return false
		}
	}
	return true
}

func needsArgs(action object) bool {
	for _, field := range obj(action["args"]) {
		if obj(field)["required"] == true && obj(field)["default"] == nil {
			return true
		}
	}
	return false
}

func ready(state string) bool { return state == PreviewReady || state == PreviewReadyLastKnown }

// substitute replaces the exact pending chain of the request with one call. It
// returns false when the anchor no longer holds that pending marker.
func substitute(doc object, r store.CapabilityRequest, call object) bool {
	on := obj(obj(obj(doc["nodes"])[r.NodeID])["on"])
	if on == nil || !exactPending(r, doc) {
		return false
	}
	on[r.Event] = []any{call}
	return true
}

// previewCall checks the call against the preview manifest (STU-320): L1–L4
// diagnostics of the anchored event block the substitution.
func previewCall(ctx context.Context, q *store.Queries, r store.CapabilityRequest, env store.Environment, body []byte, call object, lock bool) (bool, error) {
	doc := parse(body)
	if !substitute(doc, r, call) {
		return false, nil
	}
	load := validation.LoadSnapshot
	if lock {
		load = validation.Load
	}
	c, e := load(ctx, q, r.ProjectID, env.Name, &r.ChangesetID)
	if e != nil {
		return false, e
	}
	problems, e := c.CheckPublication(ctx, []validation.Document{{ObjectID: r.DocumentID, Body: raw(doc)}})
	if e != nil {
		return false, e
	}
	for _, d := range problems[r.DocumentID.String()] {
		if d.NodeID == r.NodeID && (strings.Contains(d.Pointer, "/on/") || strings.HasPrefix(string(d.Code), "ACTION_")) {
			return false, nil
		}
	}
	return true, nil
}

// storedCall returns the preview call for the current manifest, or the bare
// action when it has no required args (STU-321), with the saved args revision.
func storedCall(ctx context.Context, q *store.Queries, r store.CapabilityRequest, env store.Environment, m previewMatch) (object, string, int64, error) {
	a, e := q.GetCapabilityPreviewArgs(ctx, store.GetCapabilityPreviewArgsParams{ProjectID: r.ProjectID, RequestID: r.ID, EnvironmentID: env.ID})
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return nil, "", 0, e
	}
	switch {
	case e == nil && a.ManifestID == m.Registration.ManifestID:
		return parse(a.Args), "set", a.Revision, nil
	case !needsArgs(m.Action):
		return object{"action": r.Key}, "not_required", a.Revision, nil
	case e == nil:
		return nil, "stale", a.Revision, nil
	}
	return nil, "missing", 0, nil
}

// SetPreviewArgs — payload set-capability-preview-args (STU-321).
type SetPreviewArgs struct {
	RequestID               uuid.UUID       `json:"requestId"`
	ExpectedRequestRevision int64           `json:"expectedRequestRevision"`
	Environment             string          `json:"environment"`
	ManifestHash            string          `json:"manifestHash"`
	Handlers                json.RawMessage `json:"handlers"`
	ExpectedArgsRevision    int64           `json:"expectedArgsRevision"`
}

func registerPreviewArgs(bus *commandbus.Bus) {
	commandbus.Register(bus, commandbus.Command[SetPreviewArgs, object]{Name: "set-capability-preview-args", Authorize: func(a auth.Actor, _ SetPreviewArgs) error { return human(a) }, Validate: func(p SetPreviewArgs) error {
		if p.RequestID == uuid.Nil || p.ExpectedRequestRevision < 1 || p.ExpectedArgsRevision < 0 || p.Environment == "" || !codeHash(p.ManifestHash) || len(p.Handlers) == 0 || len(p.Handlers) > 64*1024 {
			return fail("CAPABILITY_REQUEST_INVALID")
		}
		return nil
	}, Handle: setPreviewArgs})
}

func setPreviewArgs(ctx context.Context, tx pgx.Tx, a auth.Actor, p SetPreviewArgs) (object, error) {
	if e := reason(ctx); e != nil {
		return nil, e
	}
	q := store.New(tx)
	r, e := lockRequest(ctx, q, a, p.RequestID, p.ExpectedRequestRevision)
	if e != nil {
		return nil, e
	}
	if !owns(a, r) || author(a) != nil || !a.Rights.Has(auth.BehaviorUse) {
		return nil, notFound()
	}
	if r.Status == "draft" || terminal(r.Status) {
		return nil, fail("CAPABILITY_STATE_INVALID")
	}
	env, e := q.LockManifestEnvironment(ctx, store.LockManifestEnvironmentParams{ProjectID: a.ProjectID, Name: p.Environment})
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, notFound()
	}
	if e != nil {
		return nil, e
	}
	m, e := matchPreview(ctx, q, r, env, time.Now())
	if e != nil {
		return nil, e
	}
	if !ready(m.State) {
		return nil, previewError(http.StatusConflict, "PREVIEW_NOT_READY")
	}
	if m.Hash != p.ManifestHash {
		return nil, previewError(http.StatusConflict, "PREVIEW_ARGS_STALE")
	}
	calls := arr(jsonValue(p.Handlers))
	if len(calls) != 1 || obj(calls[0])["action"] != r.Key {
		return nil, previewError(http.StatusUnprocessableEntity, "PREVIEW_ARGS_INVALID")
	}
	_, _, revision, e := storedCall(ctx, q, r, env, m)
	if e != nil {
		return nil, e
	}
	if revision != p.ExpectedArgsRevision {
		return nil, fail("CAPABILITY_REVISION_CONFLICT")
	}
	doc, e := q.ChangesetWorkingVersions(ctx, r.ChangesetID)
	if e != nil {
		return nil, e
	}
	var body []byte
	for _, v := range doc {
		if v.ObjectID == r.DocumentID {
			body = v.Body
		}
	}
	valid, e := previewCall(ctx, q, r, env, body, obj(calls[0]), true)
	if e != nil {
		return nil, e
	}
	if !valid {
		return nil, previewError(http.StatusUnprocessableEntity, "PREVIEW_ARGS_INVALID")
	}
	saved, e := q.SetCapabilityPreviewArgs(ctx, store.SetCapabilityPreviewArgsParams{ProjectID: a.ProjectID, RequestID: r.ID, EnvironmentID: env.ID, ManifestID: m.Registration.ManifestID, Args: raw(calls[0]), SetBy: a.ID})
	if e != nil {
		return nil, e
	}
	if e = projects.Audit(ctx, q, env, &a.ID, "preview_args_set", commandbus.ReasonFrom(ctx), map[string]any{"requestId": r.ID, "revision": saved.Revision}); e != nil {
		return nil, e
	}
	return object{"requestId": r.ID, "environment": env.Name, "manifestHash": m.Hash, "handlers": []any{jsonValue(saved.Args)}, "revision": saved.Revision}, nil
}

func previewView(env store.Environment, m previewMatch, argsStatus string, argsRevision int64) object {
	name := env.Name
	if env.ArchivedName != nil {
		name = *env.ArchivedName
	}
	out := object{"environment": name, "pullNumber": env.SourcePullNumber, "state": m.State, "expiresAt": env.ExpiresAt, "archivedAt": env.ArchivedAt, "archiveReason": env.ArchiveReason, "registration": nil, "observation": nil, "keyAvailable": m.Action != nil, "args": object{"status": argsStatus, "revision": argsRevision}}
	if m.Registration != nil {
		out["registration"] = object{"seq": m.Registration.Seq, "commit": m.Registration.Commit, "registeredAt": m.Registration.RegisteredAt, "appUrl": m.Registration.AppUrl, "manifestHash": m.Hash}
	}
	if m.Observation != nil {
		out["observation"] = object{"headSha": m.HeadSHA, "observedAt": m.Observation.ObservedAt, "freshUntil": m.Observation.FreshUntil}
	}
	if m.Action != nil {
		out["actionArgs"] = m.Action["args"]
	}
	return out
}

// PreviewGet lists preview environments of the request's PR (STU-318…321, §5).
// Access follows the GitHub projection; reading never calls GitHub.
func (s *GithubService) PreviewGet(ctx context.Context, q *store.Queries, a auth.Actor, id uuid.UUID) (object, error) {
	r, e := read(ctx, q, a, id)
	if e != nil {
		return nil, e
	}
	if r.Status == "draft" {
		return nil, notFound()
	}
	out := object{"items": []any{}, "canOpen": owns(a, r) && author(a) == nil && a.Rights.Has(auth.DesignRead), "canSetArgs": owns(a, r) && author(a) == nil && a.Rights.Has(auth.BehaviorUse) && !terminal(r.Status)}
	l, e := q.GetCapabilityGithubLink(ctx, store.GetCapabilityGithubLinkParams{ProjectID: a.ProjectID, RequestID: r.ID})
	if errors.Is(e, pgx.ErrNoRows) || e == nil && !l.Active {
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	envs, e := q.PreviewEnvironmentsForSource(ctx, store.PreviewEnvironmentsForSourceParams{ProjectID: a.ProjectID, SourceConfigID: &l.ConfigID, SourcePullNumber: &l.PullNumber})
	if e != nil {
		return nil, e
	}
	items := []any{}
	now := time.Now()
	for _, env := range envs {
		if *env.SourceBaseEnvironmentID != r.EnvironmentID {
			continue
		}
		m, e := matchPreview(ctx, q, r, env, now)
		if e != nil {
			return nil, e
		}
		status, revision := "missing", int64(0)
		if ready(m.State) {
			if _, status, revision, e = storedCall(ctx, q, r, env, m); e != nil {
				return nil, e
			}
		}
		items = append(items, previewView(env, m, status, revision))
	}
	out["items"] = items
	return out, nil
}

// ProjectPreview is the Delivery preview projection (STU-320/322). For a preview
// environment with a source it replaces exact pending markers of ready requests
// in the Change Set with their call; everything else is returned unchanged.
func ProjectPreview(ctx context.Context, q *store.Queries, project, environment uuid.UUID, changeset *uuid.UUID, document uuid.UUID, body []byte) ([]byte, error) {
	if changeset == nil {
		return body, nil
	}
	env, e := q.GetEnvironmentByID(ctx, store.GetEnvironmentByIDParams{ProjectID: project, ID: environment})
	if e != nil {
		return nil, e
	}
	if env.SourcePullNumber == nil || env.ArchivedAt != nil {
		return body, nil
	}
	doc := parse(body)
	changed := false
	now := time.Now()
	for _, id := range pendingRequests(doc) {
		r, e := q.GetCapabilityRequest(ctx, store.GetCapabilityRequestParams{ProjectID: project, ID: id})
		if errors.Is(e, pgx.ErrNoRows) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if r.ChangesetID != *changeset || r.DocumentID != document || r.Status == "draft" || terminal(r.Status) {
			continue
		}
		m, e := matchPreview(ctx, q, r, env, now)
		if e != nil {
			return nil, e
		}
		if !ready(m.State) {
			continue
		}
		call, _, _, e := storedCall(ctx, q, r, env, m)
		if e != nil {
			return nil, e
		}
		if call == nil {
			continue
		}
		valid, e := previewCall(ctx, q, r, env, raw(doc), call, false)
		if e != nil {
			return nil, e
		}
		if valid && substitute(doc, r, call) {
			changed = true
		}
	}
	if !changed {
		return body, nil
	}
	return raw(doc), nil
}

// pendingRequests lists request IDs of pending markers in stable node order.
func pendingRequests(doc object) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := []uuid.UUID{}
	nodes := obj(doc["nodes"])
	for _, nodeID := range keys(nodes) {
		on := obj(obj(nodes[nodeID])["on"])
		for _, event := range keys(on) {
			for _, call := range handlerChain(on[event]) {
				name, _ := obj(call)["action"].(string)
				if id, err := uuid.Parse(strings.TrimPrefix(name, "pending:")); err == nil && strings.HasPrefix(name, "pending:") && !seen[id] {
					seen[id] = true
					out = append(out, id)
				}
			}
		}
	}
	return out
}

// ChangesetPreview returns diagnostics of the Change Set against a PR preview
// manifest and the pending substitutions (STU-323). Nothing is recorded.
func ChangesetPreview(ctx context.Context, q *store.Queries, a auth.Actor, cs uuid.UUID, environment string) (object, error) {
	if a.Kind == auth.ActorService || !a.Rights.Has(auth.DesignRead) {
		return nil, notFound()
	}
	set, e := q.GetChangeset(ctx, store.GetChangesetParams{ProjectID: a.ProjectID, ID: cs})
	if errors.Is(e, pgx.ErrNoRows) || e == nil && (set.Kind != "standard" || a.Kind == auth.ActorAgent && !auth.AgentAllowsCS(a, cs)) {
		return nil, notFound()
	}
	if e != nil {
		return nil, e
	}
	env, e := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: a.ProjectID, Name: environment})
	if errors.Is(e, pgx.ErrNoRows) || e == nil && env.SourcePullNumber == nil {
		return nil, notFound()
	}
	if e != nil {
		return nil, e
	}
	versions, e := q.ChangesetWorkingVersions(ctx, cs)
	if e != nil {
		return nil, e
	}
	docs := make([]validation.Document, 0, len(versions))
	substitutions := []any{}
	now := time.Now()
	for _, v := range versions {
		docs = append(docs, validation.Document{ObjectID: v.ObjectID, VersionID: v.VersionID, Body: v.Body, Path: v.Path, Certified: v.Certified})
		for _, id := range pendingRequests(parse(v.Body)) {
			r, e := q.GetCapabilityRequest(ctx, store.GetCapabilityRequestParams{ProjectID: a.ProjectID, ID: id})
			if errors.Is(e, pgx.ErrNoRows) {
				continue
			}
			if e != nil {
				return nil, e
			}
			if r.ChangesetID != cs || r.DocumentID != v.ObjectID {
				continue
			}
			state := PreviewNoGithubLink
			substituted := false
			if r.Status != "draft" && !terminal(r.Status) {
				m, e := matchPreview(ctx, q, r, env, now)
				if e != nil {
					return nil, e
				}
				state = m.State
				if ready(m.State) {
					call, _, _, e := storedCall(ctx, q, r, env, m)
					if e != nil {
						return nil, e
					}
					if call != nil {
						if substituted, e = previewCall(ctx, q, r, env, v.Body, call, false); e != nil {
							return nil, e
						}
					}
				}
			}
			substitutions = append(substitutions, object{"requestId": r.ID, "documentId": r.DocumentID, "nodeId": r.NodeID, "event": r.Event, "key": r.Key, "state": state, "substituted": substituted})
		}
	}
	c, e := validation.LoadSnapshot(ctx, q, a.ProjectID, env.Name, &cs)
	if e != nil {
		return nil, e
	}
	problems, e := c.CheckPublication(ctx, docs)
	if e != nil {
		return nil, e
	}
	return object{"environment": env.Name, "registrationSeq": env.RegistrationSeq, "diagnostics": problems, "substitutions": substitutions}, nil
}
