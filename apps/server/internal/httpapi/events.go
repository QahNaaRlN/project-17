package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/realtime"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

func eventsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		values := r.URL.Query()
		for k, v := range values {
			if len(v) != 1 || (k != "documentId" && k != "changesetId" && k != "environment" && k != "after") {
				writeError(w, r, d.Log, badParam(k, "один поддерживаемый параметр"))
				return
			}
		}
		doc, err := uuid.Parse(values.Get("documentId"))
		if err != nil {
			writeError(w, r, d.Log, badParam("documentId", "UUID"))
			return
		}
		cs, err := uuid.Parse(values.Get("changesetId"))
		if err != nil {
			writeError(w, r, d.Log, badParam("changesetId", "UUID"))
			return
		}
		cursor := values.Get("after")
		if cursor == "" {
			cursor = r.Header.Get("Last-Event-ID")
		}
		if cursor != "" {
			n, e := strconv.ParseInt(cursor, 10, 64)
			if e != nil || n < 0 {
				writeError(w, r, d.Log, badParam("after", "неотрицательная ревизия"))
				return
			}
		}
		actor := actorFrom(r.Context())
		scope := realtime.Scope{DocumentID: doc, ChangesetID: cs, Environment: values.Get("environment")}
		if err := realtime.ValidateScope(ctx, store.New(d.Pool), actor, scope); err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		// Authentication is renewed at least every 25s. No pool connection is pinned.
		controller := http.NewResponseController(w)
		defer func() { _ = controller.SetWriteDeadline(time.Time{}) }()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		emit := func(kind, id string, value any) error {
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			if err = controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				return err
			}
			if id != "" {
				if _, err = fmt.Fprintf(w, "id: %s\n", id); err != nil {
					return err
				}
			}
			if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, raw); err != nil {
				return err
			}
			return controller.Flush()
		}
		poll := time.NewTicker(time.Second)
		defer poll.Stop()
		first := true
		ticks := 0
		for {
			var revision string
			err := d.Pool.QueryRow(ctx, `SELECT COALESCE((SELECT revision FROM project_revisions WHERE project_id=$1),0)::text`, actor.ProjectID).Scan(&revision)
			if err != nil {
				return
			}
			if first || revision != cursor {
				kind := "invalidate"
				if first && cursor == revision {
					kind = "sync"
				}
				if err := emit(kind, revision, map[string]string{"revision": revision}); err != nil {
					return
				}
				cursor = revision
			}
			if first || ticks%10 == 0 {
				rows, err := d.Pool.Query(ctx, `SELECT DISTINCT p.actor_id,a.display_name,p.changeset_id,c.title
     FROM presence_leases p JOIN actors a ON a.id=p.actor_id JOIN changesets c ON c.id=p.changeset_id
     WHERE p.project_id=$1 AND p.environment=$2 AND p.document_id=$3 AND p.expires_at>now() AND a.disabled_at IS NULL
     ORDER BY p.actor_id,p.changeset_id LIMIT 100`, actor.ProjectID, scope.Environment, scope.DocumentID)
				if err != nil {
					return
				}
				items := []realtime.Presence{}
				for rows.Next() {
					var p realtime.Presence
					if err := rows.Scan(&p.ActorID, &p.ActorName, &p.ChangesetID, &p.ChangesetTitle); err != nil {
						rows.Close()
						return
					}
					items = append(items, p)
				}
				err = rows.Err()
				rows.Close()
				if err != nil {
					return
				}
				if err := emit("presence", "", map[string]any{"items": items}); err != nil {
					return
				}
				if err := emit("sync", revision, map[string]string{"revision": revision}); err != nil {
					return
				}
			}
			first = false
			select {
			case <-ctx.Done():
				return
			case <-poll.C:
				ticks++
			}
		}
	}
}
