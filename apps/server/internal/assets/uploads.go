package assets

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/jobs"
	"github.com/riverqueue/river"
)

type Service struct {
	Pool    *pgxpool.Pool
	Storage Storage
}
type Create struct {
	Filename string `json:"filename"`
	MIME     string `json:"mimeType"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}
type Ref struct {
	AssetID uuid.UUID `json:"assetId"`
}
type Upload struct {
	AssetID   uuid.UUID `json:"assetId"`
	Status    string    `json:"status"`
	FileHash  *string   `json:"fileHash,omitempty"`
	ErrorCode *string   `json:"errorCode,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
	UploadURL string    `json:"uploadUrl,omitempty"`
}

func Limit(mime string) int64 {
	switch mime {
	case "image/jpeg", "image/png", "image/webp", "image/avif", "image/gif", "image/svg+xml":
		return 25 << 20
	case "video/mp4", "video/webm":
		return 500 << 20
	case "application/pdf":
		return 100 << 20
	}
	return 0
}
func validate(p Create) error {
	hash, err := hex.DecodeString(p.SHA256)
	if strings.TrimSpace(p.Filename) == "" || len(p.Filename) > 255 || strings.ContainsAny(p.Filename, "\x00\r\n") || err != nil || len(hash) != 32 || strings.ToLower(p.SHA256) != p.SHA256 || p.Size <= 0 || Limit(p.MIME) == 0 || p.Size > Limit(p.MIME) {
		return commandbus.Validation(map[string]string{"upload": "filename, sha256, MIME или размер недопустимы"})
	}
	return nil
}
func (s *Service) Register(bus *commandbus.Bus) {
	commandbus.Register(bus, commandbus.Command[Create, Upload]{Name: "create-asset-upload", Right: auth.AssetWrite, Validate: validate, Handle: s.create})
	commandbus.Register(bus, commandbus.Command[Ref, Upload]{Name: "complete-asset-upload", Right: auth.AssetWrite, Handle: s.complete})
}
func unavailable() error {
	return commandbus.NewError(503, "ASSET_STORAGE_UNAVAILABLE", "Хранилище ассетов недоступно", "Настройте CMS_S3_*")
}
func missing() error {
	return commandbus.NewError(404, "NOT_FOUND", "Загрузка не найдена", "Загрузка недоступна актору проекта")
}
func (s *Service) create(ctx context.Context, tx pgx.Tx, a auth.Actor, p Create) (Upload, error) {
	if s.Storage == nil {
		return Upload{}, unavailable()
	}
	var allowed bool
	if err := tx.QueryRow(ctx, `SELECT CASE WHEN settings#>'{assets,mimeTypes}' IS NULL THEN true ELSE settings#>'{assets,mimeTypes}' @> jsonb_build_array($2::text) END FROM projects WHERE id=$1`, a.ProjectID, p.MIME).Scan(&allowed); err != nil {
		return Upload{}, err
	}
	if !allowed {
		return Upload{}, commandbus.Validation(map[string]string{"mimeType": "тип запрещён настройками проекта"})
	}
	id := uuid.Must(uuid.NewV7())
	key := "uploads/" + a.ProjectID.String() + "/" + id.String()
	url, err := s.Storage.UploadURL(ctx, key)
	if err != nil {
		return Upload{}, err
	}
	hash, _ := hex.DecodeString(p.SHA256)
	var expires time.Time
	err = tx.QueryRow(ctx, `INSERT INTO asset_uploads(id,project_id,actor_id,filename,mime_type,size_bytes,source_sha256,storage_key,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,clock_timestamp()+interval '15 minutes') RETURNING expires_at`, id, a.ProjectID, a.ID, p.Filename, p.MIME, p.Size, hash, key).Scan(&expires)
	return Upload{AssetID: id, Status: "pending", ExpiresAt: expires, UploadURL: url}, err
}

type row interface{ Scan(...any) error }

func scan(r row) (Upload, error) {
	var u Upload
	var hash []byte
	err := r.Scan(&u.AssetID, &u.Status, &hash, &u.ErrorCode, &u.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, missing()
	}
	if hash != nil {
		x := hex.EncodeToString(hash)
		u.FileHash = &x
	}
	return u, err
}

const selectUpload = `SELECT id,status,file_sha256,error_code,expires_at FROM asset_uploads WHERE id=$1 AND project_id=$2 AND actor_id=$3`

func (s *Service) Get(ctx context.Context, a auth.Actor, id uuid.UUID) (Upload, error) {
	if err := commandbus.Require(a, auth.AssetWrite); err != nil {
		return Upload{}, err
	}
	return scan(s.Pool.QueryRow(ctx, selectUpload, id, a.ProjectID, a.ID))
}
func (s *Service) complete(ctx context.Context, tx pgx.Tx, a auth.Actor, p Ref) (Upload, error) {
	if s.Storage == nil {
		return Upload{}, unavailable()
	}
	u, err := scan(tx.QueryRow(ctx, selectUpload+" FOR UPDATE", p.AssetID, a.ProjectID, a.ID))
	if err != nil {
		return u, err
	}
	if u.Status != "pending" {
		return u, nil
	}
	var expired bool
	if err := tx.QueryRow(ctx, `SELECT expires_at <= clock_timestamp() FROM asset_uploads WHERE id=$1`, u.AssetID).Scan(&expired); err != nil {
		return u, err
	}
	if expired {
		return u, commandbus.NewError(409, "ASSET_UPLOAD_EXPIRED", "Срок загрузки истёк", "Создайте новую загрузку")
	}
	if _, err = tx.Exec(ctx, `UPDATE asset_uploads SET status='processing' WHERE id=$1`, u.AssetID); err != nil {
		return u, err
	}
	if err = jobs.Enqueue(ctx, tx, ProcessArgs{UploadID: u.AssetID, ProjectID: a.ProjectID}); err != nil {
		return u, err
	}
	u.Status = "processing"
	return u, nil
}

type ProcessArgs struct {
	UploadID  uuid.UUID `json:"uploadId"`
	ProjectID uuid.UUID `json:"projectId"`
}

func (ProcessArgs) Kind() string { return "asset_process" }

type Worker struct {
	river.WorkerDefaults[ProcessArgs]
	Service *Service
}

func (w *Worker) Work(ctx context.Context, j *river.Job[ProcessArgs]) error {
	return w.Service.Process(ctx, j.Args)
}
