package linker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ikts/cms/internal/harness"
	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/skills"
	"github.com/ikts/cms/internal/storage"
)

func setupLinker(t *testing.T) (string, skills.Registry, model.SkillMetadata, model.SkillMetadata) {
	t.Helper()
	root := t.TempDir()
	p := storage.Paths{DataDir: filepath.Join(root, "data")}
	if err := storage.EnsureDirs(p); err != nil {
		t.Fatal(err)
	}
	r := skills.NewRegistry(p)
	makeSkill := func(name string) model.SkillMetadata {
		src := t.TempDir()
		if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: "+name+"\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		m, _, err := r.InstallDirectory(src, model.SkillSource{Type: "local", Repository: "test", Path: name, Commit: name})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	return root, r, makeSkill("first"), makeSkill("second")
}

func TestReconcileIdempotenceAndPreservation(t *testing.T) {
	root, r, first, second := setupLinker(t)
	ctx := model.Context{Name: "backend", Skills: []model.SkillRef{{ID: first.ID}}}
	plan, err := BuildPlan(root, model.ProjectState{}, ctx, []harness.Adapter{harness.Codex{}, harness.Claude{}}, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts()) != 0 {
		t.Fatal(plan.Conflicts())
	}
	if err := Apply(root, plan, r); err != nil {
		t.Fatal(err)
	}
	manual := filepath.Join(root, ".agents", "skills", "custom-local")
	if err := os.WriteFile(manual, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := storage.LoadState(root)
	if err != nil {
		t.Fatal(err)
	}
	again, err := BuildPlan(root, state, ctx, []harness.Adapter{harness.Codex{}, harness.Claude{}}, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range again.Actions {
		if a.Kind != Keep {
			t.Fatalf("expected only KEEP, got %#v", again.Actions)
		}
	}
	next := model.Context{Name: "frontend", Skills: []model.SkillRef{{ID: second.ID}}}
	plan, err = BuildPlan(root, state, next, []harness.Adapter{harness.Codex{}, harness.Claude{}}, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts()) != 0 {
		t.Fatal(plan.Conflicts())
	}
	if err := Apply(root, plan, r); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(manual); err != nil {
		t.Fatal("manual skill was removed:", err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".agents", "skills", "first")); !os.IsNotExist(err) {
		t.Fatal("old managed link remains")
	}
}

func TestReconcileRealDirectoryIsConflict(t *testing.T) {
	root, r, first, _ := setupLinker(t)
	target := filepath.Join(root, ".agents", "skills", first.Name)
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(root, model.ProjectState{}, model.Context{Name: "ctx", Skills: []model.SkillRef{{ID: first.ID}}}, []harness.Adapter{harness.Codex{}}, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts()) != 1 {
		t.Fatalf("expected one conflict: %#v", plan.Actions)
	}
}

func TestApplyRollsBackWhenALaterLinkFails(t *testing.T) {
	root, r, first, _ := setupLinker(t)
	parent := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(parent, "blocked")
	if err := os.WriteFile(blocked, []byte("third-party"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := Plan{State: model.ProjectState{SchemaVersion: 1, Context: "ctx"}, Actions: []Action{
		{Kind: Create, Target: ".agents/skills/first", Skill: first.ID},
		{Kind: Create, Target: ".agents/skills/blocked", Skill: first.ID},
	}}
	if err := Apply(root, plan, r); err == nil {
		t.Fatal("expected second link to fail")
	}
	if _, err := os.Lstat(filepath.Join(parent, "first")); !os.IsNotExist(err) {
		t.Fatal("rollback left the first link behind")
	}
	if data, err := os.ReadFile(blocked); err != nil || string(data) != "third-party" {
		t.Fatal("rollback damaged the existing file")
	}
}

func TestUnknownBrokenSymlinkIsConflict(t *testing.T) {
	root, r, first, _ := setupLinker(t)
	target := filepath.Join(root, ".agents", "skills", first.Name)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside"), target); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(root, model.ProjectState{}, model.Context{Name: "ctx", Skills: []model.SkillRef{{ID: first.ID}}}, []harness.Adapter{harness.Codex{}}, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts()) != 1 {
		t.Fatalf("expected broken unknown symlink conflict: %#v", plan.Actions)
	}
}
