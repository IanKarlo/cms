package github

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSearchUsesCacheAndRanksResults(t *testing.T) {
	searchCalls, rawCalls := 0, 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		response := func(body string) *http.Response {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/search/code"):
			searchCalls++
			if !strings.Contains(r.URL.Query().Get("q"), "path:skills") {
				t.Errorf("search query is not restricted to top-level skills: %q", r.URL.Query().Get("q"))
			}
			body, _ := json.Marshal(map[string]any{"items": []any{
				map[string]any{"path": "skills/exact/SKILL.md", "repository": map[string]string{"full_name": "org/repo", "default_branch": "main"}},
				map[string]any{"path": "skills/exact/SKILL.md", "repository": map[string]string{"full_name": "org/repo", "default_branch": "main"}},
				map[string]any{"path": "skills/other/SKILL.md", "repository": map[string]string{"full_name": "org/other", "default_branch": "main"}},
			}})
			return response(string(body)), nil
		default:
			rawCalls++
			name := "other"
			if strings.Contains(r.URL.Path, "/org/root/main/SKILL.md") {
				name = "root-skill"
			}
			if strings.Contains(r.URL.Path, "/skills/exact/") {
				name = "query"
			}
			return response("---\nname: " + name + "\ndescription: " + name + " skill\n---\n"), nil
		}
	})
	p := NewWithCache(filepath.Join(t.TempDir(), "cache"), time.Hour)
	p.APIBase, p.RawBase, p.Client = "http://cms.test", "http://cms.test", &http.Client{Transport: transport}
	first, err := p.Search(t.Context(), "query", 10)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Search(t.Context(), "query", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("unexpected results: %d %d", len(first), len(second))
	}
	if first[0].Name != "query" || first[0].Score <= first[1].Score {
		t.Fatalf("ranking failed: %#v", first)
	}
	if searchCalls != 1 || rawCalls != 2 {
		t.Fatalf("cache did not prevent repeat request: search=%d raw=%d", searchCalls, rawCalls)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
