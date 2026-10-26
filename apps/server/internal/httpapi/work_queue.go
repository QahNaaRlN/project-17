package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

type queueCounts struct {
	Page      int32 `json:"page"`
	Component int32 `json:"component"`
	Entity    int32 `json:"entity"`
	Asset     int32 `json:"asset"`
}
type queueItem struct {
	ID           uuid.UUID   `json:"id"`
	Title        string      `json:"title"`
	OwnerID      uuid.UUID   `json:"ownerId"`
	OwnerName    string      `json:"ownerName"`
	Own          bool        `json:"own"`
	State        string      `json:"state"`
	CreatedAt    time.Time   `json:"createdAt"`
	UpdatedAt    time.Time   `json:"updatedAt"`
	Seq          int32       `json:"seq"`
	ObjectCount  int32       `json:"objectCount"`
	CountsByKind queueCounts `json:"countsByKind"`
	DeletedCount int32       `json:"deletedCount"`
}
type queueCursor struct {
	Version int       `json:"v"`
	Context string    `json:"context"`
	Time    time.Time `json:"time"`
	ID      uuid.UUID `json:"id"`
}

func queueError(status int, code string) error {
	return commandbus.NewError(status, code, "Работа недоступна", "")
}
func signQueueCursor(c queueCursor, key []byte) string {
	body, _ := json.Marshal(c)
	raw := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(raw))
	return raw + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func parseQueueCursor(raw, context string, key []byte) (queueCursor, error) {
	var c queueCursor
	fail := queueError(400, "WORK_QUEUE_CURSOR_INVALID")
	parts := strings.Split(raw, ".")
	if len(raw) > 2048 || len(parts) != 2 {
		return c, fail
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(parts[0]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return c, fail
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(body, &c) != nil || c.Version != 1 || c.Context != context || c.Time.IsZero() || c.ID == uuid.Nil {
		return c, fail
	}
	return c, nil
}

// Intersect freshly assigned rights with the token's original restrictions.
func currentQueueActor(ctx context.Context, q *store.Queries, a auth.Actor) (auth.Actor, error) {
	caps, err := q.ActorCapabilities(ctx, store.ActorCapabilitiesParams{ProjectID: a.ProjectID, ActorID: a.ID})
	if err != nil {
		return a, err
	}
	current := auth.EffectiveRights(caps, []string{auth.ScopeAll})
	rights := auth.RightSet{}
	for right, allowed := range a.Rights {
		if allowed && current.Has(right) {
			rights[right] = true
		}
	}
	a.Rights = rights
	return a, nil
}

// Direct readers must not reveal someone else's draft or a partially readable CS.
// Own historical CS remain readable for the existing editor/history APIs.
func authorizeWorkRead(ctx context.Context, q *store.Queries, a auth.Actor, id uuid.UUID) error {
	a, err := currentQueueActor(ctx, q, a)
	if err != nil {
		return err
	}
	cs, err := q.GetChangeset(ctx, store.GetChangesetParams{ProjectID: a.ProjectID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return queueError(404, "NOT_FOUND")
	}
	if err != nil {
		return err
	}
	if !auth.AgentAllowsCS(a, id) || !a.Rights.Has(auth.DesignRead) && !a.Rights.Has(auth.ContentRead) ||
		cs.OwnerID != a.ID && (a.Kind != auth.ActorHuman || !a.Rights.Has(auth.ContentPublish) || !slices.Contains([]string{"in_review", "approved"}, cs.State)) {
		return queueError(404, "NOT_FOUND")
	}
	objects, err := q.WorkQueueObjects(ctx, store.WorkQueueObjectsParams{ProjectID: a.ProjectID, ChangesetID: id})
	if err != nil {
		return err
	}
	if len(objects) > 1000 {
		return queueError(413, "WORK_QUEUE_LIMIT_EXCEEDED")
	}
	for _, o := range objects {
		right := auth.ContentRead
		if o.Kind == "document" {
			right = auth.DesignRead
		}
		if !a.Rights.Has(right) {
			return queueError(404, "NOT_FOUND")
		}
	}
	return nil
}
func workQueueRoutes(router chi.Router, d Deps) {
	for _, route := range []string{"/work-queue", "/work-queue/{id}"} {
		router.Get(route, func(w http.ResponseWriter, r *http.Request) {
			values := r.URL.Query()
			allowed := []string{"environment"}
			detail := route != "/work-queue"
			if !detail {
				allowed = append(allowed, "q", "state", "ownership", "kind", "limit", "cursor")
			}
			for k, v := range values {
				if len(v) != 1 || !slices.Contains(allowed, k) {
					writeError(w, r, d.Log, badParam(k, "one supported parameter"))
					return
				}
			}
			search := strings.TrimSpace(values.Get("q"))
			if !utf8.ValidString(search) || utf8.RuneCountInString(search) > 200 || strings.ContainsRune(search, 0) {
				writeError(w, r, d.Log, badParam("q", "200 characters without NUL"))
				return
			}
			limit := 50
			if values.Has("limit") {
				n, e := strconv.Atoi(values.Get("limit"))
				if e != nil || n < 1 || n > 100 {
					writeError(w, r, d.Log, badParam("limit", "1..100"))
					return
				}
				limit = n
			}
			filters := map[string][]string{"state": {"all", "open", "checking", "failed", "changes_requested", "in_review", "approved"}, "ownership": {"all", "mine", "others"}, "kind": {"all", "page", "component", "entity", "asset"}}
			selected := map[string]string{}
			for k, valid := range filters {
				v := values.Get(k)
				if !values.Has(k) {
					v = "all"
				}
				if !slices.Contains(valid, v) {
					writeError(w, r, d.Log, badParam(k, "supported filter"))
					return
				}
				selected[k] = v
			}
			if values.Get("environment") == "" {
				writeError(w, r, d.Log, badParam("environment", "standard environment"))
				return
			}
			id := uuid.Nil
			if detail {
				var ok bool
				id, ok = uuidParam(w, r, d, "id")
				if !ok {
					return
				}
			}
			tx, err := d.Pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			defer tx.Rollback(r.Context()) //nolint:errcheck
			q := store.New(tx)
			a, err := currentQueueActor(r.Context(), q, actorFrom(r.Context()))
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			if a.Kind == auth.ActorAgent || !a.Rights.Has(auth.DesignRead) && !a.Rights.Has(auth.ContentRead) {
				writeError(w, r, d.Log, queueError(403, "FORBIDDEN"))
				return
			}
			env, err := q.GetEnvironmentByName(r.Context(), store.GetEnvironmentByNameParams{ProjectID: a.ProjectID, Name: values.Get("environment")})
			if errors.Is(err, pgx.ErrNoRows) || err == nil && env.Kind != "standard" {
				writeError(w, r, d.Log, queueError(404, "NOT_FOUND"))
				return
			}
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			rawContext, _ := json.Marshal([]any{a.ProjectID, a.ID, env.Name, search, selected["state"], selected["ownership"], selected["kind"], limit})
			digest := sha256.Sum256(rawContext)
			context := base64.RawURLEncoding.EncodeToString(digest[:])
			args := store.WorkQueueParams{ProjectID: a.ProjectID, ActorID: a.ID, Environment: env.Name, ItemID: id, Search: search, StateFilter: selected["state"], Ownership: selected["ownership"], KindFilter: selected["kind"], CanDesign: a.Rights.Has(auth.DesignRead), CanContent: a.Rights.Has(auth.ContentRead), CanReview: a.Kind == auth.ActorHuman && a.Rights.Has(auth.ContentPublish), PageLimit: int32(limit + 1), AfterTime: time.Unix(0, 0)}
			if values.Has("cursor") {
				c, e := parseQueueCursor(values.Get("cursor"), context, env.PreviewKey)
				if e != nil {
					writeError(w, r, d.Log, e)
					return
				}
				args.HasCursor = true
				args.AfterTime = c.Time
				args.AfterID = c.ID
			}
			rows, err := q.WorkQueue(r.Context(), args)
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			items := []queueItem{}
			var next *string
			if len(rows) > limit {
				last := rows[limit-1]
				s := signQueueCursor(queueCursor{1, context, last.CreatedAt, last.ID}, env.PreviewKey)
				next = &s
				rows = rows[:limit]
			}
			for _, row := range rows {
				items = append(items, queueItem{row.ID, row.Title, row.OwnerID, row.OwnerName, row.OwnerID == a.ID, row.State, row.CreatedAt, row.UpdatedAt, row.Seq, row.PageCount + row.ComponentCount + row.EntityCount + row.AssetCount, queueCounts{row.PageCount, row.ComponentCount, row.EntityCount, row.AssetCount}, row.DeletedCount})
			}
			var result any = struct {
				Items      []queueItem `json:"items"`
				NextCursor *string     `json:"nextCursor"`
			}{items, next}
			if detail {
				if len(items) != 1 {
					writeError(w, r, d.Log, queueError(404, "WORK_QUEUE_ITEM_NOT_FOUND"))
					return
				}
				objects, e := q.WorkQueueObjects(r.Context(), store.WorkQueueObjectsParams{ProjectID: a.ProjectID, ChangesetID: id})
				if e != nil {
					writeError(w, r, d.Log, e)
					return
				}
				if len(objects) > 1000 {
					writeError(w, r, d.Log, queueError(413, "WORK_QUEUE_LIMIT_EXCEEDED"))
					return
				}
				if objects == nil {
					objects = []store.WorkQueueObjectsRow{}
				}
				result = struct {
					Item    queueItem                   `json:"item"`
					Objects []store.WorkQueueObjectsRow `json:"objects"`
				}{items[0], objects}
			}
			raw, err := json.Marshal(result)
			if err == nil && len(raw) > 4<<20 {
				err = queueError(413, "WORK_QUEUE_LIMIT_EXCEEDED")
			}
			if err == nil {
				err = tx.Commit(r.Context())
			}
			w.Header().Set("Cache-Control", "no-store")
			respond(w, r, d, result, err)
		})
	}
}
