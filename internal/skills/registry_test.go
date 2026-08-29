package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/storage"
)

func testRegistry(t *testing.T) Registry {
	t.Helper()
	root := t.TempDir()
	p := storage.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), CacheDir: filepath.Join(root, "cache")}
	if err := storage.EnsureDirs(p); err != nil {
		t.Fatal(err)
	}
	return NewRegistry(p)
}

func TestSkillIDStableAndDistinct(t *testing.T) {
	s := model.SkillSource{Type: "github", Repository: "org/repo", Path: "skills/demo", Ref: "main", Commit: "abc"}
	if SkillID("demo", s) != SkillID("demo", s) {
		t.Fatal("skill ID is not stable")
	}
	s.Commit = "def"
	if SkillID("demo", s) == SkillID("demo", model.SkillSource{Type: "github", Repository: "org/repo", Path: "skills/demo", Ref: "main", Commit: "abc"}) {
		t.Fatal("revisions should have different IDs")
	}
}

func TestInstallDirectoryIsAtomicAndIdempotent(t *testing.T) {
	r := testRegistry(t)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: demo\ndescription: Demo skill\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "notes.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := model.SkillSource{Type: "local", Repository: "test", Path: "demo", Ref: "test", Commit: "one"}
	first, already, err := r.InstallDirectory(src, source)
	if err != nil || already {
		t.Fatalf("first install: %v %v", err, already)
	}
	if _, err := os.Stat(filepath.Join(r.Store.Paths.SkillsDir(), first.ID, "content", "notes.md")); err != nil {
		t.Fatal(err)
	}
	second, already, err := r.InstallDirectory(src, source)
	if err != nil || !already || second.ID != first.ID {
		t.Fatalf("second install: %#v %v %v", second, already, err)
	}
	list, err := r.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("registry list: %d %v", len(list), err)
	}
	if err := r.Remove(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(first.ID); err == nil {
		t.Fatal("removed skill should not remain in metadata")
	}
	if _, err := os.Stat(filepath.Join(r.Store.Paths.SkillsDir(), first.ID)); !os.IsNotExist(err) {
		t.Fatalf("removed skill content still exists: %v", err)
	}
}
