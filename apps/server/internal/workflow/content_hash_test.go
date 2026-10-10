package workflow

import (
	"bytes"
	"crypto/sha256"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"testing"
)

// CHG-035: schema version and deletion state are approved content; legacy document hashes stay stable.
func TestContentHashBindsEntityVersionAndDeletion(t *testing.T) {
	id := uuid.MustParse("0192f1c4-7a1e-7c2b-9d10-3b5f2a9e4c11")
	row := store.ChangesetWorkingVersionsRow{ObjectID: id, BodyHash: []byte("body")}
	original := contentHash([]store.ChangesetWorkingVersionsRow{row})
	legacy := sha256.New()
	legacy.Write(id[:])
	legacy.Write([]byte("body"))
	legacy.Write([]byte{0})
	if !bytes.Equal(original, legacy.Sum(nil)) {
		t.Fatal("legacy hash changed")
	}
	one, two := int32(1), int32(2)
	row.SchemaVersion = &one
	v1 := contentHash([]store.ChangesetWorkingVersionsRow{row})
	row.SchemaVersion = &two
	v2 := contentHash([]store.ChangesetWorkingVersionsRow{row})
	row.Deleted = true
	deleted := contentHash([]store.ChangesetWorkingVersionsRow{row})
	if bytes.Equal(original, v1) || bytes.Equal(v1, v2) || bytes.Equal(v2, deleted) {
		t.Fatal("approval hash omits metadata")
	}
}
