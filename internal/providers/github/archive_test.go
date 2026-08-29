package github

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractSkillRejectsTraversal(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "bad.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create("repo-main/../escape/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("bad"))
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractSkill(zipPath, "", t.TempDir()); err == nil {
		t.Fatal("expected traversal archive to be rejected")
	}
}

func TestExtractSkillPathsInstallsAllSkillsFromRepository(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "skills.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	files := map[string]string{
		"repo-main/README.md":                "repository documentation",
		"repo-main/SKILL.md":                 "---\nname: root-skill\ndescription: Root skill\n---\n",
		"repo-main/notes.md":                 "root resource",
		"repo-main/skills/nested/SKILL.md":   "---\nname: nested-skill\ndescription: Nested skill\n---\n",
		"repo-main/skills/nested/example.md": "nested resource",
	}
	for name, content := range files {
		w, createErr := z.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := w.Write([]byte(content)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	extracted, err := extractSkillPaths(zipPath, "", dest, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(extracted) != 1 {
		t.Fatalf("extracted %d skills, want 1 top-level skill", len(extracted))
	}
	if extracted[0].Path != "skills/nested" {
		t.Fatalf("unexpected paths: %#v", extracted)
	}
	for _, item := range extracted {
		if _, err := os.Stat(filepath.Join(item.Directory, "SKILL.md")); err != nil {
			t.Fatalf("manifest for %q: %v", item.Path, err)
		}
	}
}
