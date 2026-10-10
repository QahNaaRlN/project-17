// Package delivery — Delivery API (docs/spec/08-api.md §5): чтение опубликованного приложением
// по публичному ключу доставки окружения.
package delivery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Key — ключ доставки в ответах API (секрет показывается только при создании).
type Key struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Environment string     `json:"environment"`
	CreatedBy   uuid.UUID  `json:"createdBy"`
	CreatedAt   time.Time  `json:"createdAt"`
	RevokedAt   *time.Time `json:"revokedAt"`
	Secret      string     `json:"key,omitempty"`
}

// Register регистрирует команды модуля.
func Register(bus *commandbus.Bus) {
	commandbus.Register(bus, commandbus.Command[createKeyPayload, Key]{
		Name:     "create-delivery-key",
		Right:    auth.ProjectAdmin,
		Validate: validateCreateKey,
		Handle:   handleCreateKey,
	})
	commandbus.Register(bus, commandbus.Command[revokeKeyPayload, Key]{
		Name:   "revoke-delivery-key",
		Right:  auth.ProjectAdmin,
		Handle: handleRevokeKey,
	})
}

type createKeyPayload struct {
	Environment string `json:"environment"`
	Name        string `json:"name"`
}

func validateCreateKey(p createKeyPayload) error {
	if n := len([]rune(strings.TrimSpace(p.Name))); n == 0 || n > 100 {
		return commandbus.Validation(map[string]string{"name": "1…100 символов"})
	}
	return nil
}

func handleCreateKey(ctx context.Context, tx pgx.Tx, actor auth.Actor, p createKeyPayload) (Key, error) {
	q := store.New(tx)
	env, err := q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: actor.ProjectID, Name: p.Environment})
	if errors.Is(err, pgx.ErrNoRows) {
		return Key{}, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Окружение не найдено",
			fmt.Sprintf("Окружение %q не найдено", p.Environment))
	}
	if err != nil {
		return Key{}, err
	}
	secret, hash := auth.NewToken(auth.DeliveryKeyPrefix)
	k, err := q.CreateDeliveryKey(ctx, store.CreateDeliveryKeyParams{
		ID: uuid.Must(uuid.NewV7()), ProjectID: actor.ProjectID, EnvironmentID: env.ID,
		Name: strings.TrimSpace(p.Name), TokenHash: hash, CreatedBy: actor.ID,
	})
	if err != nil {
		return Key{}, err
	}
	return Key{ID: k.ID, Name: k.Name, Environment: env.Name, CreatedBy: k.CreatedBy, CreatedAt: k.CreatedAt, Secret: secret}, nil
}

type revokeKeyPayload struct {
	ID uuid.UUID `json:"id"`
}

func handleRevokeKey(ctx context.Context, tx pgx.Tx, actor auth.Actor, p revokeKeyPayload) (Key, error) {
	q := store.New(tx)
	r, err := q.RevokeDeliveryKey(ctx, store.RevokeDeliveryKeyParams{ID: p.ID, ProjectID: actor.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Key{}, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Ключ не найден",
			fmt.Sprintf("Действующий ключ доставки %s не найден", p.ID))
	}
	if err != nil {
		return Key{}, err
	}
	return Key{ID: r.ID, Name: r.Name, Environment: r.EnvironmentName, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, RevokedAt: r.RevokedAt}, nil
}

// ListKeys — ключи доставки проекта (без секретов).
func ListKeys(ctx context.Context, q *store.Queries, projectID uuid.UUID) ([]Key, error) {
	rows, err := q.ListDeliveryKeys(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]Key, len(rows))
	for i, r := range rows {
		out[i] = Key{ID: r.ID, Name: r.Name, Environment: r.EnvironmentName, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, RevokedAt: r.RevokedAt}
	}
	return out, nil
}

// Access — область действия предъявленного ключа доставки.
type Access struct {
	ProjectID     uuid.UUID
	ProjectSlug   string
	EnvironmentID uuid.UUID
	Environment   string
}

// ErrUnauthenticated — ключ не передан, не найден или отозван.
var ErrUnauthenticated = errors.New("delivery: недействительный ключ доставки")

// Authenticate проверяет заголовок Authorization: Bearer cms_pub_….
func Authenticate(ctx context.Context, q *store.Queries, authorization string) (Access, error) {
	secret, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok || !strings.HasPrefix(secret, auth.DeliveryKeyPrefix) {
		return Access{}, ErrUnauthenticated
	}
	row, err := q.AuthenticateDeliveryKey(ctx, auth.HashToken(secret))
	if errors.Is(err, pgx.ErrNoRows) {
		return Access{}, ErrUnauthenticated
	}
	if err != nil {
		return Access{}, err
	}
	return Access{ProjectID: row.ProjectID, ProjectSlug: row.ProjectSlug, EnvironmentID: row.EnvironmentID, Environment: row.EnvironmentName}, nil
}
