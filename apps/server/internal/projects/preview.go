package projects

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/previeworigins"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// SourcePayload — источник preview-окружения в create-environment (STU-315).
type SourcePayload struct {
	BaseEnvironment string `json:"baseEnvironment"`
	PullNumber      int64  `json:"pullNumber"`
}

// SourceView — источник в ответах API. Repository берётся только из конфигурации.
type SourceView struct {
	BaseEnvironmentID uuid.UUID `json:"baseEnvironmentId"`
	RepositoryID      string    `json:"repositoryId"`
	PullNumber        int64     `json:"pullNumber"`
}

// Source — включённая GitHub-конфигурация standard-окружения.
type Source struct {
	BaseEnvironmentID uuid.UUID
	ConfigID          uuid.UUID
	RepositoryID      string
}

// SourceResolver находит конфигурацию base environment; ошибка — источник недоступен.
type SourceResolver func(ctx context.Context, q *store.Queries, project uuid.UUID, baseEnvironment string) (Source, error)

// Preview — зависимости preview-окружений с источником.
type Preview struct {
	Source  SourceResolver
	Origins previeworigins.List
}

// PreviewName — каноническое имя окружения PR (STU-315, решение §11.1).
func PreviewName(pull int64) string { return "preview/pr-" + strconv.FormatInt(pull, 10) }

// PreviewError — controlled refusal of the preview lifecycle.
func PreviewError(status int, code, detail string) error {
	return commandbus.NewError(status, code, "Preview-окружение недоступно", detail)
}

// DeleteEnvironmentPayload — payload delete-environment (STU-325).
type DeleteEnvironmentPayload struct {
	Environment             string `json:"environment"`
	ExpectedRegistrationSeq *int32 `json:"expectedRegistrationSeq,omitempty"`
}

func registerPreview(bus *commandbus.Bus, cfg Preview) {
	commandbus.Register(bus, commandbus.Command[DeleteEnvironmentPayload, Environment]{
		Name: "delete-environment",
		// MF-032: CI with manifest.register or a project administrator.
		Authorize: func(actor auth.Actor, _ DeleteEnvironmentPayload) error {
			if actor.Kind == auth.ActorService && actor.Rights.Has(auth.ManifestRegister) || actor.Kind == auth.ActorHuman && actor.Rights.Has(auth.ProjectAdmin) {
				return nil
			}
			return commandbus.NewError(http.StatusForbidden, "FORBIDDEN", "Нет права удалять окружение", "Нужен manifest.register (CI) или project.admin")
		},
		Validate: func(p DeleteEnvironmentPayload) error {
			if !environmentName.MatchString(p.Environment) || p.ExpectedRegistrationSeq != nil && *p.ExpectedRegistrationSeq < 0 {
				return commandbus.Validation(map[string]string{"environment": "имя окружения"})
			}
			return nil
		},
		Handle: deleteEnvironment,
	})
}

func (cfg Preview) create(ctx context.Context, tx pgx.Tx, actor auth.Actor, p CreateEnvironmentPayload) (Environment, error) {
	q := store.New(tx)
	if err := validation.LockProjectEnvironments(ctx, q, actor.ProjectID); err != nil {
		return Environment{}, err
	}
	if cfg.Source == nil {
		return Environment{}, PreviewError(http.StatusConflict, "PREVIEW_SOURCE_INVALID", "GitHub-конфигурация preview не подключена")
	}
	src, err := cfg.Source(ctx, q, actor.ProjectID, p.Source.BaseEnvironment)
	if err != nil {
		return Environment{}, err
	}
	var appURL *string
	if p.AppURL != nil {
		u, ok := cfg.Origins.AppURL(*p.AppURL)
		if !ok {
			return Environment{}, PreviewError(http.StatusUnprocessableEntity, "PREVIEW_APP_ORIGIN_NOT_ALLOWED", "appUrl вне списка CMS_PREVIEW_APP_ORIGINS")
		}
		appURL = &u
	}
	existing, err := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: actor.ProjectID, Name: p.Name})
	if err == nil {
		// CI may rerun its pipeline: the same source returns the same environment.
		if existing.SourceBaseEnvironmentID != nil && *existing.SourceBaseEnvironmentID == src.BaseEnvironmentID && *existing.SourceConfigID == src.ConfigID && *existing.SourceRepositoryID == src.RepositoryID && *existing.SourcePullNumber == p.Source.PullNumber {
			return toEnvironment(existing), nil
		}
		return Environment{}, commandbus.NewError(http.StatusConflict, "ENVIRONMENT_EXISTS", "Окружение уже существует", fmt.Sprintf("Окружение %q уже есть в проекте", p.Name))
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, err
	}
	env, err := q.CreatePreviewEnvironment(ctx, store.CreatePreviewEnvironmentParams{ID: uuid.Must(uuid.NewV7()), ProjectID: actor.ProjectID, Name: p.Name, AppUrl: appURL,
		SourceBaseEnvironmentID: &src.BaseEnvironmentID, SourceConfigID: &src.ConfigID, SourceRepositoryID: &src.RepositoryID, SourcePullNumber: &p.Source.PullNumber})
	if err != nil {
		return Environment{}, err
	}
	if err := Audit(ctx, q, env, &actor.ID, "created", commandbus.ReasonFrom(ctx), map[string]any{"pullNumber": p.Source.PullNumber}); err != nil {
		return Environment{}, err
	}
	return toEnvironment(env), nil
}

