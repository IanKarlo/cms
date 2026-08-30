package storage

import (
	"os"
	"path/filepath"
)

type Paths struct{ ConfigDir, DataDir, CacheDir string }

func ResolvePaths() (Paths, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, err
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return Paths{}, homeErr
		}
		data = filepath.Join(home, ".local", "share")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return Paths{}, err
	}
	if v := os.Getenv("CMS_CONFIG_DIR"); v != "" {
		config = v
	}
	if v := os.Getenv("CMS_DATA_DIR"); v != "" {
		data = v
	}
	if v := os.Getenv("CMS_CACHE_DIR"); v != "" {
		cache = v
	}
	return Paths{ConfigDir: filepath.Join(config, "cms"), DataDir: filepath.Join(data, "cms"), CacheDir: filepath.Join(cache, "cms")}, nil
}

func (p Paths) ContextsDir() string { return filepath.Join(p.DataDir, "contexts") }
func (p Paths) SkillsDir() string   { return filepath.Join(p.DataDir, "skills") }
func (p Paths) MetadataDir() string { return filepath.Join(p.DataDir, "skill-metadata") }
func (p Paths) MCPDir() string      { return filepath.Join(p.DataDir, "mcps") }
func (p Paths) ConfigFile() string  { return filepath.Join(p.ConfigDir, "config.toml") }
