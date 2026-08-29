package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/providers"
	"github.com/ikts/cms/internal/storage"
)

type batchProvider struct {
	downloads []providers.DownloadedSkill
}

type manifestDownloadProvider struct{ name string }

func (manifestDownloadProvider) Search(context.Context, string, int) ([]providers.SearchResult, error) {
	return nil, nil
}
func (manifestDownloadProvider) Resolve(_ context.Context, source model.SkillSource) (model.SkillSource, error) {
	return source, nil
}
func (p manifestDownloadProvider) Download(_ context.Context, _ model.SkillSource, dest string) error {
	data := []byte("---\nname: " + p.name + "\ndescription: synced skill\n---\n")
	return os.WriteFile(filepath.Join(dest, "SKILL.md"), data, 0o644)
}

func (batchProvider) Search(context.Context, string, int) ([]providers.SearchResult, error) {
	return nil, nil
}
func (batchProvider) Resolve(_ context.Context, source model.SkillSource) (model.SkillSource, error) {
	source.Ref, source.Commit = "main", "commit"
	return source, nil
}
func (batchProvider) Download(context.Context, model.SkillSource, string) error { return nil }
func (p batchProvider) DownloadAll(context.Context, model.SkillSource, string) ([]providers.DownloadedSkill, error) {
	return p.downloads, nil
}

func TestCLIContextAndInitEndToEnd(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("CMS_CONFIG_DIR", filepath.Join(root, "config"))
	t.Setenv("CMS_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("CMS_CACHE_DIR", filepath.Join(root, "cache"))
	app, err := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "SKILL.md"), []byte("---\nname: cli-demo\ndescription: CLI integration\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, _, err := app.Registry.InstallDirectory(sourceDir, model.SkillSource{Type: "local", Repository: "test", Path: "cli-demo", Commit: "one"})
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := Run([]string{"context-new", "--name", "backend", "--description", "Backend", "--skill", meta.ID}, &out, &errOut, strings.NewReader("")); code != 0 {
		t.Fatalf("context-new: %d %s", code, errOut.String())
	}
	if code := Run([]string{"init", "backend", "--target", "codex"}, &out, &errOut, strings.NewReader("")); code != 0 {
		t.Fatalf("init: %d %s", code, errOut.String())
	}
	link := filepath.Join(root, ".agents", "skills", "cli-demo")
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink: %v", err)
	}
	if code := Run([]string{"init", "backend", "--target", "codex"}, &out, &errOut, strings.NewReader("")); code != 0 {
		t.Fatalf("second init: %d %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "No changes.") {
		t.Fatalf("expected idempotence output: %s", out.String())
	}
}

func TestSkillInstallRepositoryInstallsAllSkills(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("CMS_CONFIG_DIR", filepath.Join(root, "config"))
	t.Setenv("CMS_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("CMS_CACHE_DIR", filepath.Join(root, "cache"))
	app, err := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}

	var downloads []providers.DownloadedSkill
	for _, skill := range []struct{ name, path string }{{"one", "skills/one"}, {"two", "skills/two"}} {
		dir := t.TempDir()
		manifest := []byte("---\nname: " + skill.name + "\ndescription: " + skill.name + " skill\n---\n")
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), manifest, 0o644); err != nil {
			t.Fatal(err)
		}
		downloads = append(downloads, providers.DownloadedSkill{Source: model.SkillSource{Type: "github", Repository: "org/repo", Path: skill.path, Ref: "main", Commit: "commit"}, Directory: dir})
	}
	var out bytes.Buffer
	app.Out = &out
	app.Provider = batchProvider{downloads: downloads}
	if err := app.skillInstall([]string{"https://github.com/org/repo"}); err != nil {
		t.Fatal(err)
	}
	list, err := app.Registry.List()
	if err != nil || len(list) != 2 {
		t.Fatalf("installed skills: %d %v", len(list), err)
	}
	if !strings.Contains(out.String(), "installed 2 skills from org/repo") {
		t.Fatalf("batch output: %s", out.String())
	}
}

