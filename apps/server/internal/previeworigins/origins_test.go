package previeworigins

import (
	"slices"
	"testing"
)

func TestSTU328ParseExactOrigins(t *testing.T) {
	l, err := Parse(" https://PR.example.test , http://127.0.0.1:4100,https://pr.example.test,http://[::1]:3000,http://localhost:5173, ")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"http://127.0.0.1:4100", "http://[::1]:3000", "http://localhost:5173", "https://pr.example.test"}
	if !slices.Equal(l.Origins(), want) {
		t.Fatalf("origins: %v", l.Origins())
	}
	if v6, err := Parse("http://[::1]"); err != nil || v6.Origins()[0] != "http://[::1]" {
		t.Fatal("IPv6 loopback without port", v6.Origins(), err)
	}
	if empty, err := Parse(""); err != nil || len(empty.Origins()) != 0 {
		t.Fatal("empty list", empty, err)
	}
	for _, bad := range []string{
		"https://*.example.test",
		"http://pr.example.test",
		"https://pr.example.test/path",
		"https://user@pr.example.test",
		"https://pr.example.test?x=1",
		"https://pr.example.test#a",
		"ftp://pr.example.test",
		"pr.example.test",
		"https://pr%2Eexample.test",
		"://bad",
	} {
		if _, err := Parse(bad); err != ErrConfig {
			t.Errorf("STU-328 must reject %q: %v", bad, err)
		}
	}
}

func TestSTU328AppURL(t *testing.T) {
	l, _ := Parse("https://pr.example.test,http://127.0.0.1:4100")
	for raw, want := range map[string]string{
		"https://pr.example.test":          "https://pr.example.test/",
		"https://PR.example.test/pr-1":     "https://pr.example.test/pr-1",
		"http://127.0.0.1:4100/app/":       "http://127.0.0.1:4100/app/",
		"https://pr.example.test/a%20b/c?": "",
		"https://pr.example.test/?x=1":     "",
		"https://pr.example.test/#f":       "",
		"https://u:p@pr.example.test/":     "",
		"https://other.example.test/":      "",
		"https://pr.example.test:8443/":    "",
		"http://127.0.0.1:4101/":           "",
		"%zz":                              "",
	} {
		got, ok := l.AppURL(raw)
		if got != want || ok != (want != "") {
			t.Errorf("AppURL(%q) = %q %v, want %q", raw, got, ok, want)
		}
	}
	long := "https://pr.example.test/" + string(make([]byte, 2100))
	if _, ok := l.AppURL(long); ok {
		t.Error("oversized URL accepted")
	}
}
