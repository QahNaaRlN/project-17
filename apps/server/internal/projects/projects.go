// Package projects — проекты и окружения (07-storage.md §2), первичная настройка проекта.
package projects

import (
	"context"
	"errors"
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
	"github.com/qahnaarln/project-17/apps/server/internal/securityevents"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Environment — окружение в ответах API.
type Environment struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	AppURL    *string   `json:"appUrl"`
	CreatedAt time.Time `json:"createdAt"`
	// Preview lifecycle (STU-315…326); absent for standard environments.
	Source           *SourceView `json:"source,omitempty"`
	RegistrationSeq  *int32      `json:"registrationSeq,omitempty"`
	LastRegisteredAt *time.Time  `json:"lastRegisteredAt,omitempty"`
	ExpiresAt        *time.Time  `json:"expiresAt,omitempty"`
	ArchivedAt       *time.Time  `json:"archivedAt,omitempty"`
	ArchiveReason    *string     `json:"archiveReason,omitempty"`
}

func toEnvironment(e store.Environment) Environment {
	out := Environment{ID: e.ID, Name: e.Name, Kind: e.Kind, AppURL: e.AppUrl, CreatedAt: e.CreatedAt}
	if e.Kind == "preview" {
		seq := e.RegistrationSeq
		out.RegistrationSeq, out.LastRegisteredAt, out.ExpiresAt, out.ArchivedAt, out.ArchiveReason = &seq, e.LastRegisteredAt, e.ExpiresAt, e.ArchivedAt, e.ArchiveReason
		if e.ArchivedName != nil {
			out.Name = *e.ArchivedName
		}
		if e.SourcePullNumber != nil {
			out.Source = &SourceView{BaseEnvironmentID: *e.SourceBaseEnvironmentID, RepositoryID: *e.SourceRepositoryID, PullNumber: *e.SourcePullNumber}
		}
	}
	return out
}

// CreateEnvironmentPayload — payload команды create-environment (08-api.md §3.2).
type CreateEnvironmentPayload struct {
	Name   string  `json:"name"`
	Kind   string  `json:"kind"`
	AppURL *string `json:"appUrl"`
	// Source binds a preview environment to a pull request of the base
	// environment's GitHub configuration (STU-315).
	Source *SourcePayload `json:"source,omitempty"`
}

var environmentName = regexp.MustCompile(`^[a-z0-9][a-z0-9/_-]{0,62}$`)

// Register регистрирует команды модуля в шине без preview-источников.
func Register(bus *commandbus.Bus) { RegisterWith(bus, Preview{}) }

// RegisterWith регистрирует команды; cfg настраивает окружения preview/pr-<N>
// с источником (STU-315…328), без него такие окружения отклоняются.
func RegisterWith(bus *commandbus.Bus, cfg Preview) {
	registerPreview(bus, cfg)
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
			if p.Source != nil {
				return cfg.create(ctx, tx, actor, p)
			}
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
	// STU-481: stages 8–9 render through the environment's application; bootstrap
	// environments have no address yet.
	commandbus.Register(bus, commandbus.Command[AppURLPayload, Environment]{
		Name:  "set-environment-app-url",
		Right: auth.ProjectAdmin,
		Validate: func(p AppURLPayload) error {
			u, err := url.Parse(p.AppURL)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return commandbus.Validation(map[string]string{"appUrl": "абсолютный URL http(s) без учётных данных, запроса и фрагмента"})
			}
			return nil
		},
		Handle: func(ctx context.Context, tx pgx.Tx, actor auth.Actor, p AppURLPayload) (Environment, error) {
			env, err := store.New(tx).SetEnvironmentAppURL(ctx, store.SetEnvironmentAppURLParams{ProjectID: actor.ProjectID, Name: p.Environment, AppUrl: &p.AppURL})
			if errors.Is(err, pgx.ErrNoRows) {
				return Environment{}, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Окружение не найдено",
					fmt.Sprintf("Стандартного окружения %q нет в проекте", p.Environment))
			}
			if err != nil {
				return Environment{}, err
			}
			return toEnvironment(env), nil
		},
	})
}

// AppURLPayload — payload команды set-environment-app-url (STU-481).
type AppURLPayload struct {
	Environment string `json:"environment"`
	AppURL      string `json:"appUrl"`
}

