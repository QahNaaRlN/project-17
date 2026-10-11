package httpapi

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/qahnaarln/project-17/apps/server/internal/imagedelivery"
)

func imageModelHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if d.Images == nil {
			writeError(w, r, d.Log, imagedelivery.Unavailable())
			return
		}
		id, ok := uuidParam(w, r, d, "id")
		if !ok {
			return
		}
		q, err := parseImageQuery(r)
		if err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		model, err := d.Images.Mint(r.Context(), accessFrom(r.Context()), id, q)
		respond(w, r, d, model, err)
	}
}
func parseImageQuery(r *http.Request) (imagedelivery.Options, error) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return imagedelivery.Options{}, badParam("image", "malformed query")
	}
	q, err := imagedelivery.ParseOptions(values)
	if err != nil {
		return q, badParam("image", err.Error())
	}
	return q, nil
}
func imageHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if d.Images == nil {
			writeError(w, r, d.Log, imagedelivery.Unavailable())
			return
		}
		image, err := d.Images.Fetch(r.Context(), r.URL)
		if err != nil {
			writeError(w, r, d.Log, err)
			return
		}
		h := w.Header()
		h.Set("Content-Type", image.MIME)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("ETag", image.ETag)
		if !image.Draft && image.MaxAge > 0 {
			h.Set("Cache-Control", fmt.Sprintf("public, max-age=0, s-maxage=%d, must-revalidate", image.MaxAge))
			h.Set("Surrogate-Key", image.SurrogateKey)
		}
		if ifNoneMatch(r.Header.Get("If-None-Match"), image.ETag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		h.Set("Content-Length", fmt.Sprint(len(image.Body)))
		_, _ = w.Write(image.Body)
	}
}
