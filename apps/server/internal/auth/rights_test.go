package auth

import (
	"reflect"
	"testing"
)

func TestEffectiveRights(t *testing.T) {
	cases := []struct {
		name   string
		roles  []string
		scopes []string
		want   []Right
	}{
		{"все права ролей", []string{"content.read", "content.write"}, []string{ScopeAll}, []Right{ContentRead, ContentWrite}},
		{"токен сужает права", []string{"content.read", "content.write"}, []string{"content.read"}, []Right{ContentRead}},
		{"токен не расширяет права", []string{"content.read"}, []string{"content.read", "project.admin"}, []Right{ContentRead}},
		{"неизвестные права игнорируются", []string{"content.read", "root"}, []string{ScopeAll}, []Right{ContentRead}},
		{"без областей — без прав", []string{"content.read"}, nil, []Right{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectiveRights(tc.roles, tc.scopes).Sorted(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("получено %v, ожидалось %v", got, tc.want)
			}
		})
	}
}

func TestAllRightsAreUnique(t *testing.T) {
	seen := map[Right]bool{}
	for _, r := range AllRights {
		if seen[r] {
			t.Errorf("повтор %s", r)
		}
		seen[r] = true
	}
	if len(AllRights) != 21 {
		t.Errorf("прав %d; перечень в 09-agent.md §2.1 содержит 21", len(AllRights))
	}
}

func TestTokens(t *testing.T) {
	a, hashA := NewToken(ServiceTokenPrefix)
	b, _ := NewToken(ServiceTokenPrefix)
	if a == b || len(a) < len(ServiceTokenPrefix)+40 || a[:len(ServiceTokenPrefix)] != ServiceTokenPrefix {
		t.Errorf("токены: %q %q", a, b)
	}
	if !reflect.DeepEqual(HashToken(a), hashA) || len(hashA) != 32 {
		t.Error("хэш токена не совпадает")
	}
}
