// Local import fixture only: the command catalogue does not yet expose document.delete.
package main

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/url"
	"os"
	"strings"
)

func main() {
	u, err := url.Parse(os.Getenv("STUDIO_REVIEW_DATABASE_URL"))
	if err != nil || u.Hostname() != "127.0.0.1" || !strings.HasPrefix(u.Path, "/studio_work_queue_") || len(os.Args) != 3 {
		panic("isolated local work queue fixture only")
	}
	cs, err := uuid.Parse(os.Args[1])
	if err != nil {
		panic("invalid CS")
	}
	obj, err := uuid.Parse(os.Args[2])
	if err != nil {
		panic("invalid object")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		panic("fixture connection failed")
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		panic("fixture transaction failed")
	}
	defer tx.Rollback(ctx)
	var project, owner, base uuid.UUID
	err = tx.QueryRow(ctx, `SELECT c.project_id,c.owner_id,o.head_version_id FROM changesets c JOIN projects p ON p.id=c.project_id AND p.slug='studio-work-review' JOIN objects o ON o.project_id=c.project_id AND o.id=$2 AND o.kind='document' AND o.doc_kind='page' WHERE c.id=$1 AND c.kind='standard' AND c.state='open' AND c.seq=0`, cs, obj).Scan(&project, &owner, &base)
	if err != nil {
		panic("fixture project/CS/object mismatch")
	}
	version := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO object_versions(id,project_id,object_id,state,ir_version,path,body,body_hash,created_by,deleted) SELECT $1,project_id,object_id,'working',ir_version,path,body,body_hash,$2,true FROM object_versions WHERE id=$3`, version, owner, base)
	if err != nil {
		panic("fixture version failed")
	}
	_, err = tx.Exec(ctx, `INSERT INTO changeset_objects(changeset_id,object_id,base_version_id,working_version_id) VALUES($1,$2,$3,$4)`, cs, obj, base, version)
	if err != nil {
		panic("fixture membership failed")
	}
	if tx.Commit(ctx) != nil {
		panic("fixture commit failed")
	}
}
