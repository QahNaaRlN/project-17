package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/buckket/go-blurhash"
	"github.com/gen2brain/avif"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/platform/postgres"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

func detect(raw []byte) string {
	// ISO-BMFF AVIF has an ftyp box whose major or compatible brand is avif/avis.
	if len(raw) >= 16 && string(raw[4:8]) == "ftyp" {
		size := int(uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3]))
		if size >= 16 && size <= len(raw) {
			for i := 8; i+4 <= size; i += 4 {
				if i == 12 {
					continue
				}
				if string(raw[i:i+4]) == "avif" || string(raw[i:i+4]) == "avis" {
					return "image/avif"
				}
			}
		}
	}
	trimmed := bytes.TrimSpace(raw)
	if bytes.HasPrefix(trimmed, []byte("<svg")) || bytes.HasPrefix(trimmed, []byte("<?xml")) {
		return "image/svg+xml"
	}
	return strings.Split(http.DetectContentType(raw), ";")[0]
}

// Process writes only file records and upload status. It never edits a content version.
// Storage/network errors roll back status and are retried; deterministic rejection is terminal.
func (s *Service) Process(ctx context.Context, args ProcessArgs) error {
	if s.Storage == nil {
		return unavailable()
	}
	return postgres.InTx(ctx, s.Pool, func(tx pgx.Tx) error {
		var key, mime, status string
		var size int64
		var expected []byte
		err := tx.QueryRow(ctx, `SELECT storage_key,mime_type,size_bytes,source_sha256,status FROM asset_uploads WHERE id=$1 AND project_id=$2 FOR UPDATE`, args.UploadID, args.ProjectID).Scan(&key, &mime, &size, &expected, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if status != "processing" {
			return nil
		}
		reject := func(code string) error {
			_, err := tx.Exec(ctx, `UPDATE asset_uploads SET status='rejected',error_code=$2 WHERE id=$1`, args.UploadID, code)
			return err
		}
		var allowed bool
		if err := tx.QueryRow(ctx, `SELECT CASE WHEN settings#>'{assets,mimeTypes}' IS NULL THEN true ELSE settings#>'{assets,mimeTypes}' @> jsonb_build_array($2::text) END FROM projects WHERE id=$1`, args.ProjectID, mime).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return reject("ASSET_MIME_FORBIDDEN")
		}
		source, actual, err := s.Storage.Open(ctx, key)
		if err != nil {
			return err
		}
		defer source.Close()
		if actual != size || size <= 0 || size > Limit(mime) {
			return reject("ASSET_SIZE_MISMATCH")
		}
		f, err := os.CreateTemp("", "cms-asset-*")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		defer f.Close()
		hash := sha256.New()
		n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(source, size+1))
		if err != nil {
			return err
		}
		if n != size {
			return reject("ASSET_SIZE_MISMATCH")
		}
		if !bytes.Equal(hash.Sum(nil), expected) {
			return reject("ASSET_HASH_MISMATCH")
		}
		if _, err = f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		header := make([]byte, 512)
		nheader, err := f.Read(header)
		if err != nil && err != io.EOF {
			return err
		}
		if detect(header[:nheader]) != mime {
			return reject("ASSET_MIME_MISMATCH")
		}
		if _, err = f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		var binary io.Reader = f
		final := expected
		finalSize := size
		var preview []byte
		w, h := 0, 0
		var blur *string
		if mime == "image/svg+xml" {
			raw, err := io.ReadAll(f)
			if err != nil {
				return err
			}
			clean, err := sanitizeSVG(raw)
			if err != nil {
				return reject("ASSET_INVALID_SVG")
			}
			sha := sha256.Sum256(clean)
			final = sha[:]
			finalSize = int64(len(clean))
			binary = bytes.NewReader(clean)
			w, h = svgDimensions(clean)
		} else if strings.HasPrefix(mime, "image/") {
			var cfg image.Config
			if mime == "image/avif" {
				cfg, err = avif.DecodeConfig(f)
			} else {
				cfg, _, err = image.DecodeConfig(f)
			}
			if err != nil {
				return reject("ASSET_INVALID_IMAGE")
			}
			if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 40000000 {
				return reject("ASSET_IMAGE_TOO_LARGE")
			}
			if _, err = f.Seek(0, io.SeekStart); err != nil {
				return err
			}
			var img image.Image
			if mime == "image/avif" {
				img, err = avif.Decode(f)
			} else {
				img, _, err = image.Decode(f)
			}
			if err != nil {
				return reject("ASSET_INVALID_IMAGE")
			}
			w, h = cfg.Width, cfg.Height
			pw, ph := w, h
			if pw > 320 || ph > 320 {
				if pw >= ph {
					ph = max(1, h*320/w)
					pw = 320
				} else {
					pw = max(1, w*320/h)
					ph = 320
				}
			}
			thumbnail := image.NewNRGBA(image.Rect(0, 0, pw, ph))
			draw.CatmullRom.Scale(thumbnail, thumbnail.Bounds(), img, img.Bounds(), draw.Over, nil)
			var b bytes.Buffer
			if err := png.Encode(&b, thumbnail); err != nil {
				return err
			}
			preview = b.Bytes()
			encoded, err := blurhash.Encode(4, 3, thumbnail)
			if err != nil {
				return err
			}
			blur = &encoded
			if _, err = f.Seek(0, io.SeekStart); err != nil {
				return err
			}
		}
		// Serializes identical content across independent uploads, including retries.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, args.ProjectID.String()+":"+hex.EncodeToString(final)); err != nil {
			return err
		}
		canonical := "projects/" + args.ProjectID.String() + "/assets/" + hex.EncodeToString(final)
		var ready bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM asset_files WHERE project_id=$1 AND sha256=$2 AND status='ready')`, args.ProjectID, final).Scan(&ready); err != nil {
			return err
		}
		if !ready {
			if err := s.Storage.Put(ctx, canonical, binary, finalSize, mime); err != nil {
				return err
			}
			var previewKey *string
			if len(preview) > 0 {
				k := canonical + "/preview.png"
				if err := s.Storage.Put(ctx, k, bytes.NewReader(preview), int64(len(preview)), "image/png"); err != nil {
					return err
				}
				previewKey = &k
			}
			_, err = tx.Exec(ctx, `INSERT INTO asset_files(project_id,sha256,storage_key,mime_type,size_bytes,width,height,blurhash,status,preview_key) VALUES($1,$2,$3,$4,$5,NULLIF($6,0),NULLIF($7,0),$8,'ready',$9) ON CONFLICT(project_id,sha256) DO UPDATE SET storage_key=EXCLUDED.storage_key,mime_type=EXCLUDED.mime_type,size_bytes=EXCLUDED.size_bytes,width=EXCLUDED.width,height=EXCLUDED.height,blurhash=EXCLUDED.blurhash,status='ready',preview_key=EXCLUDED.preview_key WHERE asset_files.status <> 'ready'`, args.ProjectID, final, canonical, mime, finalSize, w, h, blur, previewKey)
			if err != nil {
				return fmt.Errorf("asset file: %w", err)
			}
		}
		_, err = tx.Exec(ctx, `UPDATE asset_uploads SET status='ready',file_sha256=$2,error_code=NULL WHERE id=$1`, args.UploadID, final)
		return err
	})
}
