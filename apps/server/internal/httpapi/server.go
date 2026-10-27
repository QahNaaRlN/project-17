// Package httpapi — HTTP-интерфейс сервера: Command API и Query API (08-api.md).
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qahnaarln/project-17/apps/server/internal/agents"
	"github.com/qahnaarln/project-17/apps/server/internal/assets"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/capabilities"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/delivery"
	"github.com/qahnaarln/project-17/apps/server/internal/deliverycache"
	"github.com/qahnaarln/project-17/apps/server/internal/identity"
	"github.com/qahnaarln/project-17/apps/server/internal/imagedelivery"
	"github.com/qahnaarln/project-17/apps/server/internal/manifestregistry"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/ratelimit"
	"github.com/qahnaarln/project-17/apps/server/internal/securityevents"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// MaxCommandBody — максимальный размер тела команды.
const MaxCommandBody = 1 << 20

// Deps — зависимости HTTP-слоя.
type Deps struct {
	CapabilityGithub   *capabilities.GithubService
	CapabilityDelivery *capabilities.DeliveryService
	Agents             *agents.Service
	Identity           *identity.Service
	Pool               *pgxpool.Pool
	Bus                *commandbus.Bus
	Log                *slog.Logger
	Version            string
	Assets             *assets.Service
	Images             *imagedelivery.Service
	Cache              *deliverycache.Cache // nil — без кэша Delivery (STU-520)
	Limiter            *ratelimit.Limiter   // nil — без лимитов
	Limits             ratelimit.Limits
}

