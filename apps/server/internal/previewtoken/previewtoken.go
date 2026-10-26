// Package previewtoken — подпись preview-токенов черновика (API-041). Delivery API
// проверяет их, а pipeline выдаёт исполнителю проверок этапов 8–9 (STU-485).
package previewtoken

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// TTL — срок действия preview-токена (API-041: не больше 15 минут).
const TTL = 15 * time.Minute

// Claims — поля preview-токена (API-041).
type Claims struct {
	Project     string     `json:"prj"`
	Environment string     `json:"env"`
	Changeset   *uuid.UUID `json:"cs,omitempty"`
	// Registration pins a token of a preview environment with a source to the
	// build it was issued for (STU-329).
	Registration *int32    `json:"reg,omitempty"`
	Subject      uuid.UUID `json:"sub"`
	Expires      int64     `json:"exp"`
}

// Header — заголовок JWT HS256.
var Header = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

// MAC — подпись signingInput ключом окружения.
func MAC(key []byte, signingInput string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signingInput))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Sign выпускает JWT HS256 с полями claims.
func Sign(key []byte, c Claims) string {
	payload, _ := json.Marshal(c) // структура из простых полей
	input := Header + "." + base64.RawURLEncoding.EncodeToString(payload)
	return input + "." + MAC(key, input)
}

// IssueForChangeset — токен черновика CS в окружении для исполнителя проверок (STU-485);
// выдаётся только вместе с арендованной проверкой.
func IssueForChangeset(ctx context.Context, q *store.Queries, project, environment string, cs, subject uuid.UUID, now time.Time) (string, error) {
	env, err := q.GetEnvironmentPreviewKey(ctx, store.GetEnvironmentPreviewKeyParams{Slug: project, Name: environment})
	if err != nil {
		return "", err
	}
	return Sign(env.PreviewKey, Claims{Project: project, Environment: env.Name, Changeset: &cs, Subject: subject,
		Expires: now.Add(TTL).Truncate(time.Second).Unix()}), nil
}
