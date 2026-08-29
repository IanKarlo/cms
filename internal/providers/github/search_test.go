package github

import "testing"

func TestSkillDirectoryAcceptsRootAndNestedManifests(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "SKILL.md", want: ""},
		{path: "skills/demo/SKILL.md", want: "skills/demo"},
	}
	for _, tc := range tests {
		got, ok := skillDirectory(tc.path)
		if !ok || got != tc.want {
			t.Fatalf("skillDirectory(%q) = %q, %v; want %q, true", tc.path, got, ok, tc.want)
		}
	}
	if _, ok := skillDirectory("README.md"); ok {
		t.Fatal("non-manifest path should not be accepted")
	}
}

func TestRawSkillURLOmitsEmptyPathSegment(t *testing.T) {
	got := rawSkillURL("https://raw.example", "org/repo", "main", "")
	if got != "https://raw.example/org/repo/main/SKILL.md" {
		t.Fatalf("raw URL = %q", got)
	}
}

func TestIsTopLevelSkillPath(t *testing.T) {
	for _, path := range []string{"skills/go-testing", "skills/api-design"} {
		if !isTopLevelSkillPath(path) {
			t.Errorf("%q should be accepted", path)
		}
	}
	for _, path := range []string{"", "SKILL.md", "plugins/foo/skills/bar", "skills/nested/bar"} {
		if isTopLevelSkillPath(path) {
			t.Errorf("%q should be rejected", path)
		}
	}
}
