package delivery

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTokenRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	cs := uuid.New()
	at := time.Unix(1_800_000_000, 0)
	c := previewClaims{Project: "store", Environment: "staging", Changeset: &cs, Subject: uuid.New(), Expires: at.Add(PreviewTTL).Unix()}
	token := signToken(key, c)

	got, err := unverifiedClaims(token)
	if err != nil || got.Project != "store" || *got.Changeset != cs || got.Subject != c.Subject {
		t.Fatalf("%+v %v", got, err)
	}
	if err := verifyToken(key, token, at); err != nil {
		t.Errorf("действующий токен: %v", err)
	}
	if verifyToken(key, token, at.Add(PreviewTTL)) == nil {
		t.Error("истёкший токен (exp включительно)")
	}
	if verifyToken(key, token, at.Add(PreviewTTL-time.Second)) != nil {
		t.Error("за секунду до истечения токен ещё действует")
	}
	if verifyToken([]byte("other key"), token, at) == nil {
		t.Error("чужой ключ")
	}
	parts := strings.Split(token, ".")
	forged := parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"prj":"store","env":"production","sub":"`+c.Subject.String()+`","exp":9999999999}`)) + "." + parts[2]
	if verifyToken(key, forged, at) == nil {
		t.Error("подменённые поля")
	}
	if verifyToken(key, "no-dots", at) == nil {
		t.Error("без точек")
	}
}

func TestUnverifiedClaimsRejectsMalformed(t *testing.T) {
	for _, token := range []string{
		"a.b",
		"x." + base64.RawURLEncoding.EncodeToString([]byte(`{}`)) + ".s", // другой заголовок (alg none и т. п.)
		jwtHeader + ".!!!.s", // не base64
		jwtHeader + "." + base64.RawURLEncoding.EncodeToString([]byte(`[]`)) + ".s", // не объект
	} {
		if _, err := unverifiedClaims(token); err == nil {
			t.Errorf("%q", token)
		}
	}
}
