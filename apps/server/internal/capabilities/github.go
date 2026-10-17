package capabilities

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	gh "github.com/qahnaarln/project-17/apps/server/internal/capabilitygithub"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/jobs"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

type GithubService struct {
	Pool    *pgxpool.Pool
	Catalog *gh.Catalog
	Fetcher gh.Fetcher
}
type SetGithubConfig struct {
	Environment            string `json:"environment"`
	ConfigID               string `json:"configId"`
	Enabled                bool   `json:"enabled"`
	ExpectedConfigRevision int64  `json:"expectedConfigRevision"`
}
type GithubAction struct {
	RequestID               uuid.UUID `json:"requestId"`
	PullNumber              int64     `json:"pullNumber,omitempty"`
	ExpectedRequestRevision int64     `json:"expectedRequestRevision"`
	ExpectedLinkRevision    int64     `json:"expectedLinkRevision"`
	ExpectedConfigRevision  int64     `json:"expectedConfigRevision,omitempty"`
	ConfirmReplace          bool      `json:"confirmReplace,omitempty"`
}

func githubError(code string) error {
	status := http.StatusConflict
	if code == "NOT_FOUND" {
		status = 404
	}
	if code == "FORBIDDEN" {
		status = 403
	}
	if code == "UNAVAILABLE" {
		status = 503
	}
	return commandbus.NewError(status, "CAPABILITY_GITHUB_"+code, "GitHub недоступен", code)
}
func githubAuthor(a auth.Actor) error {
	if a.Kind != auth.ActorHuman || !a.Rights.Has(auth.CodePropose) {
		return githubError("FORBIDDEN")
	}
	return nil
}
func (s *GithubService) Register(bus *commandbus.Bus) {
	commandbus.Register(bus, commandbus.Command[SetGithubConfig, object]{Name: "set-capability-github-config", Authorize: func(a auth.Actor, _ SetGithubConfig) error { return deliveryAdmin(a) }, Validate: func(p SetGithubConfig) error {
		if p.Environment == "" || p.ConfigID == "" || p.ExpectedConfigRevision < 0 {
			return githubError("STATE_INVALID")
		}
		return nil
	}, Handle: s.setGithubConfig})
	for _, name := range []string{"link-capability-github-pr", "unlink-capability-github-pr", "refresh-capability-github-pr"} {
		name := name
		commandbus.Register(bus, commandbus.Command[GithubAction, object]{Name: name, Authorize: func(a auth.Actor, _ GithubAction) error { return githubAuthor(a) }, Validate: func(p GithubAction) error {
			if p.RequestID == uuid.Nil || p.ExpectedRequestRevision < 1 || p.ExpectedLinkRevision < 0 || p.ExpectedConfigRevision < 0 || name == "link-capability-github-pr" && p.PullNumber < 1 || name != "link-capability-github-pr" && p.PullNumber != 0 || name == "unlink-capability-github-pr" && (p.ExpectedConfigRevision != 0 || p.ConfirmReplace) || name == "refresh-capability-github-pr" && p.ConfirmReplace {
				return githubError("STATE_INVALID")
			}
			return nil
		}, Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p GithubAction) (object, error) {
			return s.githubAction(ctx, tx, a, p, name)
		}})
	}
}
func githubAudit(ctx context.Context, q *store.Queries, a auth.Actor, request *uuid.UUID, event string, generation int64) error {
	return q.InsertCapabilityGithubAudit(ctx, store.InsertCapabilityGithubAuditParams{ID: uuid.Must(uuid.NewV7()), ProjectID: a.ProjectID, RequestID: request, ActorID: &a.ID, Event: event, Generation: generation, Reason: commandbus.ReasonFrom(ctx)})
}
func (s *GithubService) repository(c store.CapabilityGithubConfig) (gh.Repository, bool) {
	r, ok := s.Catalog.Find(c.ProjectID, c.Environment, c.ConfigID)
	return r, ok && c.Enabled && r.Fingerprint == c.Fingerprint && r.CredentialID == c.CredentialID
}
func githubConfigView(c store.CapabilityGithubConfig, r gh.Repository, ready bool) object {
	return object{"configured": true, "configId": c.ConfigID, "environment": c.Environment, "revision": c.Revision, "enabled": c.Enabled, "ready": ready, "repositoryId": r.RepositoryID, "owner": r.Owner, "name": r.Name, "baseBranch": r.BaseBranch, "requiredChecks": r.RequiredChecks}
}
func githubLinkView(l store.CapabilityGithubLink) object {
	return object{"id": l.ID, "requestId": l.RequestID, "revision": l.Revision, "generation": l.Generation, "active": l.Active, "pullNumber": l.PullNumber, "configRevision": l.ConfigRevision, "syncId": l.SyncID, "syncState": l.State, "lastError": l.LastError, "nextAllowedAt": l.RefreshAfter, "nextAttemptAt": l.NextAt, "pollsLeft": l.PollsLeft, "attempts": l.Attempts}
}
func (s *GithubService) setGithubConfig(ctx context.Context, tx pgx.Tx, a auth.Actor, p SetGithubConfig) (object, error) {
	if e := reason(ctx); e != nil {
		return nil, e
	}
	q := store.New(tx)
	if e := validation.LockProjectEnvironments(ctx, q, a.ProjectID); e != nil {
		return nil, e
	}
	live, e := liveDeliveryHuman(ctx, q, a.ProjectID, a.ID)
	if e != nil {
		return nil, e
	}
	if deliveryAdmin(live) != nil {
		return nil, githubError("FORBIDDEN")
	}
	env, e := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: a.ProjectID, Name: p.Environment})
	if errors.Is(e, pgx.ErrNoRows) || e == nil && env.Kind != "standard" {
		return nil, githubError("NOT_FOUND")
	}
	if e != nil {
		return nil, e
	}
	r, ok := s.Catalog.Find(a.ProjectID, p.Environment, p.ConfigID)
	if !ok {
		return nil, githubError("UNAVAILABLE")
	}
	old, e := q.LockCapabilityGithubConfig(ctx, store.LockCapabilityGithubConfigParams{ProjectID: a.ProjectID, Environment: p.Environment})
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return nil, e
	}
	if old.Revision != p.ExpectedConfigRevision {
		return nil, githubError("REVISION_CONFLICT")
	}
	id := old.ID
	if id == uuid.Nil {
		id = uuid.Must(uuid.NewV7())
	}
	c, e := q.SetCapabilityGithubConfig(ctx, store.SetCapabilityGithubConfigParams{ID: id, ProjectID: a.ProjectID, EnvironmentID: env.ID, Environment: p.Environment, ConfigID: r.ConfigID, Fingerprint: r.Fingerprint, CredentialID: r.CredentialID, Enabled: p.Enabled, ChangedBy: a.ID})
	if e != nil {
		return nil, e
	}
	if e = q.InvalidateCapabilityGithubLinks(ctx, store.InvalidateCapabilityGithubLinksParams{ProjectID: a.ProjectID, ConfigID: c.ID}); e != nil {
		return nil, e
	}
	if e = githubAudit(ctx, q, a, nil, "config_changed", c.Revision); e != nil {
		return nil, e
	}
	return githubConfigView(c, r, p.Enabled), nil
}
func (s *GithubService) githubAction(ctx context.Context, tx pgx.Tx, a auth.Actor, p GithubAction, name string) (object, error) {
	if e := reason(ctx); e != nil {
		return nil, e
	}
	q := store.New(tx)
	r, e := lockRequest(ctx, q, a, p.RequestID, p.ExpectedRequestRevision)
	if e != nil {
		return nil, e
	}
	live, e := liveDeliveryHuman(ctx, q, a.ProjectID, a.ID)
	if e != nil {
		return nil, e
	}
	if githubAuthor(live) != nil || !visible(live, r) {
		return nil, githubError("FORBIDDEN")
	}
	if r.Status == "draft" || terminal(r.Status) {
		return nil, githubError("STATE_INVALID")
	}
	c, e := q.LockCapabilityGithubConfig(ctx, store.LockCapabilityGithubConfigParams{ProjectID: a.ProjectID, Environment: r.Environment})
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, githubError("UNAVAILABLE")
	}
	if e != nil {
		return nil, e
	}
	l, e := q.LockCapabilityGithubLink(ctx, store.LockCapabilityGithubLinkParams{ProjectID: a.ProjectID, RequestID: r.ID})
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return nil, e
	}
	if l.Revision != p.ExpectedLinkRevision {
		return nil, githubError("REVISION_CONFLICT")
	}
	if name == "unlink-capability-github-pr" {
		if l.ID == uuid.Nil {
			return nil, githubError("NOT_FOUND")
		}
		l, e = q.UnlinkCapabilityGithub(ctx, store.UnlinkCapabilityGithubParams{ProjectID: a.ProjectID, RequestID: r.ID})
		if e == nil {
			e = githubAudit(ctx, q, a, &r.ID, "unlinked", l.Generation)
		}
		return githubLinkView(l), e
	}
	if c.Revision != p.ExpectedConfigRevision {
		return nil, githubError("CONFIG_CHANGED")
	}
	if _, ok := s.repository(c); !ok {
		return nil, githubError("UNAVAILABLE")
	}
	now := time.Now()
	if l.Active && l.PullNumber == p.PullNumber && name == "link-capability-github-pr" {
		return githubLinkView(l), nil
	}
	if name == "refresh-capability-github-pr" {
		if l.ID == uuid.Nil || !l.Active {
			return nil, githubError("NOT_FOUND")
		}
		if l.State == "sending" || l.State == "queued" && l.NextAt.Before(now) {
			return githubLinkView(l), nil
		}
		if now.Before(l.RefreshAfter) {
			return nil, githubError("COOLDOWN")
		}
		p.PullNumber = l.PullNumber
	} else if l.Active && !p.ConfirmReplace {
		return nil, githubError("CONFIRM_REPLACE_REQUIRED")
	}
	count, e := q.CapabilityGithubRefreshCount(ctx, a.ProjectID)
	if e != nil {
		return nil, e
	}
	if count >= 20 {
		return nil, githubError("RATE_LIMITED")
	}
	id := l.ID
	if id == uuid.Nil {
		id = uuid.Must(uuid.NewV7())
	}
	l, e = q.SetCapabilityGithubLink(ctx, store.SetCapabilityGithubLinkParams{ID: id, ProjectID: a.ProjectID, RequestID: r.ID, ConfigID: c.ID, ConfigRevision: c.Revision, Fingerprint: c.Fingerprint, PullNumber: p.PullNumber, InitiatedBy: a.ID, SyncID: uuid.Must(uuid.NewV7())})
	if e != nil {
		return nil, e
	}
	if e = githubAudit(ctx, q, a, &r.ID, "refresh", l.Generation); e != nil {
		return nil, e
	}
	if e = jobs.Enqueue(ctx, tx, GithubArgs{ProjectID: a.ProjectID, RequestID: r.ID}); e != nil {
		return nil, e
	}
	return githubLinkView(l), nil
}
func (s *GithubService) GithubConfig(ctx context.Context, q *store.Queries, a auth.Actor, environment string) (object, error) {
	if deliveryAdmin(a) != nil && human(a) != nil && githubAuthor(a) != nil {
		return nil, githubError("FORBIDDEN")
	}
	if e := deliveryEnvironment(ctx, q, a.ProjectID, environment); e != nil {
		return nil, e
	}
	choices := []any{}
	if deliveryAdmin(a) == nil {
		for _, r := range s.Catalog.All() {
			if r.ProjectID == a.ProjectID && r.Environment == environment {
				choices = append(choices, object{"configId": r.ConfigID, "owner": r.Owner, "name": r.Name, "baseBranch": r.BaseBranch, "requiredChecks": r.RequiredChecks})
			}
		}
	}
	c, e := q.GetCapabilityGithubConfig(ctx, store.GetCapabilityGithubConfigParams{ProjectID: a.ProjectID, Environment: environment})
	if errors.Is(e, pgx.ErrNoRows) {
		return object{"configured": false, "revision": int64(0), "ready": false, "choices": choices}, nil
	}
	if e != nil {
		return nil, e
	}
	r, ready := s.repository(c)
	v := githubConfigView(c, r, ready)
	v["choices"] = choices
	return v, nil
}
func githubObservationView(o store.CapabilityGithubObservation) object {
	return object{"id": o.ID, "generation": o.Generation, "configRevision": o.ConfigRevision, "observedAt": o.ObservedAt, "freshUntil": o.FreshUntil, "digest": o.Digest, "facts": json.RawMessage(o.Body)}
}
func (s *GithubService) GithubGet(ctx context.Context, q *store.Queries, a auth.Actor, id uuid.UUID) (object, error) {
	r, e := read(ctx, q, a, id)
	if e != nil {
		return nil, e
	}
	if r.Status == "draft" {
		return nil, notFound()
	}
	out := object{"link": nil, "current": nil, "lastKnown": nil, "syncState": "unlinked", "canManage": githubAuthor(a) == nil && !terminal(r.Status)}
	c, e := q.GetCapabilityGithubConfig(ctx, store.GetCapabilityGithubConfigParams{ProjectID: a.ProjectID, Environment: r.Environment})
	if errors.Is(e, pgx.ErrNoRows) {
		out["syncState"] = "disabled"
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	repo, ready := s.repository(c)
	out["configRevision"] = c.Revision
	out["repository"] = object{"owner": repo.Owner, "name": repo.Name, "baseBranch": repo.BaseBranch}
	if !ready {
		out["syncState"] = "disabled"
	}
	l, e := q.GetCapabilityGithubLink(ctx, store.GetCapabilityGithubLinkParams{ProjectID: a.ProjectID, RequestID: r.ID})
	if errors.Is(e, pgx.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	out["link"] = githubLinkView(l)
	if ready && l.Active {
		out["syncState"] = l.State
	}
	if repo.Owner != "" && l.ConfigID == c.ID && l.ConfigRevision == c.Revision && l.Fingerprint == c.Fingerprint {
		out["url"] = gh.CanonicalURL(repo, l.PullNumber)
	}
	o, e := q.LatestCapabilityGithubObservation(ctx, store.LatestCapabilityGithubObservationParams{ProjectID: a.ProjectID, RequestID: r.ID})
	if errors.Is(e, pgx.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	out["lastKnown"] = githubObservationView(o)
	initiator, liveErr := liveDeliveryHuman(ctx, q, a.ProjectID, l.InitiatedBy)
	if liveErr != nil && !errors.Is(liveErr, pgx.ErrNoRows) {
		return nil, liveErr
	}
	if liveErr != nil || githubAuthor(initiator) != nil || !visible(initiator, r) {
		ready = false
		out["syncState"] = "unavailable"
	}
	if ready && l.Active && o.Generation == l.Generation && o.ConfigRevision == c.Revision && o.Fingerprint == c.Fingerprint && l.LastError == "" && !terminal(r.Status) && time.Now().Before(o.FreshUntil) {
		out["current"] = githubObservationView(o)
	} else if ready && l.Active && l.LastError == "" {
		out["syncState"] = "stale"
	}
	return out, nil
}
func (s *GithubService) GithubHistory(ctx context.Context, q *store.Queries, a auth.Actor, id uuid.UUID, limit int, encoded string) (object, error) {
	r, e := read(ctx, q, a, id)
	if e != nil {
		return nil, e
	}
	if r.Status == "draft" {
		return nil, notFound()
	}
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 {
		return nil, githubError("STATE_INVALID")
	}
	p := store.ListCapabilityGithubObservationsParams{ProjectID: a.ProjectID, RequestID: id, PageLimit: int32(limit + 1)}
	scope := gh.Digest(object{"project": a.ProjectID, "request": id, "actor": a.ID})
	if encoded != "" {
		b, e := base64.RawURLEncoding.DecodeString(encoded)
		var c cursor
		if e != nil || len(b) > 1024 || json.Unmarshal(b, &c) != nil || c.Scope != scope || c.ID == uuid.Nil || c.Time.IsZero() {
			return nil, githubError("STATE_INVALID")
		}
		p.BeforeID = &c.ID
		p.BeforeTime = &c.Time
	}
	rows, e := q.ListCapabilityGithubObservations(ctx, p)
	if e != nil {
		return nil, e
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	items := []any{}
	for _, o := range rows {
		items = append(items, githubObservationView(o))
	}
	var next any
	if more {
		last := rows[len(rows)-1]
		next = base64.RawURLEncoding.EncodeToString(raw(cursor{Scope: scope, Time: last.ObservedAt, ID: last.ID}))
	}
	return object{"items": items, "nextCursor": next}, nil
}

func (s *GithubService) ValidateCatalog(ctx context.Context) error {
	q := store.New(s.Pool)
	for _, repository := range s.Catalog.All() {
		env, e := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: repository.ProjectID, Name: repository.Environment})
		if e != nil || env.Kind != "standard" {
			return gh.ErrConfig
		}
	}
	return nil
}
