package reviewdiff

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const MaxBytes = 4 << 20

var snapshotPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func problem(status int, code string) error {
	return commandbus.NewError(status, code, "Сравнение недоступно", "")
}

type Object struct {
	ID                 uuid.UUID          `json:"id"`
	Kind               string             `json:"kind"`
	DocKind            *string            `json:"docKind,omitempty"`
	Title              string             `json:"title"`
	BeforePath         *string            `json:"beforePath"`
	AfterPath          *string            `json:"afterPath"`
	BaseVersionID      *uuid.UUID         `json:"baseVersionId"`
	WorkingVersionID   uuid.UUID          `json:"workingVersionId"`
	HeadVersionID      *uuid.UUID         `json:"headVersionId"`
	PublishedVersionID *uuid.UUID         `json:"publishedVersionId"`
	BaseStale          bool               `json:"baseStale"`
	Summaries          map[string]Summary `json:"summaries"`
}
type Index struct {
	ChangesetID uuid.UUID `json:"changesetId"`
	Seq         int32     `json:"seq"`
	Snapshot    string    `json:"snapshot"`
	Environment string    `json:"environment"`
	Targets     []string  `json:"targets"`
	Complete    bool      `json:"complete"`
	Objects     []Object  `json:"objects"`
}
type Page struct {
	ChangesetID  uuid.UUID `json:"changesetId"`
	ObjectID     uuid.UUID `json:"objectId"`
	Seq          int32     `json:"seq"`
	Snapshot     string    `json:"snapshot"`
	Environment  string    `json:"environment"`
	Baseline     string    `json:"baseline"`
	TotalChanges int       `json:"totalChanges"`
	Items        []Change  `json:"items"`
	NextCursor   *string   `json:"nextCursor"`
}
type loaded struct {
	index   Index
	details []Change
	key     []byte
}

