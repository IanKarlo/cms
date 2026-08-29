package github

import "testing"

func TestParseURL(t *testing.T) {
	tests := []struct{ url, repo, path, ref string }{
		{"https://github.com/org/repo", "org/repo", "", ""},
		{"https://github.com/org/repo/tree/develop/skills/demo", "org/repo", "skills/demo", "develop"},
		{"https://github.com/org/repo/blob/main/skills/demo/SKILL.md", "org/repo", "skills/demo", "main"},
	}
	for _, tc := range tests {
		s, err := ParseURL(tc.url)
		if err != nil {
			t.Errorf("%s: %v", tc.url, err)
			continue
		}
		if s.Repository != tc.repo || s.Path != tc.path || s.Ref != tc.ref {
			t.Errorf("%s: %#v", tc.url, s)
		}
	}
	if _, err := ParseURL("https://example.com/org/repo"); err == nil {
		t.Fatal("expected non-GitHub URL to fail")
	}
}
