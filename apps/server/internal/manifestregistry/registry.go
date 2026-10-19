// Package manifestregistry registers immutable application contracts (MF-020–MF-033).
package manifestregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/codeindex"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/previeworigins"
	"github.com/qahnaarln/project-17/apps/server/internal/schemaflow"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

type Payload struct {
	Manifest          json.RawMessage `json:"manifest"`
	Environment       string          `json:"environment"`
	CodeIndexUploadID *string         `json:"codeIndexUploadId,omitempty"`
	// HeadSHA and AppURL describe a build of a preview environment with a
	// source (STU-316): the commit must equal the code index commit.
	HeadSHA *string `json:"headSha,omitempty"`
	AppURL  *string `json:"appUrl,omitempty"`
}
type Registration struct {
	ManifestHash        string               `json:"manifestHash"`
	Activation          string               `json:"activation"`
	ChangesetID         *uuid.UUID           `json:"changesetId,omitempty"`
	Changes             []manifest.Change    `json:"changes"`
	Impact              []validation.Impact  `json:"impact"`
	PreviewRegistration *PreviewRegistration `json:"previewRegistration,omitempty"`
}

// PreviewRegistration — запись журнала регистраций preview (STU-316/317).
type PreviewRegistration struct {
	Seq    int32   `json:"seq"`
	Commit string  `json:"commit"`
	AppURL *string `json:"appUrl"`
}

func Register(bus *commandbus.Bus) { RegisterWith(bus, previeworigins.List{}) }