func TestFreezeAndSyncRecreatesPinnedProject(t *testing.T) {
	source := model.SkillSource{Type: "github", Repository: "org/repo", Path: "skills/go-testing", Ref: "main", Commit: "abcdef123456"}
	firstRoot := t.TempDir()
	t.Chdir(firstRoot)
	t.Setenv("CMS_CONFIG_DIR", filepath.Join(firstRoot, "config"))
	t.Setenv("CMS_DATA_DIR", filepath.Join(firstRoot, "data"))
	t.Setenv("CMS_CACHE_DIR", filepath.Join(firstRoot, "cache"))
	first, err := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: go-testing\ndescription: Go testing\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	metadata, _, err := first.Registry.InstallDirectory(src, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Contexts.Save(model.Context{Name: "backend", Description: "Backend", Skills: []model.SkillRef{{ID: metadata.ID}}}); err != nil {
		t.Fatal(err)
	}
	if err := first.freeze([]string{"backend"}); err != nil {
		t.Fatal(err)
	}
	manifestData, err := os.ReadFile(storage.ManifestPath(firstRoot))
	if err != nil {
		t.Fatal(err)
	}

	secondRoot := t.TempDir()
	t.Chdir(secondRoot)
	t.Setenv("CMS_CONFIG_DIR", filepath.Join(secondRoot, "config"))
	t.Setenv("CMS_DATA_DIR", filepath.Join(secondRoot, "data"))
	t.Setenv("CMS_CACHE_DIR", filepath.Join(secondRoot, "cache"))
	if err := os.WriteFile(storage.ManifestPath(secondRoot), manifestData, 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	second.Provider = manifestDownloadProvider{name: "go-testing"}
	var initOut bytes.Buffer
	second.Out = &initOut
	if err := second.init(nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(initOut.String(), "installed 1 skill(s) required by cms.toml:") || !strings.Contains(initOut.String(), metadata.ID) {
		t.Fatalf("init did not report installed skill: %s", initOut.String())
	}
	if _, err := second.Contexts.Get("backend"); err != nil {
		t.Fatal(err)
	}
	if got, err := second.Registry.Get(metadata.ID); err != nil || got.Source.Commit != source.Commit {
		t.Fatalf("synced metadata = %#v, error = %v", got, err)
	}
	for _, link := range []string{
		filepath.Join(secondRoot, ".agents", "skills", "go-testing"),
		filepath.Join(secondRoot, ".claude", "skills", "go-testing"),
	} {
		info, statErr := os.Lstat(link)
		if statErr != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("expected project skill link %s: %v", link, statErr)
		}
	}
}

func TestSkillRemoveBatchAndAll(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("CMS_CONFIG_DIR", filepath.Join(root, "config"))
	t.Setenv("CMS_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("CMS_CACHE_DIR", filepath.Join(root, "cache"))
	app, err := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one", "two"} {
		src := t.TempDir()
		manifest := []byte("---\nname: " + name + "\ndescription: " + name + " skill\n---\n")
		if err := os.WriteFile(filepath.Join(src, "SKILL.md"), manifest, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := app.Registry.InstallDirectory(src, model.SkillSource{Type: "local", Repository: "test", Path: name, Commit: name}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := app.Registry.List()
	if err != nil || len(list) != 2 {
		t.Fatalf("initial skills: %d %v", len(list), err)
	}
	ids := []string{list[0].ID, list[1].ID}
	app.In = strings.NewReader("n\n")
	if err := app.skillRemove(ids); err != nil {
		t.Fatal(err)
	}
	if list, _ = app.Registry.List(); len(list) != 2 {
		t.Fatalf("cancelled removal changed registry: %d", len(list))
	}
	app.In = strings.NewReader("y\n")
	if err := app.skillRemove([]string{"--all"}); err != nil {
		t.Fatal(err)
	}
	if list, _ = app.Registry.List(); len(list) != 0 {
		t.Fatalf("--all left %d skills", len(list))
	}
}

func TestGlobalInitCreatesProtectedHomeLinks(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	t.Setenv("HOME", home)
	t.Setenv("CMS_CONFIG_DIR", filepath.Join(root, "config"))
	t.Setenv("CMS_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("CMS_CACHE_DIR", filepath.Join(root, "cache"))
	app, err := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: global-demo\ndescription: Global demo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, _, err := app.Registry.InstallDirectory(source, model.SkillSource{Type: "local", Repository: "test", Path: "global-demo", Commit: "one"})
	if err != nil {
		t.Fatal(err)
	}
	otherSource := t.TempDir()
	if err := os.WriteFile(filepath.Join(otherSource, "SKILL.md"), []byte("---\nname: other-demo\ndescription: Other demo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	other, _, err := app.Registry.InstallDirectory(otherSource, model.SkillSource{Type: "local", Repository: "test", Path: "other-demo", Commit: "two"})
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	app.Out = &out
	if err := app.globalInit([]string{"--skill", meta.ID}); err != nil {
		t.Fatal(err)
	}
	global, err := app.Contexts.Get(globalContextName)
	if err != nil || len(global.Skills) != 1 || global.Skills[0].ID != meta.ID {
		t.Fatalf("global context = %#v, error = %v", global, err)
	}
	for _, link := range []string{
		filepath.Join(home, ".agents", "skills", meta.Name),
		filepath.Join(home, ".claude", "skills", meta.Name),
	} {
		info, statErr := os.Lstat(link)
		if statErr != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("expected global skill link %s: %v", link, statErr)
		}
	}
	if !strings.Contains(out.String(), "created global context with 1 skill(s)") {
		t.Fatalf("global init output: %s", out.String())
	}
	if _, statErr := os.Lstat(filepath.Join(home, ".agents", "skills", other.Name)); !os.IsNotExist(statErr) {
		t.Fatal("global context unexpectedly linked an unselected skill")
	}

	if err := app.contextEdit([]string{"--name", globalContextName}, false); err == nil {
		t.Fatal("expected reserved global context to reject context-new")
	}
	if err := app.globalRemove([]string{"--yes"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Contexts.Get(globalContextName); err == nil {
		t.Fatal("global context was not removed")
	}
	if _, err := os.Lstat(filepath.Join(home, ".agents", "skills", meta.Name)); !os.IsNotExist(err) {
		t.Fatal("global skill link was not removed")
	}
}