// NewRouter собирает маршруты сервера.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger(d.Log))
	r.Use(recoverer(d.Log))

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": d.Version})
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := d.Pool.Ping(ctx); err != nil {
			d.Log.WarnContext(r.Context(), "readiness check failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "database": "down"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	r.Route("/delivery/v1/{project}/{env}", deliveryRoutes(d))
	r.Get("/assets/v1/{env}/{id}/{hash}", imageHandler(d))
	if d.Identity != nil {
		r.Mount("/auth", d.Identity)
	}

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(auditRefusals(d))
		r.Use(authenticate(d))
		r.Use(limitRequests(d))
		r.Get("/security-events", securityEventsHandler(d))
		agentRoutes(r, d)
		r.Get("/events", eventsHandler(d))
		workspaceRoutes(r, d)
		capabilityRoutes(r, d)
		capabilityDeliveryRoutes(r, d)
		capabilityGithubRoutes(r, d)
		capabilityFulfillmentRoutes(r, d)
		capabilityPreviewRoutes(r, d)
		r.Get("/changesets/{id}/history", historyHandler(d))
		r.Post("/commands/{name}", commandHandler(d))
		r.Get("/asset-uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
			id, ok := uuidParam(w, r, d, "id")
			if !ok {
				return
			}
			s := d.Assets
			if s == nil {
				s = &assets.Service{Pool: d.Pool}
			}
			u, err := s.Get(r.Context(), actorFrom(r.Context()), id)
			respond(w, r, d, u, err)
		})
		r.Get("/manifest", func(w http.ResponseWriter, r *http.Request) {
			actor := actorFrom(r.Context())
			if err := commandbus.Require(actor, auth.DesignRead); err != nil && !actor.Rights.Has(auth.ContentRead) {
				writeError(w, r, d.Log, err)
				return
			}
			active, err := manifestregistry.GetActive(r.Context(), store.New(d.Pool), actor.ProjectID, r.URL.Query().Get("environment"))
			respond(w, r, d, active, err)
		})
		r.Get("/schemas", func(w http.ResponseWriter, r *http.Request) {
			actor := actorFrom(r.Context())
			if err := commandbus.Require(actor, auth.SchemaRead); err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			schemas, err := manifestregistry.GetSchemas(r.Context(), store.New(d.Pool), actor.ProjectID, r.URL.Query().Get("environment"))
			respond(w, r, d, schemas, err)
		})
		r.Get("/environments", func(w http.ResponseWriter, r *http.Request) {
			actor := actorFrom(r.Context())
			envs, err := projects.ListEnvironments(r.Context(), store.New(d.Pool), actor.ProjectID)
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": envs})
		})

		r.Get("/changesets", func(w http.ResponseWriter, r *http.Request) {
			actor := actorFrom(r.Context())
			var state *string
			if s := r.URL.Query().Get("state"); s != "" {
				state = &s
			}
			items, err := changes.ListChangesets(r.Context(), store.New(d.Pool), actor.ProjectID, state)
			respond(w, r, d, map[string]any{"items": items}, err)
		})
		r.Get("/changesets/{id}", func(w http.ResponseWriter, r *http.Request) {
			id, ok := uuidParam(w, r, d, "id")
			if !ok {
				return
			}
			cs, err := changes.GetChangeset(r.Context(), store.New(d.Pool), actorFrom(r.Context()).ProjectID, id)
			if err == nil && cs.Kind == "standard" {
				err = authorizeWorkRead(r.Context(), store.New(d.Pool), actorFrom(r.Context()), id)
			}
			respond(w, r, d, cs, err)
		})
		r.Get("/changesets/{id}/operations", func(w http.ResponseWriter, r *http.Request) {
			id, ok := uuidParam(w, r, d, "id")
			if !ok {
				return
			}
			after := 0
			if s := r.URL.Query().Get("afterSeq"); s != "" {
				n, err := strconv.Atoi(s)
				if err != nil || n < 0 {
					writeError(w, r, d.Log, badParam("afterSeq", "неотрицательное целое"))
					return
				}
				after = n
			}
			items, err := changes.ListOperations(r.Context(), store.New(d.Pool), actorFrom(r.Context()).ProjectID, id, int32(after))
			respond(w, r, d, map[string]any{"items": items}, err)
		})
		r.Get("/changesets/{id}/review", reviewHandler(d))
		reviewDiffRoutes(r, d)
		publicationHistoryRoutes(r, d)
		r.Get("/delivery-keys", func(w http.ResponseWriter, r *http.Request) {
			actor := actorFrom(r.Context())
			if err := commandbus.Require(actor, auth.ProjectAdmin); err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			keys, err := delivery.ListKeys(r.Context(), store.New(d.Pool), actor.ProjectID)
			respond(w, r, d, map[string]any{"items": keys}, err)
		})
		r.Get("/publications", func(w http.ResponseWriter, r *http.Request) {
			var env *string
			if s := r.URL.Query().Get("environment"); s != "" {
				env = &s
			}
			items, err := publishing.ListPublications(r.Context(), store.New(d.Pool), actorFrom(r.Context()).ProjectID, env)
			if err == nil {
				err = publishing.AuthorizeHistory(r.Context(), store.New(d.Pool), actorFrom(r.Context()), uuid.Nil)
			}
			if err == nil {
				for _, p := range items {
					if err = publishing.AuthorizeHistory(r.Context(), store.New(d.Pool), actorFrom(r.Context()), p.ID); err != nil {
						break
					}
				}
			}
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			respond(w, r, d, map[string]any{"items": items}, err)
		})
		r.Get("/publications/{id}", func(w http.ResponseWriter, r *http.Request) {
			id, ok := uuidParam(w, r, d, "id")
			if !ok {
				return
			}
			pub, err := publishing.GetPublication(r.Context(), store.New(d.Pool), actorFrom(r.Context()).ProjectID, id)
			if err == nil {
				err = publishing.AuthorizeHistory(r.Context(), store.New(d.Pool), actorFrom(r.Context()), id)
			}
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			respond(w, r, d, pub, err)
		})

		for _, kind := range []string{"entities", "assets"} {
			r.Get("/"+kind+"/{id}", func(w http.ResponseWriter, r *http.Request) {
				actor := actorFrom(r.Context())
				if err := commandbus.Require(actor, auth.ContentRead); err != nil {
					writeError(w, r, d.Log, err)
					return
				}
				id, ok := uuidParam(w, r, d, "id")
				if !ok {
					return
				}
				var cs *uuid.UUID
				if raw := r.URL.Query().Get("changesetId"); raw != "" {
					v, err := uuid.Parse(raw)
					if err != nil {
						writeError(w, r, d.Log, badParam("changesetId", "UUID"))
						return
					}
					cs = &v
				}
				value, err := changes.GetContent(r.Context(), store.New(d.Pool), actor.ProjectID, id, r.URL.Query().Get("environment"), cs)
				expected := "entity"
				if kind == "assets" {
					expected = "asset"
				}
				if err == nil && value.Kind != expected {
					err = commandbus.NewError(404, "NOT_FOUND", "Контент не найден", id.String())
				}
				respond(w, r, d, value, err)
			})
		}
		r.Get("/documents/{id}", func(w http.ResponseWriter, r *http.Request) {
			actor := actorFrom(r.Context())
			if err := commandbus.Require(actor, auth.DesignRead); err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			id, ok := uuidParam(w, r, d, "id")
			if !ok {
				return
			}
			if env := r.URL.Query().Get("environment"); env != "" {
				doc, err := publishing.GetPublishedDocument(r.Context(), store.New(d.Pool), actor.ProjectID, id, env)
				respond(w, r, d, doc, err)
				return
			}
			var cs *uuid.UUID
			if s := r.URL.Query().Get("changesetId"); s != "" {
				parsed, err := uuid.Parse(s)
				if err != nil {
					writeError(w, r, d.Log, badParam("changesetId", "UUID"))
					return
				}
				cs = &parsed
			}
			doc, err := changes.GetDocument(r.Context(), store.New(d.Pool), actor.ProjectID, id, cs)
			respond(w, r, d, doc, err)
		})
		r.Get("/documents/{id}/component-extract-plan", func(w http.ResponseWriter, r *http.Request) {
			id, ok := uuidParam(w, r, d, "id")
			if !ok {
				return
			}
			cs, err := uuid.Parse(r.URL.Query().Get("changesetId"))
			if err != nil {
				writeError(w, r, d.Log, badParam("changesetId", "UUID"))
				return
			}
			plan, err := changes.ExtractPlan(r.Context(), store.New(d.Pool), actorFrom(r.Context()), id, cs, r.URL.Query().Get("nodeId"), r.URL.Query().Get("environment"))
			respond(w, r, d, plan, err)
		})
	})

	r.With(authenticate(d)).Post("/mcp", mcpHandler(d))
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, d.Log, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Не найдено", "Ресурс не существует"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, d.Log, commandbus.NewError(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED",
			"Метод не поддерживается", fmt.Sprintf("Метод %s недоступен для %s", r.Method, r.URL.Path)))
	})
	return r
}

