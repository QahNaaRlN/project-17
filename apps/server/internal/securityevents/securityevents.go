// Package securityevents — журнал событий безопасности SEC-005 (STU-504…509):
// отказы доступа, входы, выпуск токенов, роли и смены политик. Событие несёт
// коды, идентификаторы и хэши, но не тела запросов, токены и cookie.
package securityevents

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Event — одно событие журнала; ProjectID и ActorID пусты, когда неизвестны.
type Event struct {
	ProjectID *uuid.UUID
	ActorID   *uuid.UUID
	Type      string
	Data      map[string]any
}

// Record пишет событие тем же соединением или транзакцией, что и действие.
func Record(ctx context.Context, db store.DBTX, e Event) error {
	if e.Data == nil {
		e.Data = map[string]any{}
	}
	// Callers pass codes, identifiers and hashes only: plain JSON values.
	raw, _ := json.Marshal(e.Data)
	return store.New(db).InsertSecurityEvent(ctx, store.InsertSecurityEventParams{
		ID: uuid.Must(uuid.NewV7()), ProjectID: e.ProjectID, ActorID: e.ActorID, Type: e.Type, Data: raw,
	})
}
