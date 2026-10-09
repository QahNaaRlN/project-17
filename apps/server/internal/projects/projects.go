// Package projects — проекты и окружения (07-storage.md §2), первичная настройка проекта.
package projects

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/platform/postgres"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Environment — окружение в ответах API.
type Environment struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	AppURL    *string   `json:"appUrl"`
	CreatedAt time.Time `json:"createdAt"`
}

func toEnvironment(e store.Environment) Environment {
	return Environment{ID: e.ID, Name: e.Name, Kind: e.Kind, AppURL: e.AppUrl, CreatedAt: e.CreatedAt}
}

// CreateEnvironmentPayload — payload команды create-environment (08-api.md §3.2).
type CreateEnvironmentPayload struct {
	Name   string  `json:"name"`
	Kind   string  `json:"kind"`
	AppURL *string `json:"appUrl"`
}

var environmentName = regexp.MustCompile(`^[a-z0-9][a-z0-9/_-]{0,62}$`)

// Register регистрирует команды модуля в шине.
func Register(bus *commandbus.Bus) {
	commandbus.Register(bus, commandbus.Command[CreateEnvironmentPayload, Environment]{
		Name: "create-environment",
		// Стандартное окружение создаёт администратор проекта; preview-окружение — CI с правом
		// регистрации manifest (08-api.md §3.2, MF-030).
		Authorize: func(actor auth.Actor, p CreateEnvironmentPayload) error {
			if p.Kind == "preview" && actor.Rights.Has(auth.ManifestRegister) {
				return nil
			}
			return commandbus.Require(actor, auth.ProjectAdmin)
		},
		Validate: validateEnvironment,
		Handle: func(ctx context.Context, tx pgx.Tx, actor auth.Actor, p CreateEnvironmentPayload) (Environment, error) {
			env, err := store.New(tx).CreateEnvironment(ctx, store.CreateEnvironmentParams{
				ID: uuid.Must(uuid.NewV7()), ProjectID: actor.ProjectID, Name: p.Name, Kind: p.Kind, AppUrl: p.AppURL,
			})
			if postgres.IsUniqueViolation(err) {
				return Environment{}, commandbus.NewError(http.StatusConflict, "ENVIRONMENT_EXISTS",
					"Окружение уже существует", fmt.Sprintf("Окружение %q уже есть в проекте", p.Name))
			}
			if err != nil {
				return Environment{}, err
			}
			return toEnvironment(env), nil
		},
	})
}

func validateEnvironment(p CreateEnvironmentPayload) error {
	fields := map[string]string{}
	if !environmentName.MatchString(p.Name) {
		fields["name"] = "1…63 символа: строчные латинские буквы, цифры, '-', '_', '/'; начинается с буквы или цифры"
	}
	if p.Kind != "standard" && p.Kind != "preview" {
		fields["kind"] = "допустимо: standard, preview"
	}
	if p.AppURL != nil {
		u, err := url.Parse(*p.AppURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			fields["appUrl"] = "абсолютный URL http(s)"
		}
	}
	if len(fields) > 0 {
		return commandbus.Validation(fields)
	}
	return nil
}

// ListEnvironments — окружения проекта по имени.
func ListEnvironments(ctx context.Context, q *store.Queries, projectID uuid.UUID) ([]Environment, error) {
	rows, err := q.ListEnvironments(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]Environment, len(rows))
	for i, r := range rows {
		out[i] = toEnvironment(r)
	}
	return out, nil
}

// BootstrapResult — итог первичной настройки проекта.
type BootstrapResult struct {
	ProjectID uuid.UUID
	ActorID   uuid.UUID
	Token     string // секрет показывается один раз (SEC-002)
	ExpiresAt time.Time
}

// Bootstrap создаёт проект, окружения staging и production, роль admin со всеми правами
// и сервисного актора с токеном. Это единственная операция вне Command Bus: до неё в проекте
// нет ни одного актора, от имени которого можно выполнить команду.
func Bootstrap(ctx context.Context, pool *pgxpool.Pool, slug, name string, tokenTTL time.Duration) (BootstrapResult, error) {
	var res BootstrapResult
	err := postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		project, err := q.CreateProject(ctx, store.CreateProjectParams{ID: uuid.Must(uuid.NewV7()), Slug: slug, Name: name})
		if postgres.IsUniqueViolation(err) {
			return fmt.Errorf("проект %q уже существует", slug)
		}
		if err != nil {
			return err
		}
		for _, env := range []string{"staging", "production"} {
			if _, err := q.CreateEnvironment(ctx, store.CreateEnvironmentParams{
				ID: uuid.Must(uuid.NewV7()), ProjectID: project.ID, Name: env, Kind: "standard",
			}); err != nil {
				return err
			}
		}
		rights := make([]string, len(auth.AllRights))
		for i, r := range auth.AllRights {
			rights[i] = string(r)
		}
		role, err := q.CreateRole(ctx, store.CreateRoleParams{ID: uuid.Must(uuid.NewV7()), ProjectID: project.ID, Name: "admin", Capabilities: rights})
		if err != nil {
			return err
		}
		actor, err := q.CreateActor(ctx, store.CreateActorParams{
			ID: uuid.Must(uuid.NewV7()), Kind: string(auth.ActorService), ProjectID: &project.ID, DisplayName: "bootstrap-admin",
		})
		if err != nil {
			return err
		}
		if err := q.BindRole(ctx, store.BindRoleParams{ProjectID: project.ID, ActorID: actor.ID, RoleID: role.ID}); err != nil {
			return err
		}
		secret, hash := auth.NewToken(auth.ServiceTokenPrefix)
		token, err := q.CreateAPIToken(ctx, store.CreateAPITokenParams{
			ID: uuid.Must(uuid.NewV7()), ProjectID: project.ID, ActorID: actor.ID, TokenHash: hash,
			Scopes: []string{auth.ScopeAll}, ExpiresAt: time.Now().Add(tokenTTL),
		})
		if err != nil {
			return err
		}
		res = BootstrapResult{ProjectID: project.ID, ActorID: actor.ID, Token: secret, ExpiresAt: token.ExpiresAt}
		return nil
	})
	return res, err
}