func deleteEnvironment(ctx context.Context, tx pgx.Tx, actor auth.Actor, p DeleteEnvironmentPayload) (Environment, error) {
	q := store.New(tx)
	if err := validation.LockProjectEnvironments(ctx, q, actor.ProjectID); err != nil {
		return Environment{}, err
	}
	env, err := q.LockManifestEnvironment(ctx, store.LockManifestEnvironmentParams{ProjectID: actor.ProjectID, Name: p.Environment})
	if errors.Is(err, pgx.ErrNoRows) {
		archived, err := q.LatestArchivedEnvironment(ctx, store.LatestArchivedEnvironmentParams{ProjectID: actor.ProjectID, Name: p.Environment})
		if err == nil {
			return toEnvironment(archived), nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return Environment{}, err
		}
		return Environment{}, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Окружение не найдено", p.Environment)
	}
	if err != nil {
		return Environment{}, err
	}
	if env.Kind != "preview" {
		return Environment{}, PreviewError(http.StatusConflict, "ENVIRONMENT_NOT_EPHEMERAL", "Удалять можно только preview-окружение")
	}
	if p.ExpectedRegistrationSeq != nil && *p.ExpectedRegistrationSeq != env.RegistrationSeq {
		return Environment{}, PreviewError(http.StatusConflict, "PREVIEW_REGISTRATION_CHANGED", "В окружение зарегистрирована новая сборка")
	}
	archived, err := Archive(ctx, q, env, "deleted", &actor.ID, commandbus.ReasonFrom(ctx))
	if err != nil {
		return Environment{}, err
	}
	return toEnvironment(archived), nil
}

// Archive снимает окружение с работы (STU-325/326): освобождает имя, снимает
// active manifest и отзывает preview-токены новым ключом. Вызывающий держит
// блокировку окружений проекта. Manifests, регистрации и аудит сохраняются.
func Archive(ctx context.Context, q *store.Queries, env store.Environment, reason string, actor *uuid.UUID, why string) (store.Environment, error) {
	if err := q.DeleteEnvironmentPresence(ctx, store.DeleteEnvironmentPresenceParams{ProjectID: env.ProjectID, Environment: env.Name}); err != nil {
		return env, err
	}
	archived, err := q.ArchiveEnvironment(ctx, store.ArchiveEnvironmentParams{ProjectID: env.ProjectID, ID: env.ID, Tombstone: "archived/" + hex.EncodeToString(env.ID[:]), Reason: reason})
	if err != nil {
		return env, err
	}
	return archived, Audit(ctx, q, archived, actor, "archived", why, map[string]any{"archiveReason": reason, "registrationSeq": archived.RegistrationSeq})
}

// Audit записывает событие жизненного цикла preview-окружения (STU-332).
func Audit(ctx context.Context, q *store.Queries, env store.Environment, actor *uuid.UUID, event, reason string, data map[string]any) error {
	body, _ := json.Marshal(data)
	return q.InsertPreviewEnvironmentAudit(ctx, store.InsertPreviewEnvironmentAuditParams{ID: uuid.Must(uuid.NewV7()), ProjectID: env.ProjectID, EnvironmentID: env.ID, ActorID: actor, Event: event, Reason: reason, Data: body})
}
