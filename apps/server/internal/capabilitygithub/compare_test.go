package capabilitygithub

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// STU-339 (decision §11.1): one fixed compare GET; only ahead/identical with
// the merge commit as merge base proves a descendant.
func TestSTU339CompareDescendant(t *testing.T) {
	base, head := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, c := range []struct {
		name    string
		body    map[string]any
		status  int
		descend bool
		code    string
	}{
		{"ahead", map[string]any{"status": "ahead", "ahead_by": 3, "base_commit": map[string]any{"sha": base}, "merge_base_commit": map[string]any{"sha": base}}, 200, true, ""},
		{"identical", map[string]any{"status": "identical", "ahead_by": 0, "base_commit": map[string]any{"sha": base}, "merge_base_commit": map[string]any{"sha": base}}, 200, true, ""},
		{"diverged", map[string]any{"status": "diverged", "ahead_by": 1, "base_commit": map[string]any{"sha": base}, "merge_base_commit": map[string]any{"sha": strings.Repeat("c", 40)}}, 200, false, ""},
		{"behind", map[string]any{"status": "behind", "ahead_by": 0, "base_commit": map[string]any{"sha": base}, "merge_base_commit": map[string]any{"sha": head}}, 200, false, ""},
		{"wrong base", map[string]any{"status": "ahead", "ahead_by": 1, "base_commit": map[string]any{"sha": head}, "merge_base_commit": map[string]any{"sha": base}}, 200, false, "invalid_response"},
		{"unknown status", map[string]any{"status": "strange", "ahead_by": 1, "base_commit": map[string]any{"sha": base}, "merge_base_commit": map[string]any{"sha": base}}, 200, false, "invalid_response"},
		{"not found", map[string]any{}, 404, false, "not_found_or_inaccessible"},
	} {
		t.Run(c.name, func(t *testing.T) {
			calls := 0
			client := &Client{client: &http.Client{Transport: roundtrip(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != "GET" || req.URL.Host != "api.github.com" || req.URL.Path != "/repos/owner/repo/compare/"+base+"..."+head {
					t.Fatal("fixed compare request", req.Method, req.URL)
				}
				return response(c.status, c.body), nil
			})}}
			got, e := client.Compare(context.Background(), repoFixture(), base, head)
			if c.code != "" {
				var f *Failure
				if e == nil || !strings.Contains(e.Error(), c.code) && (!errorsAs(e, &f) || f.Code != c.code) {
					t.Fatal("refused", c.code, e)
				}
				return
			}
			if e != nil || got.Descends() != c.descend || calls != 1 {
				t.Fatal(got, e, calls)
			}
		})
	}
	if _, e := (&Client{}).Compare(context.Background(), repoFixture(), "short", head); e == nil {
		t.Fatal("short SHA")
	}
}

func errorsAs(e error, f **Failure) bool {
	v, ok := e.(*Failure)
	*f = v
	return ok
}
