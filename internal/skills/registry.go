package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/storage"
)

type Registry struct{ Store storage.Registry }

func NewRegistry(paths storage.Paths) Registry {
	return Registry{Store: storage.Registry{Paths: paths}}
}

func SkillID(name string, source model.SkillSource) string {
	h := sha256.New()
	h.Write([]byte(source.Type + "\x00" + source.Repository + "\x00" + source.Path + "\x00" + source.Ref + "\x00" + source.Commit))
	sum := hex.EncodeToString(h.Sum(nil))
	return name + "@" + sum[:8]
}

func (r Registry) List() ([]model.SkillMetadata, error)       { return r.Store.List() }
func (r Registry) Get(id string) (model.SkillMetadata, error) { return r.Store.Get(id) }

// Remove deletes an installed skill. It is intentionally small and is used by
// batch installation rollback when a later skill in the same package fails.
func (r Registry) Remove(id string) error {
	if id == "" || filepath.Base(id) != id {
		return fmt.Errorf("invalid skill id %q", id)
	}
	lock, err := acquireLock(filepath.Join(r.Store.Paths.DataDir, ".lock"))
	if err != nil {
		return err
	}
	defer releaseLock(lock)
	if err := os.Remove(r.Store.MetadataPath(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.RemoveAll(filepath.Join(r.Store.Paths.SkillsDir(), id)); err != nil {
		return err
	}
	return nil
}

func (r Registry) InstallDirectory(sourceDir string, source model.SkillSource) (model.SkillMetadata, bool, error) {
	manifest, err := ValidateDirectory(sourceDir)
	if err != nil {
		return model.SkillMetadata{}, false, err
	}
	hasScripts, hasExec, err := InspectFiles(sourceDir)
	if err != nil {
		return model.SkillMetadata{}, false, err
	}
	id := SkillID(manifest.Name, source)
	if existing, err := r.Get(id); err == nil {
		return existing, true, nil
	}
	if err := os.MkdirAll(r.Store.Paths.SkillsDir(), 0o755); err != nil {
		return model.SkillMetadata{}, false, err
	}
	lock, err := acquireLock(filepath.Join(r.Store.Paths.DataDir, ".lock"))
	if err != nil {
		return model.SkillMetadata{}, false, err
	}
	defer releaseLock(lock)
	if existing, err := r.Get(id); err == nil {
		return existing, true, nil
	}
	tmp, err := os.MkdirTemp(r.Store.Paths.SkillsDir(), ".install-")
	if err != nil {
		return model.SkillMetadata{}, false, err
	}
	defer os.RemoveAll(tmp)
	content := filepath.Join(tmp, "content")
	if err := copyTree(sourceDir, content); err != nil {
		return model.SkillMetadata{}, false, err
	}
	if _, err := ValidateDirectory(content); err != nil {
		return model.SkillMetadata{}, false, err
	}
	final := filepath.Join(r.Store.Paths.SkillsDir(), id)
	if err := os.Rename(tmp, final); err != nil {
		return model.SkillMetadata{}, false, err
	}
	m := model.SkillMetadata{SchemaVersion: 1, ID: id, Name: manifest.Name, Description: manifest.Description, InstalledAt: time.Now().UTC(), InstallPath: filepath.Join("skills", id, "content"), Source: source, HasScripts: hasScripts, HasExecutables: hasExec}
	if err := r.Store.Save(m); err != nil {
		os.RemoveAll(final)
		return model.SkillMetadata{}, false, err
	}
	return m, false, nil
}

func acquireLock(path string) (*os.File, error) {
	for i := 0; i < 100; i++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			return f, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out waiting for CMS registry lock")
}
func releaseLock(f *os.File) { path := f.Name(); f.Close(); os.Remove(path) }

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if rel == "." {
			return os.MkdirAll(target, 0o755)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("skill package contains unsupported symlink %q", rel)
		}
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func NormalizePath(path string) string {
	return strings.Trim(filepath.ToSlash(filepath.Clean(path)), "/")
}
