package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

// artifactUploadHandler — STU-573: the check runner stores a screenshot or report of the
// check it holds the lease of (header X-CMS-Check-Lease).
func artifactUploadHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		check, ok := uuidParam(w, r, d, "checkId")
		if !ok {
			return
		}
		lease, err := uuid.Parse(r.Header.Get("X-CMS-Check-Lease"))
		if err != nil {
			writeError(w, r, d.Log, badParam("X-CMS-Check-Lease", "UUID аренды проверки"))
			return
		}
		contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, workflow.MaxArtifactSize+1))
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			err = commandbus.NewError(http.StatusRequestEntityTooLarge, "CHECK_ARTIFACTS_LIMIT", "Артефакт больше 8 МиБ", "")
		}
		if err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		err = workflow.PutArtifact(r.Context(), d.Pool, d.Artifacts, actorFrom(r.Context()), check, lease, chi.URLParam(r, "name"), contentType, body)
		if err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// artifactReadHandler — STU-575: whoever reads the Change Set reads its check artifacts;
// the bucket stays closed and an HTML report runs in a script-less sandbox.
func artifactReadHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cs, ok := uuidParam(w, r, d, "id")
		if !ok {
			return
		}
		check, ok := uuidParam(w, r, d, "checkId")
		if !ok {
			return
		}
		tx, err := d.Pool.BeginTx(r.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		defer tx.Rollback(r.Context()) //nolint:errcheck
		q := store.New(tx)
		actor := actorFrom(r.Context())
		if err := authorizeWorkRead(r.Context(), q, actor, cs); err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		a, err := workflow.OpenArtifact(r.Context(), q, d.Artifacts, actor.ProjectID, cs, check, chi.URLParam(r, "name"))
		if err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		defer a.Body.Close()
		h := w.Header()
		h.Set("Content-Type", a.ContentType)
		h.Set("Content-Length", strconv.FormatInt(a.Size, 10))
		h.Set("Cache-Control", "private, max-age=3600")
		h.Set("X-Content-Type-Options", "nosniff")
		if a.ContentType == "text/html" {
			// The Lighthouse report renders with scripts: it is downloaded, never shown here.
			h.Set("Content-Security-Policy", "sandbox")
			h.Set("Content-Disposition", `attachment; filename="`+chi.URLParam(r, "name")+`"`)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, a.Body)
	}
}
