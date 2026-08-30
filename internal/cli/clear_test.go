package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikts/cms/internal/harness"
	"github.com/ikts/cms/internal/mcps"
	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/skills"
	"github.com/ikts/cms/internal/storage"
)

func newClearTestApp(root string, in io.Reader, out *bytes.Buffer) *App {
	paths := storage.Paths{
		ConfigDir: filepath.Join(root, "config"),
		DataDir:   filepath.Join(root, "data"),
		CacheDir:  filepath.Join(root, "cache"),
	}
	registry := skills.NewRegistry(paths)
	return &App{
		Out: out, Err: &bytes.Buffer{}, In: in, Paths: paths,
		Registry:  registry,
		MCPs:      mcps.NewRegistry(paths),
		Contexts:  storage.ContextStore{Paths: paths, Registry: registry.Store},
		Harnesses: harness.Builtins(),
		Config:    storage.Config{LinkMode: "symlink"},
	}
}

func testClearManifest() model.ProjectManifest {
	return model.ProjectManifest{
		SchemaVersion: 2,
		Context:       "backend",
		Targets:       []string{"codex"},
		Skills: []model.PinnedSkill{{
			ID: "demo@12345678", Name: "demo",
			Source: model.SkillSource{Type: "github", Repository: "org/repo", Path: "skills/demo", Commit: "abcdef"},
		}},
	}
}

func TestClearRequiresConfirmation(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := storage.SaveManifest(root, testClearManifest()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	app := newClearTestApp(root, strings.NewReader("n\n"), &out)
	if err := app.clear(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(storage.ManifestPath(root)); err != nil {
		t.Fatalf("manifest was removed after cancellation: %v", err)
	}
	if !strings.Contains(out.String(), "WARNING: cms clear removes project configuration") || !strings.Contains(out.String(), "Clear cancelled.") {
		t.Fatalf("confirmation warning/cancellation missing:\n%s", out.String())
	}
}

func TestClearRemovesManagedProjectStateOnly(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := storage.SaveManifest(root, testClearManifest()); err != nil {
		t.Fatal(err)
	}
	paths := storage.Paths{DataDir: filepath.Join(root, "data")}
	managedDir := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(managedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	managedLink := filepath.Join(managedDir, "demo")
	if err := os.Symlink(filepath.Join(paths.SkillsDir(), "demo@12345678"), managedLink); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agents", "user-config.toml"), []byte("user = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	externalSkill := filepath.Join(root, ".claude", "skills", "user-skill")
	if err := os.MkdirAll(externalSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(externalSkill, "SKILL.md"), []byte("external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveState(root, model.ProjectState{
		SchemaVersion: 2,
		Context:       "backend",
		Targets:       []string{"codex"},
		Links:         []model.ProjectLink{{SkillID: "demo@12345678", Target: ".agents/skills/demo"}},
	}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	app := newClearTestApp(root, strings.NewReader(""), &out)
	if err := app.clear([]string{"--yes"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{storage.ManifestPath(root), storage.StatePath(root), managedLink} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("managed path %s still exists, err=%v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "user-config.toml")); err != nil {
		t.Fatalf("user harness file was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(externalSkill, "SKILL.md")); err != nil {
		t.Fatalf("non-target harness content was removed: %v", err)
	}
	if !strings.Contains(out.String(), "Cleared 1 skill link(s)") {
		t.Fatalf("clear report missing:\n%s", out.String())
	}
}

func TestClearDryRunDoesNotRemoveFiles(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := storage.SaveManifest(root, testClearManifest()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	app := newClearTestApp(root, strings.NewReader(""), &out)
	if err := app.clear([]string{"--dry-run"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(storage.ManifestPath(root)); err != nil {
		t.Fatalf("dry-run removed manifest: %v", err)
	}
	if !strings.Contains(out.String(), "Would clear") {
		t.Fatalf("dry-run report missing:\n%s", out.String())
	}
}
