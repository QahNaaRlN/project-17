package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/platform/telemetry"
)

// OPS-023, STU-603…604: runtime diagnostics from the application server. Nothing is stored;
// a counter grows by code, source and environment.
const (
	maxTelemetryBody    = 16 << 10
	maxTelemetryEntries = 50
	maxTelemetryCount   = 10_000
	telemetryLimit      = 10 // requests per second per delivery key
)

var (
	diagnosticCode = regexp.MustCompile(`^[A-Z0-9_]{1,64}$`)
	// knownDiagnostics — the codes of @cms/runtime, @cms/core and @cms/react (STU-601):
	// any other code is counted as OTHER so that labels stay bounded.
	knownDiagnostics      = map[string]bool{}
	diagnosticSources     = map[string]bool{"render": true, "source": true, "action": true}
	runtimeDiagnostics, _ = telemetry.Meter().Int64Counter("cms.runtime.diagnostics",
		metric.WithDescription("Runtime diagnostics reported by application servers"))
)

func init() {
	for _, code := range []string{"CMS_ACTION_ARGS_INVALID", "CMS_ACTION_CHAIN_INVALID", "CMS_ACTION_FAILED", "CMS_ACTION_HANDLER_MISSING", "CMS_ACTION_PENDING_CAPABILITY", "CMS_ACTION_SOURCE_UNAVAILABLE", "CMS_ACTION_TARGET_INVALID", "CMS_ACTION_TARGET_MISSING", "CMS_ACTION_UNKNOWN", "CMS_ACTION_UNSUPPORTED", "CMS_BREAKPOINT_INVALID", "CMS_CHILDREN_INVALID", "CMS_COMPONENT_CYCLE", "CMS_COMPONENT_DESIGN_UNSUPPORTED", "CMS_COMPONENT_LOAD_LIMIT", "CMS_COMPONENT_MISSING", "CMS_COMPONENT_PENDING_CAPABILITY", "CMS_COMPONENT_VERSION_UNSUPPORTED", "CMS_DATA_VALUE_INVALID", "CMS_DESIGN_INVALID", "CMS_DESIGN_UNSUPPORTED", "CMS_EVENT_UNKNOWN", "CMS_EXPANSION_LIMIT", "CMS_FORMAT_INVALID", "CMS_FORMAT_UNKNOWN", "CMS_IMAGE_INVALID", "CMS_IMAGE_UNAVAILABLE", "CMS_INPUT_MISSING", "CMS_NODE_CYCLE", "CMS_NODE_INVALID", "CMS_NODE_MISSING", "CMS_NODE_UNKNOWN", "CMS_OPERAND_INVALID", "CMS_PREDICATE_LIMIT", "CMS_PREDICATE_UNKNOWN", "CMS_PREVIEW_DOCUMENT_INVALID", "CMS_PREVIEW_INIT_INVALID", "CMS_PREVIEW_INIT_REQUIRED", "CMS_PREVIEW_MANIFEST_INVALID", "CMS_PREVIEW_MESSAGE_INVALID", "CMS_PREVIEW_MESSAGE_UNKNOWN", "CMS_PREVIEW_NAVIGATE_UNAVAILABLE", "CMS_PREVIEW_ORIGIN_INVALID", "CMS_PREVIEW_PATCH_INVALID", "CMS_PREVIEW_PATH_INVALID", "CMS_PREVIEW_PROTOCOL_UNSUPPORTED", "CMS_PREVIEW_REFRESH_UNAVAILABLE", "CMS_PREVIEW_REQUEST_INVALID", "CMS_PREVIEW_TOKEN_EXPIRED", "CMS_PREVIEW_TOKEN_INVALID", "CMS_PREVIEW_UPDATE_FAILED", "CMS_PREVIEW_VERIFY_FAILED", "CMS_PROP_CONFLICT", "CMS_PROP_INVALID", "CMS_REPEAT_INVALID", "CMS_REPEAT_KEY_DUPLICATE", "CMS_REPEAT_KEY_INVALID", "CMS_RESPONSIVE_INVALID", "CMS_RESPONSIVE_UNSUPPORTED", "CMS_ROUTE_UNRESOLVED", "CMS_SLOT_INVALID", "CMS_SLOT_UNSUPPORTED", "CMS_SOURCE_ABORTED", "CMS_SOURCE_CURSOR_INVALID", "CMS_SOURCE_FAILED", "CMS_SOURCE_KEY_INVALID", "CMS_SOURCE_LIMIT", "CMS_SOURCE_PARAMS_INVALID", "CMS_SOURCE_PENDING", "CMS_SOURCE_RESULT_INVALID", "CMS_SOURCE_TIMEOUT", "CMS_SOURCE_TIMEOUT_INVALID", "CMS_SOURCE_UNKNOWN", "CMS_TEMPLATE_INVALID", "CMS_VALUE_INVALID"} {
		knownDiagnostics[code] = true
	}
}

type telemetryReport struct {
	Diagnostics []struct {
		Code   string `json:"code"`
		Count  int    `json:"count"`
		Source string `json:"source"`
	} `json:"diagnostics"`
}

func telemetryHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := accessFrom(r.Context())
		if a.Draft {
			writeError(w, r, d.Log, commandbus.NewError(http.StatusForbidden, "FORBIDDEN", "Телеметрия принимается по ключу доставки",
				"Отправляйте диагностику с серверной части приложения по ключу cms_pub_…"))
			return
		}
		if limited(w, r, d, a.Subject, "telemetry", telemetryLimit) {
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxTelemetryBody))
		var report telemetryReport
		if err != nil || json.Unmarshal(raw, &report) != nil || len(report.Diagnostics) == 0 || len(report.Diagnostics) > maxTelemetryEntries {
			writeError(w, r, d.Log, commandbus.NewError(http.StatusBadRequest, "PAYLOAD_INVALID", "Некорректная телеметрия",
				"Ожидается {diagnostics: [{code, count, source}]}: 1…50 записей, до 16 KiB"))
			return
		}
		for _, e := range report.Diagnostics {
			if !diagnosticCode.MatchString(e.Code) || e.Count < 1 || e.Count > maxTelemetryCount || (e.Source != "" && !diagnosticSources[e.Source]) {
				writeError(w, r, d.Log, commandbus.NewError(http.StatusBadRequest, "PAYLOAD_INVALID", "Некорректная телеметрия",
					"code — [A-Z0-9_]{1,64}, count — 1…10000, source — render, source или action"))
				return
			}
		}
		for _, e := range report.Diagnostics {
			code, source := e.Code, e.Source
			if !knownDiagnostics[code] {
				code = "OTHER"
			}
			if source == "" {
				source = "render"
			}
			runtimeDiagnostics.Add(r.Context(), int64(e.Count), metric.WithAttributes(attribute.String("cms.diagnostic.code", code),
				attribute.String("cms.diagnostic.source", source), attribute.String("deployment.environment", a.Environment)))
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