// RegisterWith регистрирует команды; allowed — список CMS_PREVIEW_APP_ORIGINS (STU-328).
func RegisterWith(bus *commandbus.Bus, allowed previeworigins.List) {
	schemaflow.Register(bus)
	registerIndex(bus)
	commandbus.Register(bus, commandbus.Command[Payload, Registration]{Name: "register-manifest", Authorize: authorize, Validate: validate, Handle: func(ctx context.Context, tx pgx.Tx, actor auth.Actor, p Payload) (Registration, error) {
		return handle(ctx, tx, actor, p, allowed)
	}})
}
func authorize(actor auth.Actor, _ Payload) error {
	if actor.Kind != auth.ActorService {
		return commandbus.NewError(http.StatusForbidden, "FORBIDDEN", "Нужен сервисный актор", "MF-004: manifest регистрирует CI/CD")
	}
	return commandbus.Require(actor, auth.ManifestRegister)
}
func validate(p Payload) error {
	if p.Environment == "" {
		return commandbus.Validation(map[string]string{"environment": "укажите окружение"})
	}
	if p.HeadSHA != nil && !codeindex.CommitPattern.MatchString(*p.HeadSHA) {
		return previewCommitMismatch()
	}
	app, err := manifest.ParseJSON(p.Manifest)
	if err != nil {
		return commandbus.Validation(map[string]string{"manifest": err.Error()})
	}
	r := manifest.Validate(app)
	if !r.Valid {
		return commandbus.NewError(http.StatusUnprocessableEntity, "MANIFEST_INVALID", "Некорректный manifest", "Контракт не прошёл проверку").WithParams(map[string]any{"diagnostics": r.Diagnostics})
	}
	if index, ok := app.(map[string]any)["codeIndex"].(map[string]any); ok && index["uploaded"] == true && p.CodeIndexUploadID == nil {
		return commandbus.NewError(http.StatusConflict, "CODE_INDEX_UPLOAD_NOT_READY", "Загрузка code index ещё не подключена", "Нельзя подтверждать uploaded без сохранённого файла")
	}
	return nil
}
func handle(ctx context.Context, tx pgx.Tx, actor auth.Actor, p Payload, origins previeworigins.List) (Registration, error) {
	q := store.New(tx)
	if err := validation.LockProjectEnvironments(ctx, q, actor.ProjectID); err != nil {
		return Registration{}, err
	}
	env, err := q.LockManifestEnvironment(ctx, store.LockManifestEnvironmentParams{ProjectID: actor.ProjectID, Name: p.Environment})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, archived := q.LatestArchivedEnvironment(ctx, store.LatestArchivedEnvironmentParams{ProjectID: actor.ProjectID, Name: p.Environment}); archived == nil {
			return Registration{}, commandbus.NewError(http.StatusConflict, "ENVIRONMENT_ARCHIVED", "Окружение архивировано", "Создайте окружение заново командой create-environment")
		}
		return Registration{}, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Окружение не найдено", p.Environment)
	}
	if err != nil {
		return Registration{}, err
	}
	sourced := env.SourcePullNumber != nil
	var appURL *string
	if !sourced && (p.HeadSHA != nil || p.AppURL != nil) {
		return Registration{}, commandbus.Validation(map[string]string{"headSha": "только для preview-окружения с источником"})
	}
	if sourced && (p.HeadSHA == nil || p.CodeIndexUploadID == nil) {
		return Registration{}, previewCommitMismatch()
	}
	if sourced && p.AppURL != nil {
		u, ok := origins.AppURL(*p.AppURL)
		if !ok {
			return Registration{}, commandbus.NewError(http.StatusUnprocessableEntity, "PREVIEW_APP_ORIGIN_NOT_ALLOWED", "Preview-окружение недоступно", "appUrl вне списка CMS_PREVIEW_APP_ORIGINS")
		}
		appURL = &u
	}
	app, err := manifest.ParseJSON(p.Manifest)
	if err != nil {
		return Registration{}, err
	}
	var before any
	if env.ActiveManifestID != nil {
		m, err := q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: actor.ProjectID, ID: *env.ActiveManifestID})
		if err != nil {
			return Registration{}, err
		}
		before, err = manifest.ParseJSON(m.Body)
		if err != nil {
			return Registration{}, err
		}
	}
	changes := manifest.Diff(before, app)
	impact, err := validation.AnalyzeImpact(ctx, q, actor.ProjectID, p.Environment, app)
	if err != nil {
		return Registration{}, err
	}
	for _, item := range impact {
		if item.Stage != "published" {
			continue
		}
		pendingSchemas := env.Kind == "standard" && manifest.SchemasChanged(changes)
		blocked := false
		for _, change := range manifest.BreakingChanges(item.Changes) {
			blocked = blocked || !pendingSchemas || !change.Schema
		}
		// A schema candidate is not active; CNT-022 repairs its affected pages in the same CS.
		// Breaking application contracts still require the two-phase MF-022 removal path.
		if !pendingSchemas {
			for _, d := range item.Diagnostics {
				blocked = blocked || d.Severity == ir.SeverityError
			}
		}
		if blocked {
			return Registration{}, commandbus.NewError(http.StatusConflict, "MANIFEST_BREAKING_IN_USE", "Manifest нарушает опубликованный контракт", "Сначала измените использующие контракт документы").WithParams(map[string]any{"changes": changes, "impact": impact})
		}
	}
	schemas := map[string]any{}
	if s, ok := app.(map[string]any)["schemas"].(map[string]any); ok {
		schemas = s
	}
	if env.Kind == "standard" {
		if err := schemaflow.CheckVersions(ctx, q, actor.ProjectID, app); err != nil {
			return Registration{}, err
		}
	}
	body, err := manifest.CanonicalJSON(app)
	if err != nil {
		return Registration{}, err
	}
	hash, err := manifest.Hash(app)
	if err != nil {
		return Registration{}, err
	}
	indexKey, err := registrationIndex(ctx, q, actor, p, hash, app.(map[string]any))
	if err != nil {
		return Registration{}, err
	}
	if err := q.InsertManifest(ctx, store.InsertManifestParams{ID: uuid.Must(uuid.NewV7()), ProjectID: actor.ProjectID, Hash: hash, AppVersion: fmt.Sprint(app.(map[string]any)["app"].(map[string]any)["version"]), Body: body, CodeIndexKey: indexKey, RegisteredBy: actor.ID}); err != nil {
		return Registration{}, err
	}
	m, err := q.GetManifestByHash(ctx, store.GetManifestByHashParams{ProjectID: actor.ProjectID, Hash: hash})
	if err != nil {
		return Registration{}, err
	}
	if indexKey != nil {
		if m.CodeIndexKey == nil || *m.CodeIndexKey != *indexKey {
			return Registration{}, indexError("CODE_INDEX_IMMUTABLE")
		}
		if err := q.LinkCodeIndexUpload(ctx, store.LinkCodeIndexUploadParams{ProjectID: actor.ProjectID, ID: uuid.MustParse(*indexKey)}); err != nil {
			return Registration{}, err
		}
	}
	if sourced {
		upload, err := q.GetCodeIndexUpload(ctx, store.GetCodeIndexUploadParams{ProjectID: actor.ProjectID, ID: uuid.MustParse(*indexKey)})
		if err != nil {
			return Registration{}, err
		}
		if upload.RepoCommit != *p.HeadSHA {
			return Registration{}, previewCommitMismatch()
		}
	}
	result := Registration{ManifestHash: hash, Activation: "active", Changes: changes, Impact: impact}
	if env.Kind == "standard" && manifest.SchemasChanged(changes) {
		id, err := q.PendingManifestCandidate(ctx, store.PendingManifestCandidateParams{ProjectID: actor.ProjectID, EnvironmentID: env.ID, ManifestID: m.ID, BaseManifestID: env.ActiveManifestID})
		if errors.Is(err, pgx.ErrNoRows) {
			cs, e := q.CreateManifestCandidate(ctx, store.CreateManifestCandidateParams{ID: uuid.Must(uuid.NewV7()), ProjectID: actor.ProjectID, Title: "Manifest " + hash, OwnerID: actor.ID, Targets: []string{env.Name}})
			if e != nil {
				return Registration{}, e
			}
			id = cs.ID
			err = q.BindManifestCandidate(ctx, store.BindManifestCandidateParams{ChangesetID: id, ProjectID: actor.ProjectID, EnvironmentID: env.ID, ManifestID: m.ID, BaseManifestID: env.ActiveManifestID})
			if err == nil {
				err = schemaflow.Prepare(ctx, q, actor, cs)
			}
		}
		if err != nil {
			return Registration{}, err
		}
		result.Activation = "pending"
		result.ChangesetID = &id
		return result, nil
	}
	if env.Kind == "preview" {
		for _, name := range keys(schemas) {
			version, raw, err := schemaflow.Schema(schemas[name])
			if err != nil {
				return Registration{}, err
			}
			if err := q.InsertPreviewSchemaSnapshot(ctx, store.InsertPreviewSchemaSnapshotParams{ManifestID: m.ID, SchemaName: name, Version: version, Body: raw}); err != nil {
				return Registration{}, err
			}
		}
	}
	if _, err := q.SetActiveManifest(ctx, store.SetActiveManifestParams{ProjectID: actor.ProjectID, ID: env.ID, ActiveManifestID: &m.ID}); err != nil {
		return Registration{}, err
	}
	if err := Refresh(ctx, q, actor.ProjectID, env.Name); err != nil {
		return Registration{}, err
	}
	if sourced {
		reg, err := recordPreview(ctx, q, actor, env, m.ID, *p.HeadSHA, appURL)
		if err != nil {
			return Registration{}, err
		}
		result.PreviewRegistration = &reg
	}
	return result, nil
}