func respond(w http.ResponseWriter, r *http.Request, d Deps, v any, err error) {
	if err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func badParam(name, want string) error {
	return commandbus.NewError(http.StatusBadRequest, "PARAM_INVALID", "Некорректный параметр",
		fmt.Sprintf("Параметр %s: ожидается %s", name, want)).WithParams(map[string]any{"param": name})
}

func uuidParam(w http.ResponseWriter, r *http.Request, d Deps, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		writeError(w, r, d.Log, badParam(name, "UUID"))
		return uuid.Nil, false
	}
	return id, true
}

// commandEnvelope — тело POST /api/v1/commands/{name} (08-api.md §3.1).
type commandEnvelope struct {
	Payload json.RawMessage `json:"payload"`
	Reason  string          `json:"reason"`
}

func commandHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if note, ok := r.Context().Value(refusalKey{}).(*refusalNote); ok {
			// The Command Bus records refusals of commands it receives itself.
			note.dispatched = true
		}
		limit := int64(MaxCommandBody)
		if chi.URLParam(r, "name") == "upload-code-index" {
			limit = 2 * MaxCommandBody
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, r, d.Log, commandbus.NewError(http.StatusRequestEntityTooLarge, "LIMIT_EXCEEDED",
				"Слишком большой запрос", fmt.Sprintf("Тело команды больше %d байт", limit)))
			return
		}
		if err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		var env commandEnvelope
		if err := json.Unmarshal(body, &env); err != nil || len(env.Payload) == 0 {
			writeError(w, r, d.Log, commandbus.NewError(http.StatusBadRequest, "PAYLOAD_INVALID",
				"Некорректный запрос", `Ожидается JSON-объект вида {"payload": {...}, "reason": "..."}`))
			return
		}
		resp, err := d.Bus.Dispatch(r.Context(), actorFrom(r.Context()), commandbus.Request{
			Name:           chi.URLParam(r, "name"),
			IdempotencyKey: r.Header.Get("Idempotency-Key"),
			Payload:        env.Payload,
			Reason:         env.Reason,
		})
		if err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if resp.Replayed {
			w.Header().Set("Idempotent-Replayed", "true")
		}
		w.WriteHeader(resp.Status)
		_, _ = w.Write(resp.Body)
	}
}

type actorKey struct{}

func actorFrom(ctx context.Context) auth.Actor {
	a, _ := ctx.Value(actorKey{}).(auth.Actor)
	return a
}

