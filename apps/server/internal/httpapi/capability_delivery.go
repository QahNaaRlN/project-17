package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/capabilities"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

func capabilityDeliveryRoutes(router chi.Router, d Deps) {
	s := d.CapabilityDelivery
	if s == nil {
		s = &capabilities.DeliveryService{Pool: d.Pool}
	}
	for _, path := range []string{"/capability-delivery-endpoints", "/capability-delivery-target", "/capability-requests/{id}/delivery-preview", "/capability-requests/{id}/deliveries", "/capability-deliveries/{id}"} {
		router.Get(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "private, no-store")
			allowed := map[string]bool{"environment": true}
			if strings.HasSuffix(r.URL.Path, "/deliveries") {
				allowed["limit"] = true
				allowed["cursor"] = true
			}
			for key, values := range r.URL.Query() {
				if !allowed[key] || len(values) != 1 || len(values[0]) > 4096 {
					writeError(w, r, d.Log, commandbus.Validation(map[string]string{"query": "invalid filter"}))
					return
				}
			}
			tx, err := d.Pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				writeError(w, r, d.Log, err)
				return
			}
			defer func() { _ = tx.Rollback(r.Context()) }()
			q := store.New(tx)
			a := actorFrom(r.Context())
			environment := r.URL.Query().Get("environment")
			var result map[string]any
			if chi.URLParam(r, "id") == "" {
				if environment == "" {
					err = commandbus.Validation(map[string]string{"environment": "required"})
				} else if strings.HasSuffix(r.URL.Path, "/capability-delivery-endpoints") {
					result, err = s.DeliveryEndpoints(r.Context(), q, a, environment)
				} else {
					result, err = s.DeliveryTarget(r.Context(), q, a, environment)
				}
			} else {
				id, ok := uuidParam(w, r, d, "id")
				if !ok {
					return
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "/delivery-preview"):
					result, err = s.DeliveryPreview(r.Context(), q, a, id)
				case strings.HasSuffix(r.URL.Path, "/deliveries"):
					limit := 0
					if value := r.URL.Query().Get("limit"); value != "" {
						limit, err = strconv.Atoi(value)
					}
					if err == nil {
						result, err = s.DeliveryList(r.Context(), q, a, id, limit, r.URL.Query().Get("cursor"))
					} else {
						err = commandbus.Validation(map[string]string{"limit": "1…100"})
					}
				default:
					result, err = s.DeliveryGet(r.Context(), q, a, id)
				}
				if err == nil && environment != "" {
					requestID := id
					if strings.Contains(r.URL.Path, "/capability-deliveries/") {
						delivery, e := q.GetCapabilityDelivery(r.Context(), store.GetCapabilityDeliveryParams{ProjectID: a.ProjectID, ID: id})
						if e != nil {
							err = e
						} else {
							requestID = delivery.RequestID
						}
					}
					if err == nil {
						request, e := q.GetCapabilityRequest(r.Context(), store.GetCapabilityRequestParams{ProjectID: a.ProjectID, ID: requestID})
						if e != nil {
							err = e
						} else if request.Environment != environment {
							err = commandbus.NewError(404, "CAPABILITY_DELIVERY_NOT_FOUND", "Передача не найдена", "Окружение не совпадает")
						}
					}
				}
			}
			respond(w, r, d, result, err)
		})
	}
}