func validateEnvironment(p CreateEnvironmentPayload) error {
	fields := map[string]string{}
	if !environmentName.MatchString(p.Name) {
		fields["name"] = "1…63 символа: строчные латинские буквы, цифры, '-', '_', '/'; начинается с буквы или цифры"
	}
	if p.Kind != "standard" && p.Kind != "preview" {
		fields["kind"] = "допустимо: standard, preview"
	}
	if p.Source != nil && (p.Kind != "preview" || p.Source.PullNumber < 1 || p.Source.BaseEnvironment == "" || p.Name != PreviewName(p.Source.PullNumber)) {
		fields["source"] = "preview с источником: kind=preview, имя preview/pr-<pullNumber>, baseEnvironment"
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

// Bootstrap создаёт проект, окружения staging и production, роль admin с обычными правами
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
		// STU-345: the project's token-less system actor for capability fulfillment.
		if err := q.CreateSystemActor(ctx, store.CreateSystemActorParams{ID: uuid.Must(uuid.NewV7()), ProjectID: &project.ID}); err != nil {
			return err
		}
		for _, env := range []string{"staging", "production"} {
			if _, err := q.CreateEnvironment(ctx, store.CreateEnvironmentParams{
				ID: uuid.Must(uuid.NewV7()), ProjectID: project.ID, Name: env, Kind: "standard",
			}); err != nil {
				return err
			}
		}
		rights := make([]string, 0, len(auth.AllRights)-1)
		for _, r := range auth.AllRights {
			// External sharing always requires a separately granted role, even
			// when a human is subsequently bound to this bootstrap admin role.
			// The check runner's right belongs to its own service actor (STU-488).
			if r != auth.CapabilityDeliver && r != auth.ChecksRun {
				rights = append(rights, string(r))
			}
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
		return tokenCreated(ctx, tx, project.ID, actor.ID, token)
	})
	return res, err
}

// CheckRunnerToken issues a token of the project's cms-checks service actor, whose only
// right is checks.run (STU-488). The actor and its role are created on first use.
func CheckRunnerToken(ctx context.Context, pool *pgxpool.Pool, slug string, tokenTTL time.Duration) (BootstrapResult, error) {
	var res BootstrapResult
	err := postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		project, err := q.GetProjectBySlug(ctx, slug)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("проект %q не найден", slug)
		}
		if err != nil {
			return err
		}
		var actorID uuid.UUID
		err = tx.QueryRow(ctx, `SELECT a.id FROM actors a JOIN role_bindings b ON b.actor_id = a.id JOIN roles r ON r.id = b.role_id
			WHERE a.project_id = $1 AND a.kind = 'service' AND a.display_name = 'cms-checks' AND r.name = 'check-runner' LIMIT 1`, project.ID).Scan(&actorID)
		if errors.Is(err, pgx.ErrNoRows) {
			role, err := q.CreateRole(ctx, store.CreateRoleParams{ID: uuid.Must(uuid.NewV7()), ProjectID: project.ID, Name: "check-runner", Capabilities: []string{string(auth.ChecksRun)}})
			if err != nil {
				return err
			}
			actor, err := q.CreateActor(ctx, store.CreateActorParams{ID: uuid.Must(uuid.NewV7()), Kind: string(auth.ActorService), ProjectID: &project.ID, DisplayName: "cms-checks"})
			if err != nil {
				return err
			}
			if err := q.BindRole(ctx, store.BindRoleParams{ProjectID: project.ID, ActorID: actor.ID, RoleID: role.ID}); err != nil {
				return err
			}
			actorID = actor.ID
		} else if err != nil {
			return err
		}
		secret, hash := auth.NewToken(auth.ServiceTokenPrefix)
		token, err := q.CreateAPIToken(ctx, store.CreateAPITokenParams{
			ID: uuid.Must(uuid.NewV7()), ProjectID: project.ID, ActorID: actorID, TokenHash: hash,
			Scopes: []string{string(auth.ChecksRun)}, ExpiresAt: time.Now().Add(tokenTTL),
		})
		if err != nil {
			return err
		}
		res = BootstrapResult{ProjectID: project.ID, ActorID: actorID, Token: secret, ExpiresAt: token.ExpiresAt}
		return tokenCreated(ctx, tx, project.ID, actorID, token)
	})
	return res, err
}

// tokenCreated — STU-507: a token issued by the operator in the SEC-005 journal; the
// token ID, its actor, scopes and expiry, never the secret.
func tokenCreated(ctx context.Context, tx pgx.Tx, project, actor uuid.UUID, token store.ApiToken) error {
	return securityevents.Record(ctx, tx, securityevents.Event{ProjectID: &project, ActorID: &actor, Type: "token.created",
		Data: map[string]any{"tokenId": token.ID, "scopes": token.Scopes, "expiresAt": token.ExpiresAt}})
}
