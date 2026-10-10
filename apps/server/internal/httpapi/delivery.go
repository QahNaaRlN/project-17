package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/delivery"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// PublishedCacheControl — кэширование опубликованных ответов (API-033).
const PublishedCacheControl = "public, s-maxage=300, stale-while-revalidate=60, stale-if-error=86400"

// MaxDeliveryPath — предел длины пути в /page?path=.
const MaxDeliveryPath = 2048

type accessKey struct{}

func accessFrom(ctx context.Context) delivery.Access {
	a, _ := ctx.Value(accessKey{}).(delivery.Access)
	return a
}

// deliveryRoutes — Delivery API (08 §5): /delivery/v1/{project}/{env}/….
func deliveryRoutes(d Deps) func(chi.Router) {
	return func(r chi.Router) {
		r.Use(authenticateDelivery(d))
		r.Get("/page", func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Query().Get("path")
			if !strings.HasPrefix(path, "/") || len(path) > MaxDeliveryPath {
				writeError(w, r, d.Log, badParam("path", fmt.Sprintf("путь страницы, начинающийся с «/», до %d символов", MaxDeliveryPath)))
				return
			}
			if path != "/" {
				path = strings.TrimSuffix(path, "/")
			}
			page, err := delivery.GetPage(r.Context(), store.New(d.Pool), accessFrom(r.Context()), path)
			deliver(w, r, d, page, err, page.Page.ObjectID.String())
		})
		r.Get("/document/{id}", func(w http.ResponseWriter, r *http.Request) {
			id, ok := uuidParam(w, r, d, "id")
			if !ok {
				return
			}
			doc, err := delivery.GetDocument(r.Context(), store.New(d.Pool), accessFrom(r.Context()), id)
			deliver(w, r, d, doc, err, id.String())
		})
		r.Get("/routes", func(w http.ResponseWriter, r *http.Request) {
			routes, err := delivery.GetRoutes(r.Context(), store.New(d.Pool), accessFrom(r.Context()))
			keys := []string{"routes"}
			for _, rt := range routes {
				keys = append(keys, rt.ObjectID.String())
			}
			deliver(w, r, d, map[string]any{"items": routes}, err, keys...)
		})
	}
}

// deliver отдаёт опубликованный ответ с ETag, Cache-Control и Surrogate-Key (API-033)
// и отвечает 304 на совпавший If-None-Match.
func deliver(w http.ResponseWriter, r *http.Request, d Deps, v any, err error, surrogateKeys ...string) {
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		writeError(w, r, d.Log, err)
		return
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		writeError(w, r, d.Log, err)
		return
	}
	sum := sha256.Sum256(buf.Bytes())
	etag := `"` + base64.RawURLEncoding.EncodeToString(sum[:18]) + `"`
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", PublishedCacheControl)
	h.Set("Surrogate-Key", strings.Join(surrogateKeys, " "))
	if ifNoneMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "application/json")
	_, _ = w.Write(buf.Bytes())
}

func ifNoneMatch(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		c := strings.TrimSpace(candidate)
		if c == "*" || strings.TrimPrefix(c, "W/") == etag {
			return true
		}
	}
	return false
}

// authenticateDelivery проверяет ключ доставки и его соответствие проекту и окружению пути.
func authenticateDelivery(d Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			a, err := delivery.Authenticate(r.Context(), store.New(d.Pool), r.Header.Get("Authorization"))
			if errors.Is(err, delivery.ErrUnauthenticated) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="cms-delivery"`)
				writeError(w, r, d.Log, commandbus.NewError(http.StatusUnauthorized, "UNAUTHENTICATED",
					"Требуется ключ доставки", "Передайте действующий ключ cms_pub_… в заголовке Authorization: Bearer"))
				return
			}
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			if chi.URLParam(r, "project") != a.ProjectSlug || chi.URLParam(r, "env") != a.Environment {
				writeError(w, r, d.Log, commandbus.NewError(http.StatusForbidden, "FORBIDDEN", "Ключ выдан для другого окружения",
					fmt.Sprintf("Ключ действует для %s/%s", a.ProjectSlug, a.Environment)))
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), accessKey{}, a)))
		})
	}
}
