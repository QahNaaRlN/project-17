package httpapi

import (
	"context"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

// STU-155: an unavailable read transaction is an error, never an empty comparison.
func TestDiffUnavailableReadTransaction(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://local:local@127.0.0.1:1/local?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	router := chi.NewRouter()
	reviewDiffRoutes(router, Deps{Pool: pool, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	for _, route := range []string{"/changesets/0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c22/diff?environment=staging", "/changesets/0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c22/diff/0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11?environment=staging&baseline=base"} {
		result := httptest.NewRecorder()
		router.ServeHTTP(result, httptest.NewRequest("GET", route, nil))
		if result.Code != 500 || strings.Contains(result.Body.String(), "complete") || strings.Contains(result.Body.String(), "postgres://") {
			t.Fatal(result.Code, result.Body.String())
		}
	}
}
