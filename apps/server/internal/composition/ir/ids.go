package ir

import (
	"crypto/rand"
	"regexp"
)

var nodeIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{4,32}$`)

const (
	idAlphabet     = "abcdefghijklmnopqrstuvwxyz0123456789"
	idRandomLength = 8
)

// IsNodeID проверяет формат ID узла (02-ir.md §3).
func IsNodeID(s string) bool {
	return nodeIDPattern.MatchString(s)
}

// GenerateNodeID возвращает новый ID узла: "n_" + 8 случайных символов [a-z0-9],
// не встречающийся в taken.
func GenerateNodeID(taken map[string]bool) string {
	for {
		buf := make([]byte, 0, 2+idRandomLength)
		buf = append(buf, "n_"...)
		var b [1]byte
		for len(buf) < cap(buf) {
			if _, err := rand.Read(b[:]); err != nil {
				panic(err) // crypto/rand не возвращает ошибок на поддерживаемых платформах
			}
			// 256 не делится на 36 — отбрасываем байты ≥ 252 для равномерного распределения.
			if b[0] >= 252 {
				continue
			}
			buf = append(buf, idAlphabet[int(b[0])%len(idAlphabet)])
		}
		if id := string(buf); !taken[id] {
			return id
		}
	}
}
