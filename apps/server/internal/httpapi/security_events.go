package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// securityEvent — событие журнала в ответе Query API.
type securityEvent struct {
	ID        uuid.UUID       `json:"id"`
	ActorID   *uuid.UUID      `json:"actorId"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"createdAt"`
}

// securityEventsHandler — STU-509: журнал проекта для project.admin, новые первыми,
// keyset по (createdAt, id). Курсор не подписан: он лишь сдвигает позицию внутри
// проекта актора.
func securityEventsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r.Context())
		if err := commandbus.Require(actor, auth.ProjectAdmin); err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		params := r.URL.Query()
		for k, v := range params {
			if len(v) != 1 || (k != "type" && k != "after" && k != "limit") {
				writeError(w, r, d.Log, badParam(k, "type, after, limit"))
				return
			}
		}
		limit := 50
		if params.Has("limit") {
			var err error
			if limit, err = strconv.Atoi(params.Get("limit")); err != nil || limit < 1 || limit > 100 {
				writeError(w, r, d.Log, badParam("limit", "1..100"))
				return
			}
		}
		typ := params.Get("type")
		args := store.ListSecurityEventsParams{ProjectID: &actor.ProjectID, PageLimit: int32(limit + 1)}
		if typ != "" {
			if len(typ) > 64 {
				writeError(w, r, d.Log, badParam("type", "≤ 64"))
				return
			}
			args.Type = &typ
		}
		if cursor := params.Get("after"); cursor != "" {
			raw, err := base64.RawURLEncoding.DecodeString(cursor)
			parts := strings.Split(string(raw), "|")
			if err != nil || len(parts) != 2 {
				writeError(w, r, d.Log, badParam("after", "cursor"))
				return
			}
			at, e1 := time.Parse(time.RFC3339Nano, parts[0])
			id, e2 := uuid.Parse(parts[1])
			if errors.Join(e1, e2) != nil {
				writeError(w, r, d.Log, badParam("after", "cursor"))
				return
			}
			args.BeforeTime, args.BeforeID = &at, &id
		}
		rows, err := store.New(d.Pool).ListSecurityEvents(r.Context(), args)
		if err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		items := []securityEvent{}
		var next *string
		for i, e := range rows {
			if i == limit {
				last := rows[i-1]
				c := base64.RawURLEncoding.EncodeToString([]byte(last.CreatedAt.Format(time.RFC3339Nano) + "|" + last.ID.String()))
				next = &c
				break
			}
			items = append(items, securityEvent{ID: e.ID, ActorID: e.ActorID, Type: e.Type, Data: e.Data, CreatedAt: e.CreatedAt})
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
	}
}
