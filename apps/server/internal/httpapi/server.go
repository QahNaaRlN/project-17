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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// MaxCommandBody — максимальный размер тела команды.
const MaxCommandBody = 1 << 20

// Deps — зависимости HTTP-слоя.
type Deps struct {
	Pool    *pgxpool.Pool
	Bus     *commandbus.Bus
	Log     *slog.Logger
	Version string
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

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(authenticate(d))
		r.Post("/commands/{name}", commandHandler(d))
		r.Get("/environments", func(w http.ResponseWriter, r *http.Request) {
			actor := actorFrom(r.Context())
			envs, err := projects.ListEnvironments(r.Context(), store.New(d.Pool), actor.ProjectID)
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": envs})
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, d.Log, commandbus.NewError(http.StatusNotFound, "NOT_FOUND", "Не найдено", "Ресурс не существует"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, d.Log, commandbus.NewError(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED",
			"Метод не поддерживается", fmt.Sprintf("Метод %s недоступен для %s", r.Method, r.URL.Path)))
	})
	return r
}

// commandEnvelope — тело POST /api/v1/commands/{name} (08-api.md §3.1).
type commandEnvelope struct {
	Payload json.RawMessage `json:"payload"`
	Reason  string          `json:"reason"`
}

func commandHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxCommandBody))
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, r, d.Log, commandbus.NewError(http.StatusRequestEntityTooLarge, "LIMIT_EXCEEDED",
				"Слишком большой запрос", fmt.Sprintf("Тело команды больше %d байт", MaxCommandBody)))
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
			actor, err := auth.Authenticate(r.Context(), store.New(d.Pool), r.Header.Get("Authorization"))
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
