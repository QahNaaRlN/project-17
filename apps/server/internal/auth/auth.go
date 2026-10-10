package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// ActorKind — вид актора (06-changes-publishing.md §2.1).
type ActorKind string

const (
	ActorHuman     ActorKind = "human"
	ActorAgent     ActorKind = "agent"
	ActorService   ActorKind = "service"
	ActorMigration ActorKind = "migration"
)

// Actor — аутентифицированный субъект запроса в контексте проекта.
type Actor struct {
	ID          uuid.UUID
	Kind        ActorKind
	DisplayName string
	ProjectID   uuid.UUID
	ProjectSlug string
	Rights      RightSet
}

// ServiceTokenPrefix — префикс сервисных токенов (08-api.md §2).
const ServiceTokenPrefix = "cms_svc_"

// DeliveryKeyPrefix — префикс публичных ключей доставки окружения (08-api.md §2).
const DeliveryKeyPrefix = "cms_pub_"

// ErrUnauthenticated — токен отсутствует, неизвестен, отозван или истёк.
var ErrUnauthenticated = errors.New("unauthenticated")

// NewToken возвращает новый секрет токена с префиксом и его хэш для хранения (SEC-002).
func NewToken(prefix string) (secret string, hash []byte) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	secret = prefix + base64.RawURLEncoding.EncodeToString(buf)
	return secret, HashToken(secret)
}

// HashToken — SHA-256 секрета токена.
func HashToken(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// Authenticate находит актора по заголовку Authorization ("Bearer <token>").
func Authenticate(ctx context.Context, q *store.Queries, authorization string) (Actor, error) {
	secret, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok || secret == "" {
		return Actor{}, ErrUnauthenticated
	}
	p, err := q.GetTokenPrincipal(ctx, HashToken(secret))
	if errors.Is(err, pgx.ErrNoRows) {
		return Actor{}, ErrUnauthenticated
	}
	if err != nil {
		return Actor{}, fmt.Errorf("auth: %w", err)
	}
	caps, err := q.ActorCapabilities(ctx, store.ActorCapabilitiesParams{ProjectID: p.ProjectID, ActorID: p.ActorID})
	if err != nil {
		return Actor{}, fmt.Errorf("auth: %w", err)
	}
	if err := q.TouchToken(ctx, p.TokenID); err != nil {
		return Actor{}, fmt.Errorf("auth: %w", err)
	}
	return Actor{
		ID:          p.ActorID,
		Kind:        ActorKind(p.ActorKind),
		DisplayName: p.DisplayName,
		ProjectID:   p.ProjectID,
		ProjectSlug: p.ProjectSlug,
		Rights:      EffectiveRights(caps, p.Scopes),
	}, nil
}