func same(a, b *uuid.UUID) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func image(ctx context.Context, q *store.Queries, project, obj uuid.UUID, id *uuid.UUID, schema *string, working *uuid.UUID, kind string) (*Image, error) {
	if id == nil {
		return nil, nil
	}
	v, err := q.GetDiffVersion(ctx, store.GetDiffVersionParams{ProjectID: project, ObjectID: obj, ID: *id})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, problem(409, "DIFF_BASE_UNAVAILABLE")
	}
	if err != nil {
		return nil, err
	}
	if working == nil && v.State != "committed" || working != nil && (v.State != "working" || v.ChangesetID == nil || *v.ChangesetID != *working) {
		return nil, problem(409, "DIFF_BASE_UNAVAILABLE")
	}
	body, err := decode(v.Body)
	if err != nil || body == nil {
		return nil, problem(422, "DIFF_PROJECTION_UNSUPPORTED")
	}
	if kind == "document" {
		root, ok := body["root"].(string)
		nodes, nodesOK := body["nodes"].(map[string]any)
		if !ok || !nodesOK || nodes[root] == nil {
			return nil, problem(422, "DIFF_PROJECTION_UNSUPPORTED")
		}
		for _, node := range nodes {
			if _, ok := node.(map[string]any); !ok {
				return nil, problem(422, "DIFF_PROJECTION_UNSUPPORTED")
			}
		}
	}
	return &Image{Body: body, Deleted: v.Deleted, Meta: map[string]any{"path": v.Path, "deleted": v.Deleted, "schema": schema, "schemaVersion": v.SchemaVersion, "certified": v.Certified}}, nil
}
func title(kind string, img *Image, path *string, id uuid.UUID) string {
	if img != nil {
		if kind == "document" {
			root, _ := img.Body["root"].(string)
			nodes, _ := img.Body["nodes"].(map[string]any)
			node, _ := nodes[root].(map[string]any)
			if name, ok := node["name"].(string); ok && name != "" {
				return name
			}
		}
		for _, key := range []string{"title", "name"} {
			if name, ok := img.Body[key].(string); ok && name != "" {
				return name
			}
			if localized, ok := img.Body[key].(map[string]any); ok {
				for _, locale := range append([]string{"ru", "en"}, sortedKeys(localized)...) {
					if name, ok := localized[locale].(string); ok && name != "" {
						return name
					}
				}
			}
		}
	}
	if path != nil {
		return *path
	}
	return kind + " · " + id.String()[28:]
}
func load(ctx context.Context, q *store.Queries, actor auth.Actor, id uuid.UUID, environment string, wanted *uuid.UUID, baseline string) (loaded, error) {
	out := loaded{}
	cs, err := q.GetChangeset(ctx, store.GetChangesetParams{ID: id, ProjectID: actor.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return out, problem(404, "NOT_FOUND")
	}
	if err != nil {
		return out, err
	}
	if !auth.AgentAllowsCS(actor, id) || cs.OwnerID != actor.ID && (!actor.Rights.Has(auth.ContentPublish) || !slices.Contains([]string{"in_review", "approved"}, cs.State)) {
		return out, problem(404, "NOT_FOUND")
	}
	if cs.Kind != "standard" {
		return out, problem(422, "DIFF_KIND_UNSUPPORTED")
	}
	if !slices.Contains([]string{"open", "failed", "changes_requested", "in_review", "approved"}, cs.State) {
		return out, problem(409, "DIFF_STATE_UNSUPPORTED")
	}
	targets := append([]string{}, cs.Targets...)
	slices.Sort(targets)
	if environment == "" && len(targets) > 0 {
		environment = targets[0]
	}
	if !slices.Contains(targets, environment) {
		return out, problem(403, "DIFF_ENVIRONMENT_FORBIDDEN")
	}
	objects, err := q.ListDiffObjects(ctx, store.ListDiffObjectsParams{ProjectID: actor.ProjectID, ChangesetID: id})
	if err != nil {
		return out, err
	}
	if len(objects) > 1000 {
		return out, problem(422, "DIFF_LIMIT_EXCEEDED")
	}
	for _, o := range objects {
		right := auth.ContentRead
		if o.Kind == "document" {
			right = auth.DesignRead
		} else if o.Kind != "entity" && o.Kind != "asset" {
			return out, problem(422, "DIFF_PROJECTION_UNSUPPORTED")
		}
		if err := commandbus.Require(actor, right); err != nil {
			return out, err
		}
	}
	if wanted != nil && !slices.ContainsFunc(objects, func(o store.ListDiffObjectsRow) bool { return o.ID == *wanted }) {
		return out, problem(404, "NOT_FOUND")
	}
	environments, err := q.ListEnvironments(ctx, actor.ProjectID)
	if err != nil {
		return out, err
	}
	envParts := map[string]any{}
	for _, env := range environments {
		if slices.Contains(targets, env.Name) {
			envParts[env.Name] = env.ActiveManifestID
			if env.Name == environment {
				out.key = env.PreviewKey
			}
		}
	}
	if len(envParts) != len(targets) {
		return out, problem(403, "DIFF_ENVIRONMENT_FORBIDDEN")
	}
	settings, err := q.GetProjectSettings(ctx, actor.ProjectID)
	if err != nil {
		return out, err
	}
	policy, err := decode(settings)
	if err != nil {
		return out, problem(422, "DIFF_PROJECTION_UNSUPPORTED")
	}
	pointers, err := q.ListDiffPointers(ctx, store.ListDiffPointersParams{ProjectID: actor.ProjectID, ChangesetID: id})
	if err != nil {
		return out, err
	}
	pub := map[string]uuid.UUID{}
	pointerParts := map[string]any{}
	for _, p := range pointers {
		if slices.Contains(targets, p.Environment) {
			key := p.Environment + ":" + p.ObjectID.String()
			pub[key] = p.VersionID
			pointerParts[key] = p.VersionID
		}
	}
	parts := []any{}
	out.index = Index{ChangesetID: id, Seq: cs.Seq, Environment: environment, Targets: targets, Complete: true, Objects: []Object{}}
	for _, o := range objects {
		work, err := image(ctx, q, actor.ProjectID, o.ID, &o.WorkingVersionID, o.SchemaName, &id, o.Kind)
		if err != nil {
			return out, err
		}
		base, err := image(ctx, q, actor.ProjectID, o.ID, o.BaseVersionID, o.SchemaName, nil, o.Kind)
		if err != nil {
			return out, err
		}
		var pubID *uuid.UUID
		if value, ok := pub[environment+":"+o.ID.String()]; ok {
			pubID = &value
		}
		published, err := image(ctx, q, actor.ProjectID, o.ID, pubID, o.SchemaName, nil, o.Kind)
		if err != nil {
			return out, err
		}
		bc, bs := Diff(o.Kind, base, work)
		pc, ps := Diff(o.Kind, published, work)
		var beforePath *string
		if base != nil {
			beforePath, _ = base.Meta["path"].(*string)
		}
		afterPath, _ := work.Meta["path"].(*string)
		chosen := work
		if work.Deleted {
			if base != nil {
				chosen = base
			} else if published != nil {
				chosen = published
			}
		}
		out.index.Objects = append(out.index.Objects, Object{ID: o.ID, Kind: o.Kind, DocKind: o.DocKind, Title: title(o.Kind, chosen, afterPath, o.ID), BeforePath: beforePath, AfterPath: afterPath, BaseVersionID: o.BaseVersionID, WorkingVersionID: o.WorkingVersionID, HeadVersionID: o.HeadVersionID, PublishedVersionID: pubID, BaseStale: !same(o.BaseVersionID, o.HeadVersionID), Summaries: map[string]Summary{"base": bs, "published": ps}})
		parts = append(parts, map[string]any{"id": o.ID, "kind": o.Kind, "base": o.BaseVersionID, "working": o.WorkingVersionID, "body": hash(work.Body), "meta": work.Meta, "head": o.HeadVersionID})
		if wanted != nil && o.ID == *wanted {
			out.details = bc
			if baseline == "published" {
				out.details = pc
			}
		}
	}
	out.index.Snapshot = hash(map[string]any{"project": actor.ProjectID, "cs": id, "seq": cs.Seq, "targets": targets, "objects": parts, "environments": envParts, "pointers": pointerParts, "policy": policy})
	raw, _ := json.Marshal(out.index)
	if len(raw) > MaxBytes {
		return loaded{}, problem(422, "DIFF_LIMIT_EXCEEDED")
	}
	return out, nil
}
func ReadIndex(ctx context.Context, q *store.Queries, actor auth.Actor, id uuid.UUID, environment string) (Index, error) {
	v, err := load(ctx, q, actor, id, environment, nil, "")
	return v.index, err
}
func CheckExpected(ctx context.Context, q *store.Queries, actor auth.Actor, id uuid.UUID, snapshot *string) error {
	if snapshot == nil {
		return nil
	}
	if !snapshotPattern.MatchString(*snapshot) {
		return problem(400, "PAYLOAD_INVALID")
	}
	v, err := load(ctx, q, actor, id, "", nil, "")
	if err != nil {
		return err
	}
	if v.index.Snapshot != *snapshot {
		return problem(409, "DIFF_SNAPSHOT_STALE")
	}
	return nil
}
func cursor(context string, n int, key []byte) string {
	p := base64.RawURLEncoding.EncodeToString([]byte(context + ":" + strconv.Itoa(n)))
	m := hmac.New(sha256.New, key)
	m.Write([]byte(p))
	return p + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func offset(raw, context string, key []byte) (int, error) {
	if raw == "" {
		return 0, nil
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return 0, problem(400, "DIFF_CURSOR_INVALID")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	m := hmac.New(sha256.New, key)
	m.Write([]byte(parts[0]))
	if err != nil || !hmac.Equal(sig, m.Sum(nil)) {
		return 0, problem(400, "DIFF_CURSOR_INVALID")
	}
	p, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || !strings.HasPrefix(string(p), context+":") {
		return 0, problem(400, "DIFF_CURSOR_INVALID")
	}
	n, err := strconv.Atoi(strings.TrimPrefix(string(p), context+":"))
	if err != nil || n < 0 {
		return 0, problem(400, "DIFF_CURSOR_INVALID")
	}
	return n, nil
}
func ReadPage(ctx context.Context, q *store.Queries, actor auth.Actor, id, obj uuid.UUID, environment, baseline, snapshot, after string, limit int) (Page, error) {
	out := Page{}
	if !slices.Contains([]string{"base", "published"}, baseline) || !snapshotPattern.MatchString(snapshot) || limit < 1 || limit > 100 {
		return out, problem(400, "PAYLOAD_INVALID")
	}
	v, err := load(ctx, q, actor, id, environment, &obj, baseline)
	if err != nil {
		return out, err
	}
	if v.index.Snapshot != snapshot {
		return out, problem(409, "DIFF_SNAPSHOT_STALE")
	}
	context := actor.ProjectID.String() + ":" + id.String() + ":" + obj.String() + ":" + environment + ":" + baseline + ":" + snapshot
	n, err := offset(after, context, v.key)
	if err != nil {
		return out, err
	}
	if n > len(v.details) {
		return out, problem(400, "DIFF_CURSOR_INVALID")
	}
	out = Page{ChangesetID: id, ObjectID: obj, Seq: v.index.Seq, Snapshot: snapshot, Environment: environment, Baseline: baseline, TotalChanges: len(v.details), Items: []Change{}}
	for end := n; end < len(v.details) && end < n+limit; end++ {
		candidate := append(out.Items, v.details[end])
		raw, _ := json.Marshal(candidate)
		if len(raw) > MaxBytes-1024 {
			if len(out.Items) == 0 {
				return Page{}, problem(422, "DIFF_LIMIT_EXCEEDED")
			}
			break
		}
		out.Items = candidate
	}
	if end := n + len(out.Items); end < len(v.details) {
		next := cursor(context, end, v.key)
		out.NextCursor = &next
	}
	return out, nil
}
