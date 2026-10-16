package manifestregistry

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/codeindex"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

type UploadIndex struct {
	ManifestHash string          `json:"manifestHash"`
	IndexHash    string          `json:"indexHash"`
	Index        json.RawMessage `json:"index"`
}
type UploadedIndex struct {
	UploadID uuid.UUID `json:"uploadId"`
	Hash     string    `json:"hash"`
	Ready    bool      `json:"ready"`
}

func indexError(code string) error {
	return commandbus.NewError(409, code, "Индекс кода недоступен", "Проверьте индекс и повторите сборку/загрузку manifest")
}
func registerIndex(bus *commandbus.Bus) {
	commandbus.Register(bus, commandbus.Command[UploadIndex, UploadedIndex]{Name: "upload-code-index",
		Authorize: func(a auth.Actor, _ UploadIndex) error {
			if a.Kind != auth.ActorService {
				return commandbus.NewError(403, "FORBIDDEN", "Нужен сервисный актор", "Индекс загружает CI/CD")
			}
			return commandbus.Require(a, auth.ManifestRegister)
		},
		Validate: func(p UploadIndex) error {
			if !codeindex.HashPattern.MatchString(p.ManifestHash) || !codeindex.HashPattern.MatchString(p.IndexHash) {
				return indexError("CODE_INDEX_INVALID")
			}
			_, body, err := codeindex.Parse(p.Index)
			if err != nil {
				return indexError("CODE_INDEX_INVALID")
			}
			value, _ := manifest.ParseJSON(body)
			hash, _ := manifest.Hash(value)
			if hash != p.IndexHash {
				return indexError("CODE_INDEX_HASH_MISMATCH")
			}
			return nil
		},
		Handle: func(ctx context.Context, tx pgx.Tx, a auth.Actor, p UploadIndex) (UploadedIndex, error) {
			index, body, _ := codeindex.Parse(p.Index)
			q := store.New(tx)
			if _, err := q.DeleteExpiredCodeIndexes(ctx); err != nil {
				return UploadedIndex{}, err
			}
			if err := q.InsertCodeIndexUpload(ctx, store.InsertCodeIndexUploadParams{ID: uuid.Must(uuid.NewV7()), ProjectID: a.ProjectID, ManifestHash: p.ManifestHash, IndexHash: p.IndexHash, RepoCommit: index.Repo.Commit, Body: body, CreatedBy: a.ID}); err != nil {
				return UploadedIndex{}, err
			}
			row, err := q.FindCodeIndexUpload(ctx, store.FindCodeIndexUploadParams{ProjectID: a.ProjectID, ManifestHash: p.ManifestHash, IndexHash: p.IndexHash})
			return UploadedIndex{UploadID: row.ID, Hash: row.IndexHash, Ready: true}, err
		},
	})
}
func registrationIndex(ctx context.Context, q *store.Queries, a auth.Actor, p Payload, hash string, app map[string]any) (*string, error) {
	if p.CodeIndexUploadID == nil {
		return nil, nil
	}
	id, err := uuid.Parse(*p.CodeIndexUploadID)
	if err != nil {
		return nil, indexError("CODE_INDEX_INVALID")
	}
	row, err := q.GetCodeIndexUpload(ctx, store.GetCodeIndexUploadParams{ProjectID: a.ProjectID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, indexError("CODE_INDEX_UPLOAD_NOT_READY")
	}
	if err != nil {
		return nil, err
	}
	expected, _ := app["codeIndex"].(map[string]any)
	if row.ManifestHash != hash || row.IndexHash != expected["hash"] {
		return nil, indexError("CODE_INDEX_HASH_MISMATCH")
	}
	if !row.Linked && time.Since(row.CreatedAt) > 24*time.Hour {
		return nil, indexError("CODE_INDEX_UPLOAD_NOT_READY")
	}
	index, _, err := codeindex.Parse(row.Body)
	if err != nil || !codeindex.Match(index, app) {
		return nil, indexError("CODE_INDEX_INVALID")
	}
	key := id.String()
	return &key, nil
}
