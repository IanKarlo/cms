package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ikts/cms/internal/model"
)

func TestContextRoundTrip(t *testing.T) {
	root := t.TempDir()
	p := Paths{DataDir: filepath.Join(root, "data")}
	if err := EnsureDirs(p); err != nil {
		t.Fatal(err)
	}
	r := Registry{Paths: p}
	m := model.SkillMetadata{ID: "demo@12345678", Name: "demo", Description: "demo", InstallPath: "skills/demo@12345678/content"}
	if err := r.Save(m); err != nil {
		t.Fatal(err)
	}
	s := ContextStore{Paths: p, Registry: r}
	want := model.Context{Name: "backend", Description: "Backend", Skills: []model.SkillRef{{ID: m.ID}}, MCPs: []string{}}
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("backend")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != want.Name || got.Description != want.Description || len(got.Skills) != 1 || got.Skills[0].ID != m.ID {
		t.Fatalf("got %#v", got)
	}
	if _, err := os.Stat(s.Path("backend")); err != nil {
		t.Fatal(err)
	}
}

func TestContextRejectsDuplicateHarnessNames(t *testing.T) {
	root := t.TempDir()
	p := Paths{DataDir: filepath.Join(root, "data")}
	if err := EnsureDirs(p); err != nil {
		t.Fatal(err)
	}
	r := Registry{Paths: p}
	for _, id := range []string{"one@11111111", "two@22222222"} {
		if err := r.Save(model.SkillMetadata{ID: id, Name: "same", Description: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	s := ContextStore{Paths: p, Registry: r}
	err := s.Save(model.Context{Name: "ctx", Skills: []model.SkillRef{{ID: "one@11111111"}, {ID: "two@22222222"}}})
	if err == nil {
		t.Fatal("expected duplicate name validation")
	}
}
