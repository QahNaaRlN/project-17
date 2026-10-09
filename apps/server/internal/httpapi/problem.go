package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
)

// problem — тело ошибки по RFC 9457 (08-api.md §6).
type problem struct {
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Status    int            `json:"status"`
	Code      string         `json:"code"`
	Detail    string         `json:"detail,omitempty"`
	Instance  string         `json:"instance"`
	RequestID string         `json:"requestId,omitempty"`
	Params    map[string]any `json:"params,omitempty"`
}

const problemTypeBase = "https://cms.dev/errors/"

// writeError отдаёт ошибку как application/problem+json. Ошибки, не являющиеся
// *commandbus.Error, логируются и скрываются за 500 INTERNAL.
func writeError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	var cerr *commandbus.Error
	if !errors.As(err, &cerr) {
		log.ErrorContext(r.Context(), "request failed", "error", err, "path", r.URL.Path,
			"requestId", middleware.GetReqID(r.Context()))
		cerr = commandbus.NewError(http.StatusInternalServerError, "INTERNAL", "Внутренняя ошибка",
			"Запрос не выполнен; подробности — в журнале сервера по requestId")
	}
	p := problem{
		Type:      problemTypeBase + strings.ToLower(strings.ReplaceAll(cerr.Code, "_", "-")),
		Title:     cerr.Title,
		Status:    cerr.Status,
		Code:      cerr.Code,
		Detail:    cerr.Detail,
		Instance:  r.URL.Path,
		RequestID: middleware.GetReqID(r.Context()),
		Params:    cerr.Params,
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(cerr.Status)
	_ = json.NewEncoder(w).Encode(p)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
