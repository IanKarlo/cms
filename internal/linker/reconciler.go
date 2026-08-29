package linker

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ikts/cms/internal/harness"
	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/skills"
	"github.com/ikts/cms/internal/storage"
)

type Kind string

const (
	Create   Kind = "CREATE"
	Remove   Kind = "REMOVE"
	Keep     Kind = "KEEP"
	Conflict Kind = "CONFLICT"
)

type Action struct {
	Kind   Kind
	Target string
	Skill  string
	Reason string
}
type Plan struct {
	Actions []Action
	Desired []model.ProjectLink
	State   model.ProjectState
}

func (p Plan) Conflicts() []Action {
	var out []Action
	for _, a := range p.Actions {
		if a.Kind == Conflict {
			out = append(out, a)
		}
	}
	return out
}

func BuildPlan(root string, current model.ProjectState, desired model.Context, targets []harness.Adapter, registry skills.Registry) (Plan, error) {
	p := Plan{State: model.ProjectState{SchemaVersion: 1, Context: desired.Name}}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return p, err
	}
	old := map[string]model.ProjectLink{}
	for _, l := range current.Links {
		oldPath := filepath.Join(rootAbs, filepath.FromSlash(l.Target))
		if !inside(rootAbs, oldPath) {
			p.Actions = append(p.Actions, Action{Kind: Conflict, Target: l.Target, Skill: l.SkillID, Reason: "state target escapes the project root"})
			continue
		}
		old[cleanRel(l.Target)] = l
	}
	newTargets := map[string]bool{}
	for _, adapter := range targets {
		if !adapter.SupportsSymlink() {
			return p, fmt.Errorf("harness %q does not support symlinks", adapter.ID())
		}
		if err := validateSkillDir(rootAbs, adapter.SkillDir(rootAbs)); err != nil {
			p.Actions = append(p.Actions, Action{Kind: Conflict, Target: adapter.SkillDir(rootAbs), Reason: err.Error()})
			continue
		}
		for _, ref := range desired.Skills {
			meta, err := registry.Get(ref.ID)
			if err != nil {
				return p, err
			}
			if info, statErr := os.Stat(registry.Store.ContentPath(ref.ID)); statErr != nil || !info.IsDir() {
				return p, fmt.Errorf("skill %q has metadata but its content is missing", ref.ID)
			}
			abs := filepath.Join(adapter.SkillDir(root), meta.Name)
			rel, err := filepath.Rel(root, abs)
			if err != nil {
				return p, err
			}
			rel = filepath.ToSlash(rel)
			newTargets[rel] = true
			p.Desired = append(p.Desired, model.ProjectLink{SkillID: ref.ID, Target: rel})
			p.State.Links = append(p.State.Links, model.ProjectLink{SkillID: ref.ID, Target: rel})
		}
	}
	p.State.Targets = make([]string, 0, len(targets))
	for _, t := range targets {
		p.State.Targets = append(p.State.Targets, t.ID())
	}
	sort.Strings(p.State.Targets)
	for target, oldLink := range old {
		if newTargets[target] {
			continue
		}
		path := filepath.Join(rootAbs, filepath.FromSlash(target))
		if managedLink(root, path, oldLink, registry.Store.Paths.SkillsDir()) {
			if _, err := os.Lstat(path); err == nil || os.IsNotExist(err) {
				p.Actions = append(p.Actions, Action{Kind: Remove, Target: target, Skill: oldLink.SkillID})
			}
		}
	}
	for _, link := range p.Desired {
		path := filepath.Join(root, filepath.FromSlash(link.Target))
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			p.Actions = append(p.Actions, Action{Kind: Create, Target: link.Target, Skill: link.SkillID})
			continue
		}
		if err != nil {
			return p, err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			p.Actions = append(p.Actions, Action{Kind: Conflict, Target: link.Target, Skill: link.SkillID, Reason: "target exists and is not a CMS-managed symlink"})
			continue
		}
		expected, _ := filepath.Abs(registry.Store.ContentPath(link.SkillID))
		actual := resolvedLink(path)
		if filepath.Clean(actual) == filepath.Clean(expected) {
			p.Actions = append(p.Actions, Action{Kind: Keep, Target: link.Target, Skill: link.SkillID})
			continue
		}
		if oldLink, ok := old[cleanRel(link.Target)]; ok && managedLink(root, path, oldLink, registry.Store.Paths.SkillsDir()) {
			p.Actions = append(p.Actions, Action{Kind: Remove, Target: link.Target, Skill: oldLink.SkillID})
			p.Actions = append(p.Actions, Action{Kind: Create, Target: link.Target, Skill: link.SkillID})
			continue
		}
		p.Actions = append(p.Actions, Action{Kind: Conflict, Target: link.Target, Skill: link.SkillID, Reason: "symlink exists but is not managed by CMS"})
	}
	return p, nil
}

func Apply(root string, p Plan, registry skills.Registry) error {
	if len(p.Conflicts()) > 0 {
		return fmt.Errorf("cannot apply plan while conflicts exist")
	}
	type removedLink struct{ path, target string }
	var created []string
	var removed []removedLink
	rollback := func() {
		for i := len(created) - 1; i >= 0; i-- {
			_ = os.Remove(created[i])
		}
		for i := len(removed) - 1; i >= 0; i-- {
			_ = os.Symlink(removed[i].target, removed[i].path)
		}
	}
	for _, a := range p.Actions {
		path := filepath.Join(root, filepath.FromSlash(a.Target))
		if !inside(root, path) {
			return fmt.Errorf("refusing target outside project root: %s", a.Target)
		}
		switch a.Kind {
		case Remove:
			info, statErr := os.Lstat(path)
			if statErr != nil && !os.IsNotExist(statErr) {
				rollback()
				return statErr
			}
			if statErr == nil && info.Mode()&os.ModeSymlink == 0 {
				rollback()
				return fmt.Errorf("refusing to remove a non-symlink target: %s", a.Target)
			}
			oldTarget := resolvedLink(path)
			if statErr == nil {
				oldTargetRaw, readErr := os.Readlink(path)
				if readErr != nil {
					rollback()
					return readErr
				}
				oldTarget = oldTargetRaw
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				rollback()
				return err
			}
			if statErr == nil {
				removed = append(removed, removedLink{path: path, target: oldTarget})
			}
		case Create:
			linkTarget := registry.Store.ContentPath(a.Skill)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				rollback()
				return err
			}
			if err := os.Symlink(linkTarget, path); err != nil {
				rollback()
				return err
			}
			created = append(created, path)
		}
	}
	if err := storage.SaveState(root, p.State); err != nil {
		rollback()
		return err
	}
	return nil
}

func validateSkillDir(root, dir string) error {
	rel, err := filepath.Rel(root, dir)
	if err != nil || !inside(root, dir) {
		return fmt.Errorf("harness skill directory escapes the project root")
	}
	current := root
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("harness skill directory contains an unmanaged symlink")
		}
		if !info.IsDir() {
			return fmt.Errorf("harness skill directory is not a directory")
		}
	}
	return nil
}

func managedLink(root, path string, state model.ProjectLink, skillsDir string) bool {
	if state.Target == "" {
		return false
	}
	actual := resolvedLink(path)
	if actual == "" {
		return false
	}
	abs, _ := filepath.Abs(actual)
	base, _ := filepath.Abs(skillsDir)
	rel, err := filepath.Rel(base, abs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func resolvedLink(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target)
}
func cleanRel(v string) string { return filepath.ToSlash(filepath.Clean(v)) }

func inside(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