func previewCommitMismatch() error {
	return commandbus.NewError(http.StatusConflict, "PREVIEW_COMMIT_MISMATCH", "Preview-окружение недоступно", "headSha должен совпадать с полным commit загруженного code index")
}

// recordPreview appends the registration journal (STU-316/317). Repeating the
// current manifest/commit/appUrl keeps the same sequence number.
func recordPreview(ctx context.Context, q *store.Queries, actor auth.Actor, env store.Environment, manifestID uuid.UUID, commit string, appURL *string) (PreviewRegistration, error) {
	current, err := q.CurrentPreviewRegistration(ctx, store.CurrentPreviewRegistrationParams{ProjectID: actor.ProjectID, EnvironmentID: env.ID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return PreviewRegistration{}, err
	}
	if err == nil && current.ManifestID == manifestID && current.Commit == commit && (current.AppUrl == nil) == (appURL == nil) && (appURL == nil || *current.AppUrl == *appURL) {
		return PreviewRegistration{Seq: current.Seq, Commit: current.Commit, AppURL: current.AppUrl}, nil
	}
	env, err = q.RecordPreviewRegistration(ctx, store.RecordPreviewRegistrationParams{ProjectID: actor.ProjectID, ID: env.ID})
	if err != nil {
		return PreviewRegistration{}, err
	}
	reg, err := q.InsertPreviewRegistration(ctx, store.InsertPreviewRegistrationParams{ID: uuid.Must(uuid.NewV7()), ProjectID: actor.ProjectID, EnvironmentID: env.ID, Seq: env.RegistrationSeq, ManifestID: manifestID, Commit: commit, AppUrl: appURL, RegisteredBy: actor.ID})
	if err != nil {
		return PreviewRegistration{}, err
	}
	data, _ := json.Marshal(map[string]any{"seq": reg.Seq, "commit": reg.Commit})
	if err := q.InsertPreviewEnvironmentAudit(ctx, store.InsertPreviewEnvironmentAuditParams{ID: uuid.Must(uuid.NewV7()), ProjectID: actor.ProjectID, EnvironmentID: env.ID, ActorID: &actor.ID, Event: "registered", Reason: commandbus.ReasonFrom(ctx), Data: data}); err != nil {
		return PreviewRegistration{}, err
	}
	return PreviewRegistration{Seq: reg.Seq, Commit: reg.Commit, AppURL: reg.AppUrl}, nil
}
func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Refresh replaces diagnostics only for unfinished ordinary Change Sets of this environment.
func Refresh(ctx context.Context, q *store.Queries, project uuid.UUID, environment string) error {
	rows, err := q.ManifestChangesets(ctx, store.ManifestChangesetsParams{ProjectID: project, Environment: environment})
	if err != nil {
		return err
	}
	for _, cs := range rows {
		// Schema candidates retain their own contract; freshness tracks changes to their base.
		if cs.Kind == "schema" {
			continue
		}
		c, err := validation.Load(ctx, q, project, environment, &cs.ID)
		if err != nil {
			return err
		}
		versions, err := q.ChangesetWorkingVersions(ctx, cs.ID)
		if err != nil {
			return err
		}
		docs := make([]validation.Document, 0, len(versions))
		for _, v := range versions {
			docs = append(docs, validation.Document{ObjectID: v.ObjectID, VersionID: v.VersionID, Body: v.Body, Path: v.Path, Certified: v.Certified})
		}
		problems, err := c.CheckPublication(ctx, docs)
		if err != nil {
			return err
		}
		if err := c.RecordDiagnostics(ctx, cs, problems); err != nil {
			return err
		}
	}
	return nil
}

type Active struct {
	ManifestHash       string          `json:"manifestHash"`
	Manifest           json.RawMessage `json:"manifest"`
	CodeIndexAvailable bool            `json:"codeIndexAvailable"`
}

func GetActive(ctx context.Context, q *store.Queries, project uuid.UUID, environment string) (Active, error) {
	if environment == "" {
		return Active{}, commandbus.Validation(map[string]string{"environment": "укажите окружение"})
	}
	env, err := q.GetManifestEnvironment(ctx, store.GetManifestEnvironmentParams{ProjectID: project, Name: environment})
	if errors.Is(err, pgx.ErrNoRows) {
		return Active{}, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Окружение не найдено", environment)
	}
	if err != nil {
		return Active{}, err
	}
	if env.ActiveManifestID == nil {
		return Active{}, commandbus.NewError(http.StatusConflict, "MANIFEST_NOT_READY", "Manifest не активирован", "Сначала зарегистрируйте контракт окружения")
	}
	m, err := q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: project, ID: *env.ActiveManifestID})
	if err != nil {
		return Active{}, err
	}
	return Active{ManifestHash: m.Hash, Manifest: m.Body, CodeIndexAvailable: m.CodeIndexKey != nil}, nil
}
func GetSchemas(ctx context.Context, q *store.Queries, project uuid.UUID, environment string) (map[string]any, error) {
	active, err := GetActive(ctx, q, project, environment)
	if err != nil {
		return nil, err
	}
	var app map[string]json.RawMessage
	if err := json.Unmarshal(active.Manifest, &app); err != nil {
		return nil, err
	}
	schemas := app["schemas"]
	if schemas == nil {
		schemas = json.RawMessage(`{}`)
	}
	return map[string]any{"manifestHash": active.ManifestHash, "schemas": schemas}, nil
}
