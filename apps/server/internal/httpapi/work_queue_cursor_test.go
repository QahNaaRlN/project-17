package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestQueueCursorIntegrity(t *testing.T) {
	key := []byte("test-only-key")
	good := queueCursor{1, "context", time.Now().UTC(), uuid.New()}
	raw := signQueueCursor(good, key)
	got, err := parseQueueCursor(raw, "context", key)
	if err != nil || got.ID != good.ID {
		t.Fatal(got, err)
	}
	for _, v := range []string{"", raw + "x", "%%%." + raw, signQueueCursor(queueCursor{2, "context", good.Time, good.ID}, key), signQueueCursor(queueCursor{1, "other", good.Time, good.ID}, key), signQueueCursor(queueCursor{1, "context", time.Time{}, good.ID}, key), signQueueCursor(queueCursor{1, "context", good.Time, uuid.Nil}, key)} {
		if _, err := parseQueueCursor(v, "context", key); err == nil {
			t.Fatal("accepted invalid cursor", v)
		}
	}
	body := base64.RawURLEncoding.EncodeToString([]byte("invalid-json"))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(body))
	if _, err := parseQueueCursor(body+"."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), "context", key); err == nil {
		t.Fatal("accepted invalid JSON")
	}
	if _, err := parseQueueCursor(raw, "context", []byte("different-key")); err == nil {
		t.Fatal("accepted wrong key")
	}
}
