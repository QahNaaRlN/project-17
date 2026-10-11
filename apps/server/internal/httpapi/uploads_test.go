package httpapi_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"net/http"
	"testing"
)

// CNT-040/API-012: upload status is private and validates IDs before accessing data.
func TestAssetUploadStatusHTTP(t *testing.T) {
	e := setup(t)
	a, err := auth.Authenticate(context.Background(), store.New(e.pool), "Bearer "+e.token)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	_, err = e.pool.Exec(context.Background(), `INSERT INTO asset_uploads(id,project_id,actor_id,filename,mime_type,size_bytes,source_sha256,storage_key,expires_at) VALUES($1,$2,$3,'x','image/png',1,$4,$5,now()+interval '15 minutes')`, id, a.ProjectID, a.ID, make([]byte, 32), id.String())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		id   string
		want int
	}{{id.String(), 200}, {uuid.NewString(), 404}, {"bad", 400}} {
		resp, body := e.do(t, http.MethodGet, "/api/v1/asset-uploads/"+test.id, "", map[string]string{"Authorization": "Bearer " + e.token, "X-CMS-Project": "store"})
		if resp.StatusCode != test.want {
			t.Fatal(resp.StatusCode, body)
		}
	}
	resp, _ := e.do(t, http.MethodGet, "/api/v1/asset-uploads/"+id.String(), "", nil)
	if resp.StatusCode != 401 {
		t.Fatal(resp.Status)
	}
}
