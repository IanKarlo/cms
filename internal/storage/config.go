package storage

import (
	"errors"
	"os"
	"path/filepath"
)

type Config struct {
	SchemaVersion     int
	DefaultTargets    []string
	GitHubSearchLimit int
	GitHubCacheTTL    string
	LinkMode          string
}

func LoadConfig(p Paths) (Config, error) {
	c := Config{SchemaVersion: 1, DefaultTargets: []string{"codex", "claude"}, GitHubSearchLimit: 30, GitHubCacheTTL: "10m", LinkMode: "symlink"}
	b, err := os.ReadFile(p.ConfigFile())
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	d, err := parseDocument(string(b))
	if err != nil {
		return c, err
	}
	if v, ok := d.scalars[""]["schema_version"]; ok {
		c.SchemaVersion = intValue(v, 1)
	}
	if v := d.arrays[""]["default_targets"]; v != nil {
		c.DefaultTargets = v
	}
	if v, ok := d.scalars["github"]["search_limit"]; ok {
		c.GitHubSearchLimit = intValue(v, c.GitHubSearchLimit)
	}
	if v, ok := d.scalars["github"]["cache_ttl"]; ok {
		c.GitHubCacheTTL, _ = parseString(v)
	}
	if v, ok := d.scalars["linking"]["mode"]; ok {
		c.LinkMode, _ = parseString(v)
	}
	return c, nil
}

func EnsureDirs(p Paths) error {
	for _, dir := range []string{p.ConfigDir, p.DataDir, p.ContextsDir(), p.SkillsDir(), p.MetadataDir(), p.CacheDir} {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cms-tmp-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(perm); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
