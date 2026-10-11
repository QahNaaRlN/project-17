package changes

import (
	"encoding/hex"
	"errors"
	"reflect"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
	"github.com/qahnaarln/project-17/apps/server/internal/platform/postgres"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

var fileFields = []string{"fileHash", "mimeType", "size", "assetKind", "width", "height", "durationMs", "blurhash", "managedFile"}

func fileBody(f store.AssetFile) map[string]any {
	kind := "file"
	if strings.HasPrefix(f.MimeType, "image/") {
		kind = "image"
	}
	if strings.HasPrefix(f.MimeType, "video/") {
		kind = "video"
	}
	body := map[string]any{"fileHash": hex.EncodeToString(f.Sha256), "mimeType": f.MimeType, "size": f.SizeBytes, "assetKind": kind, "managedFile": true}
	if f.Width != nil {
		body["width"] = *f.Width
	}
	if f.Height != nil {
		body["height"] = *f.Height
	}
	if f.DurationMs != nil {
		body["durationMs"] = *f.DurationMs
	}
	if f.Blurhash != nil {
		body["blurhash"] = *f.Blurhash
	}
	return decodeBody(mustJSON(body))
}
func (s *session) createAsset(in OperationInput) (recordInput, error) {
	var p struct {
		UploadID uuid.UUID `json:"uploadId"`
	}
	if err := parsePayload(in.Payload, &p); err != nil {
		return recordInput{}, err
	}
	f, err := s.q.ReadyAssetUpload(s.ctx, store.ReadyAssetUploadParams{ProjectID: s.actor.ProjectID, ID: p.UploadID, ActorID: s.actor.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return recordInput{}, payloadError("загрузка не готова или недоступна")
	}
	if err != nil {
		return recordInput{}, err
	}
	if _, err := s.q.CreateAssetObject(s.ctx, store.CreateAssetObjectParams{ID: f.ID, ProjectID: s.actor.ProjectID}); err != nil {
		if postgres.IsUniqueViolation(err) {
			return recordInput{}, payloadError("ассет этой загрузки уже создан")
		}
		return recordInput{}, err
	}
	body := fileBody(store.AssetFile{Sha256: f.Sha256, MimeType: f.MimeType, SizeBytes: f.SizeBytes, Width: f.Width, Height: f.Height, DurationMs: f.DurationMs, Blurhash: f.Blurhash})
	d, err := s.createWorking(f.ID, nil, nil, body)
	if err != nil {
		return recordInput{}, err
	}
	d.kind = "asset"
	return recordInput{target: f.ID, opType: AssetCreate, payload: in.Payload, after: map[string]any{"assetId": f.ID, "data": body}}, nil
}

// Only a ready project file may be selected. Supplied derived fields must match;
// this also validates inverse operations without allowing metadata/file forgery.
func (s *session) replaceAssetFile(id uuid.UUID, d *workingDoc, op ops.Op) (recordInput, error) {
	var p struct {
		Set   map[string]any `json:"set"`
		Unset []string       `json:"unset"`
	}
	if err := parsePayload(op.Payload, &p); err != nil {
		return recordInput{}, err
	}
	text, ok := p.Set["fileHash"].(string)
	hash, err := hex.DecodeString(text)
	if !ok || err != nil || len(hash) != 32 {
		return recordInput{}, payloadError("set.fileHash нужен SHA-256 готового файла")
	}
	f, err := s.q.ReadyAssetFile(s.ctx, store.ReadyAssetFileParams{ProjectID: s.actor.ProjectID, Sha256: hash})
	if errors.Is(err, pgx.ErrNoRows) {
		return recordInput{}, payloadError("файл не готов")
	}
	if err != nil {
		return recordInput{}, err
	}
	desired := fileBody(f)
	for k, v := range p.Set {
		if !reflect.DeepEqual(desired[k], v) {
			return recordInput{}, payloadError("производные поля файла неизменяемы")
		}
	}
	unset := []string{}
	for _, k := range fileFields {
		if _, ok := desired[k]; !ok {
			unset = append(unset, k)
		}
	}
	for _, k := range p.Unset {
		found := false
		for _, u := range unset {
			found = found || k == u
		}
		if !found {
			return recordInput{}, payloadError("можно удалять только отсутствующие поля файла")
		}
	}
	normalized := mustJSON(map[string]any{"set": desired, "unset": unset})
	r, err := contentFields(d.body, ops.Op{Type: AssetReplaceFile, Payload: normalized})
	if err != nil {
		return recordInput{}, err
	}
	return recordInput{target: id, opType: op.Type, payload: normalized, before: r.Before, after: r.After, inverse: &r.Inverse}, nil
}