// authenticate проверяет Bearer-токен и соответствие проекта заголовку X-CMS-Project.
func authenticate(d Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var actor auth.Actor
			var err error
			if r.Header.Get("Authorization") != "" || d.Identity == nil {
				actor, err = auth.Authenticate(r.Context(), store.New(d.Pool), r.Header.Get("Authorization"))
			} else {
				var info identity.Info
				info, err = d.Identity.Info(r.Context(), r)
				if err == nil && !info.HasAccess {
					err = identity.ErrAccess
				}
				if expected := r.Header.Get("X-CMS-Actor"); err == nil && expected != "" && expected != info.Actor.ID.String() {
					err = identity.ErrChanged
				}
				if err == nil && r.Method != "GET" && r.Method != "HEAD" {
					err = d.Identity.CheckCSRF(r, info)
				}
				actor = info.Actor
			}
			if note, ok := r.Context().Value(refusalKey{}).(*refusalNote); ok && actor.ID != uuid.Nil {
				note.actor = actor
			}
			if errors.Is(err, identity.ErrAccess) || errors.Is(err, identity.ErrCSRF) || errors.Is(err, identity.ErrChanged) {
				writeError(w, r, d.Log, commandbus.NewError(403, err.Error(), "Доступ не подтверждён", "Проверьте доступ к проекту и сессию"))
				return
			}
			if errors.Is(err, auth.ErrUnauthenticated) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="cms"`)
				writeError(w, r, d.Log, commandbus.NewError(http.StatusUnauthorized, "UNAUTHENTICATED",
					"Требуется аутентификация", "Передайте действующий токен в заголовке Authorization: Bearer"))
				return
			}
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			if project := r.Header.Get("X-CMS-Project"); project != actor.ProjectSlug {
				writeError(w, r, d.Log, commandbus.NewError(http.StatusForbidden, "PROJECT_MISMATCH",
					"Токен выдан для другого проекта",
					fmt.Sprintf("Заголовок X-CMS-Project (%q) не совпадает с проектом токена", project)))
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, actor)))
		})
	}
}

// limitRequests применяет лимиты 08 §7 к аутентифицированным запросам /api/v1 (STU-531, 534):
// людям — на все запросы и на команды, сервисам и агентским сессиям — на команды.
func limitRequests(d Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor := actorFrom(r.Context())
			command := r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/commands/")
			subject := "actor:" + actor.ID.String()
			switch {
			case actor.Kind == auth.ActorHuman:
				if limited(w, r, d, subject, "requests", d.Limits.HumanRequests) || command && limited(w, r, d, subject, "commands", d.Limits.HumanCommands) {
					return
				}
			case actor.Kind == auth.ActorAgent && command:
				if actor.AgentSessionID != nil {
					subject = "agent-session:" + actor.AgentSessionID.String()
				}
				if limited(w, r, d, subject, "commands", d.Limits.AgentCommands) {
					return
				}
			case actor.Kind == auth.ActorService && command:
				if limited(w, r, d, subject, "commands", d.Limits.ServiceCommands) {
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			log.InfoContext(r.Context(), "http request",
				"method", r.Method, "path", r.URL.Path, "status", ww.Status(),
				"bytes", ww.BytesWritten(), "duration", time.Since(start),
				"requestId", middleware.GetReqID(r.Context()))
		})
	}
}

func recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					if p == http.ErrAbortHandler {
						panic(p)
					}
					writeError(w, r, log, fmt.Errorf("panic: %v", p))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

type refusalKey struct{}

// refusalNote carries what auditRefusals needs from inner handlers: the identified
// actor, even when access was then refused, and whether the bus took the command.
type refusalNote struct {
	actor      auth.Actor
	dispatched bool
}

// auditRefusals — STU-505: 401 and 403 of the Query API in the SEC-005 journal; the
// Command Bus records refusals of commands itself. Route patterns, codes and the
// method only, never query values or bodies.
func auditRefusals(d Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			note := &refusalNote{}
			sw := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), refusalKey{}, note)))
			status := sw.Status()
			// Отказ по лимиту пишется для агентов и сервисов (STU-532); людей ограничивает Studio.
			throttled := status == http.StatusTooManyRequests && (note.actor.Kind == auth.ActorAgent || note.actor.Kind == auth.ActorService)
			if status != http.StatusUnauthorized && status != http.StatusForbidden && !throttled || note.dispatched {
				return
			}
			ctx := context.WithoutCancel(r.Context())
			var project, id *uuid.UUID
			if note.actor.ID != uuid.Nil {
				id = &note.actor.ID
			}
			if note.actor.ProjectID != uuid.Nil {
				project = &note.actor.ProjectID
			} else if p, err := store.New(d.Pool).GetProjectBySlug(ctx, r.Header.Get("X-CMS-Project")); err == nil {
				project = &p.ID
			}
			_ = securityevents.Record(ctx, d.Pool, securityevents.Event{ProjectID: project, ActorID: id, Type: "access.denied",
				Data: map[string]any{"method": r.Method, "route": r.URL.Path, "status": status}})
		})
	}
}
