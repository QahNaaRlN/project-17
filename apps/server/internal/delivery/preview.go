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
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/previewtoken"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// PreviewTTL — срок действия preview-токена (API-041: не больше 15 минут).
const PreviewTTL = previewtoken.TTL

type previewClaims = previewtoken.Claims

var jwtHeader = previewtoken.Header

func sign(key []byte, signingInput string) string { return previewtoken.MAC(key, signingInput) }

func signToken(key []byte, c previewClaims) string { return previewtoken.Sign(key, c) }

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
	// RegistrationSeq — сборка preview/pr-<N>, для которой выпущен токен (STU-329).
	RegistrationSeq *int32    `json:"registrationSeq,omitempty"`
	ExpiresAt       time.Time `json:"expiresAt"`
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
	var targets []string
	if p.ChangesetID != nil {
		cs, err := changes.GetChangeset(ctx, q, actor.ProjectID, *p.ChangesetID)
		if err != nil {
			return PreviewToken{}, err
		}
		targets = cs.Targets
	}
	var registration *int32
	if env.SourcePullNumber != nil {
		// STU-327: a PR preview shows a draft of its base environment only to
		// those who may read design.
		if !actor.Rights.Has(auth.DesignRead) || p.ChangesetID == nil || len(targets) > 0 && !slices.Contains(targets, env.SourceBaseName) {
			return PreviewToken{}, commandbus.NewError(http.StatusForbidden, "FORBIDDEN", "Нет доступа к предпросмотру PR", "Нужны design.read и Change Set базового окружения")
		}
		registration = &env.RegistrationSeq
	}
	expires := now().Add(PreviewTTL).Truncate(time.Second)
	token := signToken(env.PreviewKey, previewClaims{
		Project: actor.ProjectSlug, Environment: env.Name, Changeset: p.ChangesetID, Registration: registration, Subject: actor.ID, Expires: expires.Unix(),
	})
	return PreviewToken{Token: token, Environment: env.Name, ChangesetID: p.ChangesetID, RegistrationSeq: registration, ExpiresAt: expires}, nil
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
	// A new build of a PR preview invalidates tokens of the previous one.
	if env.SourcePullNumber != nil && (c.Registration == nil || *c.Registration != env.RegistrationSeq) {
		return Access{}, ErrUnauthenticated
	}
	return Access{ProjectID: env.ProjectID, ProjectSlug: c.Project, EnvironmentID: env.ID, Environment: env.Name,
		Draft: true, ChangesetID: c.Changeset, ExpiresAt: time.Unix(c.Expires, 0), PreviewKeyHash: previewFingerprint(env.PreviewKey), BaseEnvironment: env.SourceBaseName,
		Subject: "preview:" + c.Subject.String()}, nil
}

func previewFingerprint(key []byte) string {
	h := sha256.Sum256(key)
	return fmt.Sprintf("%x", h[:])
}
