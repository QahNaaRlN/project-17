package delivery

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// PreviewTTL — срок действия preview-токена (API-041: не больше 15 минут).
const PreviewTTL = 15 * time.Minute

// previewClaims — поля preview-токена (API-041).
type previewClaims struct {
	Project     string     `json:"prj"`
	Environment string     `json:"env"`
	Changeset   *uuid.UUID `json:"cs,omitempty"`
	Subject     uuid.UUID  `json:"sub"`
	Expires     int64      `json:"exp"`
}

var jwtHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

func sign(key []byte, signingInput string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signingInput))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// signToken выпускает JWT HS256 с полями claims.
func signToken(key []byte, c previewClaims) string {
	payload, _ := json.Marshal(c) // структура из простых полей
	input := jwtHeader + "." + base64.RawURLEncoding.EncodeToString(payload)
	return input + "." + sign(key, input)
}

// errTokenInvalid — подпись, формат или срок preview-токена не годятся.
var errTokenInvalid = errors.New("delivery: недействительный preview-токен")

// unverifiedClaims разбирает поля токена без проверки подписи — чтобы найти ключ окружения.
func unverifiedClaims(token string) (previewClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != jwtHeader {
		return previewClaims{}, errTokenInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return previewClaims{}, errTokenInvalid
	}
	var c previewClaims
	if err := json.Unmarshal(raw, &c); err != nil {
		return previewClaims{}, errTokenInvalid
	}
	return c, nil
}

// verifyToken проверяет подпись и срок токена.
func verifyToken(key []byte, token string, now time.Time) error {
	i := strings.LastIndexByte(token, '.')
	if i < 0 || !hmac.Equal([]byte(sign(key, token[:i])), []byte(token[i+1:])) {
		return errTokenInvalid
	}
	c, err := unverifiedClaims(token)
	if err != nil {
		return err
	}
	if now.Unix() >= c.Expires {
		return errTokenInvalid
	}
	return nil
}

// PreviewToken — ответ create-preview-token.
type PreviewToken struct {
	Token       string     `json:"token"`
	Environment string     `json:"environment"`
	ChangesetID *uuid.UUID `json:"changesetId"`
	ExpiresAt   time.Time  `json:"expiresAt"`
}

type previewPayload struct {
	Environment string     `json:"environment"`
	ChangesetID *uuid.UUID `json:"changesetId"`
}

// now — часы; подменяются в тестах.
var now = time.Now

func handleCreatePreviewToken(ctx context.Context, tx pgx.Tx, actor auth.Actor, p previewPayload) (PreviewToken, error) {
	q := store.New(tx)
	env, err := q.GetEnvironmentPreviewKey(ctx, store.GetEnvironmentPreviewKeyParams{Slug: actor.ProjectSlug, Name: p.Environment})
	if errors.Is(err, pgx.ErrNoRows) {
		return PreviewToken{}, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Окружение не найдено",
			fmt.Sprintf("Окружение %q не найдено", p.Environment))
	}
	if err != nil {
		return PreviewToken{}, err
	}
	if p.ChangesetID != nil {
		if _, err := changes.GetChangeset(ctx, q, actor.ProjectID, *p.ChangesetID); err != nil {
			return PreviewToken{}, err
		}
	}
	expires := now().Add(PreviewTTL).Truncate(time.Second)
	token := signToken(env.PreviewKey, previewClaims{
		Project: actor.ProjectSlug, Environment: env.Name, Changeset: p.ChangesetID, Subject: actor.ID, Expires: expires.Unix(),
	})
	return PreviewToken{Token: token, Environment: env.Name, ChangesetID: p.ChangesetID, ExpiresAt: expires}, nil
}

// AuthenticatePreview проверяет заголовок Authorization: Preview <JWT> (API-041).
func AuthenticatePreview(ctx context.Context, q *store.Queries, authorization string) (Access, error) {
	token, ok := strings.CutPrefix(authorization, "Preview ")
	if !ok {
		return Access{}, ErrUnauthenticated
	}
	c, err := unverifiedClaims(token)
	if err != nil {
		return Access{}, ErrUnauthenticated
	}
	env, err := q.GetEnvironmentPreviewKey(ctx, store.GetEnvironmentPreviewKeyParams{Slug: c.Project, Name: c.Environment})
	if errors.Is(err, pgx.ErrNoRows) {
		return Access{}, ErrUnauthenticated
	}
	if err != nil {
		return Access{}, err
	}
	if verifyToken(env.PreviewKey, token, now()) != nil {
		return Access{}, ErrUnauthenticated
	}
	return Access{ProjectID: env.ProjectID, ProjectSlug: c.Project, EnvironmentID: env.ID, Environment: env.Name,
		Draft: true, ChangesetID: c.Changeset}, nil
}
