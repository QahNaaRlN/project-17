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
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/schemaflow"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

type Payload struct {
	Manifest          json.RawMessage `json:"manifest"`
	Environment       string          `json:"environment"`
	CodeIndexUploadID *string         `json:"codeIndexUploadId,omitempty"`
}
type Registration struct {
	ManifestHash string              `json:"manifestHash"`
	Activation   string              `json:"activation"`
	ChangesetID  *uuid.UUID          `json:"changesetId,omitempty"`
	Changes      []manifest.Change   `json:"changes"`
	Impact       []validation.Impact `json:"impact"`
}

func Register(bus *commandbus.Bus) {
	schemaflow.Register(bus)
	commandbus.Register(bus, commandbus.Command[Payload, Registration]{Name: "register-manifest", Authorize: authorize, Validate: validate, Handle: handle})
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
	if p.CodeIndexUploadID != nil {
		return commandbus.NewError(http.StatusConflict, "CODE_INDEX_UPLOAD_NOT_READY", "Загрузка code index ещё не подключена", "Регистрация с codeIndexUploadId пока недоступна")
	}
	app, err := manifest.ParseJSON(p.Manifest)
	if err != nil {
		return commandbus.Validation(map[string]string{"manifest": err.Error()})
	}
	r := manifest.Validate(app)
	if !r.Valid {
		return commandbus.NewError(http.StatusUnprocessableEntity, "MANIFEST_INVALID", "Некорректный manifest", "Контракт не прошёл проверку").WithParams(map[string]any{"diagnostics": r.Diagnostics})
	}
	if index, ok := app.(map[string]any)["codeIndex"].(map[string]any); ok && index["uploaded"] == true {
		return commandbus.NewError(http.StatusConflict, "CODE_INDEX_UPLOAD_NOT_READY", "Загрузка code index ещё не подключена", "Нельзя подтверждать uploaded без сохранённого файла")
	}
	return nil
}
func handle(ctx context.Context, tx pgx.Tx, actor auth.Actor, p Payload) (Registration, error) {
	q := store.New(tx)
	if err := validation.LockProjectEnvironments(ctx, q, actor.ProjectID); err != nil {
		return Registration{}, err
	}
	env, err := q.LockManifestEnvironment(ctx, store.LockManifestEnvironmentParams{ProjectID: actor.ProjectID, Name: p.Environment})
	if errors.Is(err, pgx.ErrNoRows) {
		return Registration{}, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Окружение не найдено", p.Environment)
	}
	if err != nil {
		return Registration{}, err
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
	if err := q.InsertManifest(ctx, store.InsertManifestParams{ID: uuid.Must(uuid.NewV7()), ProjectID: actor.ProjectID, Hash: hash, AppVersion: fmt.Sprint(app.(map[string]any)["app"].(map[string]any)["version"]), Body: body, RegisteredBy: actor.ID}); err != nil {
		return Registration{}, err
	}
	m, err := q.GetManifestByHash(ctx, store.GetManifestByHashParams{ProjectID: actor.ProjectID, Hash: hash})
	if err != nil {
		return Registration{}, err
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
	return result, nil
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
	ManifestHash string          `json:"manifestHash"`
	Manifest     json.RawMessage `json:"manifest"`
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
	return Active{m.Hash, m.Body}, nil
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
