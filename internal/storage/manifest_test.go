package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikts/cms/internal/model"
)

func TestManifestRoundTrip(t *testing.T) {
	root := t.TempDir()
	want := model.ProjectManifest{
		SchemaVersion: 1,
		Context:       "backend",
		Description:   "Backend skills",
		Targets:       []string{"claude", "codex"},
		Skills: []model.PinnedSkill{{
			ID: "go-testing@12345678", Name: "go-testing", Description: "Go testing",
			Source: model.SkillSource{Type: "github", Repository: "org/repo", Path: "skills/go-testing", Ref: "main", Commit: "abcdef"},
		}},
	}
	if err := SaveManifest(root, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Context != want.Context || got.Description != want.Description || len(got.Skills) != 1 || got.Skills[0].Source.Commit != want.Skills[0].Source.Commit {
		t.Fatalf("manifest = %#v", got)
	}
	if _, err := os.Stat(filepath.Join(root, "cms.toml")); err != nil {
		t.Fatal(err)
	}
}

func TestLoadManifestNotFound(t *testing.T) {
	_, err := LoadManifest(t.TempDir())
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v, want not found", err)
	}
}
