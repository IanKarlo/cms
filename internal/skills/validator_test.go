package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseManifest(t *testing.T) {
	m, err := ParseManifest([]byte("---\nname: go-testing\ndescription: |\n  Guidelines for Go tests\nallowed-tools: Bash\n---\n# Body\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "go-testing" || m.Description == "" || !m.HasAllowedTools {
		t.Fatalf("unexpected manifest: %#v", m)
	}
}

func TestParseManifestValidation(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"missing frontmatter", "# no frontmatter"},
		{"missing description", "---\nname: valid-name\n---"},
		{"invalid name", "---\nname: Bad_Name\ndescription: no\n---"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(tc.body)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidateDirectoryAndInspection(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: local-skill\ndescription: local\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateDirectory(root); err != nil {
		t.Fatal(err)
	}
	hasScripts, hasExec, err := InspectFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasScripts || !hasExec {
		t.Fatalf("expected script indicators: %v %v", hasScripts, hasExec)
	}
}
