package harness

import (
	"os"
	"path/filepath"
)

type Adapter interface {
	ID() string
	Detect(projectRoot string) (bool, error)
	SkillDir(projectRoot string) string
	SupportsSymlink() bool
}

type Codex struct{}

func (Codex) ID() string { return "codex" }
func (Codex) Detect(root string) (bool, error) {
	_, err := os.Stat(filepath.Join(root, ".agents"))
	return err == nil || !os.IsNotExist(err), nil
}
func (Codex) SkillDir(root string) string { return filepath.Join(root, ".agents", "skills") }
func (Codex) SupportsSymlink() bool       { return true }

type Claude struct{}

func (Claude) ID() string { return "claude" }
func (Claude) Detect(root string) (bool, error) {
	_, err := os.Stat(filepath.Join(root, ".claude"))
	return err == nil || !os.IsNotExist(err), nil
}
func (Claude) SkillDir(root string) string { return filepath.Join(root, ".claude", "skills") }
func (Claude) SupportsSymlink() bool       { return true }

func Builtins() map[string]Adapter { return map[string]Adapter{"codex": Codex{}, "claude": Claude{}} }
