// Read-only local acceptance verification; never connects to production.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"io"
	"net/url"
	"os"
	"strings"
)

func main() {
	u, err := url.Parse(os.Getenv("STUDIO_REVIEW_DATABASE_URL"))
	if err != nil || u.Hostname() != "127.0.0.1" || !strings.HasPrefix(u.Path, "/studio_work_queue_") || len(os.Args) != 2 {
		panic("isolated local verification only")
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	var report map[string]any
	if json.Unmarshal(raw, &report) != nil || report["project"] != "studio-work-review" {
		panic("wrong acceptance report")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		panic("local database unavailable")
	}
	defer conn.Close(ctx)
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		panic("read-only transaction failed")
	}
	defer tx.Rollback(ctx)
	for _, w := range report["works"].([]any) {
		work := w.(map[string]any)
		var state string
		var count, pages, committed int
		err = tx.QueryRow(ctx, `SELECT c.state,count(*)::int,count(*) FILTER(WHERE o.kind='document')::int,count(*) FILTER(WHERE v.state='committed' AND o.head_version_id=v.id AND pp.version_id=v.id)::int FROM changesets c JOIN projects p ON p.id=c.project_id AND p.slug='studio-work-review' JOIN changeset_objects co ON co.changeset_id=c.id JOIN objects o ON o.id=co.object_id JOIN object_versions v ON v.id=co.working_version_id JOIN environments e ON e.project_id=c.project_id AND e.name='staging' LEFT JOIN published_pointers pp ON pp.environment_id=e.id AND pp.object_id=o.id WHERE c.id=$1 GROUP BY c.state`, work["id"]).Scan(&state, &count, &pages, &committed)
		if err != nil || state != "merged" || count != 1 || committed != 1 || work["kind"] != "document" && pages != 0 {
			panic("committed/published/page-count invariant failed")
		}
		work["committedAndPublished"] = true
		if work["kind"] != "document" {
			work["noPageCreated"] = true
		}
	}
	asset := report["asset"].(map[string]any)
	var project, key string
	err = tx.QueryRow(ctx, `SELECT o.project_id::text,f.storage_key FROM objects o JOIN asset_files f ON f.project_id=o.project_id AND encode(f.sha256,'hex')=$2 AND f.status='ready' WHERE o.id=$1`, asset["id"], asset["fileHash"]).Scan(&project, &key)
	if err != nil || key != "projects/"+project+"/assets/"+asset["fileHash"].(string) {
		panic("ready file binding failed")
	}
	client, err := minio.New("127.0.0.1:18217", &minio.Options{Creds: credentials.NewStaticV4("test-access", "test-secret", ""), Secure: false})
	if err != nil {
		panic(err)
	}
	object, err := client.GetObject(ctx, "assets", key, minio.GetObjectOptions{})
	if err != nil {
		panic("ready object unavailable")
	}
	defer object.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, object)
	if err != nil || n == 0 || hex.EncodeToString(hash.Sum(nil)) != asset["fileHash"] {
		panic("ready object hash mismatch")
	}
	asset["fileAvailable"] = true
	asset["downloadVerifiedBytes"] = n
	if tx.Commit(ctx) != nil {
		panic("verification transaction failed")
	}
	out, _ := json.MarshalIndent(report, "", "  ")
	if os.WriteFile(os.Args[1], out, 0600) != nil {
		panic("report write failed")
	}
	fmt.Println("PASS: immutable committed versions and published pointers; no pages in entity/asset CS; ready S3 object downloaded with exact hash.")
}
