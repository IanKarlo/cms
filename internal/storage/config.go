package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	SchemaVersion          int
	DefaultTargets         []string
	GitHubSearchLimit      int
	GitHubCacheTTL         string
	LinkMode               string
	MCPRegistryBaseURL     string
	MCPRegistrySearchLimit int
	MCPRegistryCacheTTL    string
}

func LoadConfig(p Paths) (Config, error) {
	// An empty DefaultTargets means "all registered harnesses". This keeps
	// existing installations compatible while allowing users to opt into a
	// smaller persistent target set through the CLI.
	c := Config{SchemaVersion: 1, GitHubSearchLimit: 30, GitHubCacheTTL: "10m", LinkMode: "symlink", MCPRegistryBaseURL: "https://registry.modelcontextprotocol.io", MCPRegistrySearchLimit: 30, MCPRegistryCacheTTL: "10m"}
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
	if v, ok := d.scalars["mcp_registry"]["base_url"]; ok {
		c.MCPRegistryBaseURL, _ = parseString(v)
	}
	if v, ok := d.scalars["mcp_registry"]["search_limit"]; ok {
		c.MCPRegistrySearchLimit = intValue(v, c.MCPRegistrySearchLimit)
	}
	if v, ok := d.scalars["mcp_registry"]["cache_ttl"]; ok {
		c.MCPRegistryCacheTTL, _ = parseString(v)
	}
	return c, nil
}

// SaveDefaultTargets updates only the top-level default_targets setting. The
// config file is user-editable, so unrelated settings, comments and layout
// must survive this operation.
func SaveDefaultTargets(p Paths, targets []string) error {
	path := p.ConfigFile()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		var content strings.Builder
		content.WriteString("schema_version = 1\n")
		if len(targets) > 0 {
			content.WriteString("default_targets = ")
			content.WriteString(array(targets))
			content.WriteByte('\n')
		}
		return AtomicWrite(path, []byte(content.String()), 0o644)
	}
	if err != nil {
		return err
	}
	if _, err := parseDocument(string(b)); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	lines := strings.SplitAfter(string(b), "\n")
	keyFound := false
	section := ""
	for i, raw := range lines {
		line := strings.TrimSpace(stripComment(strings.TrimSuffix(raw, "\n")))
		if strings.HasPrefix(line, "[") {
			if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
				section = strings.TrimSpace(line[2 : len(line)-2])
			} else if strings.HasSuffix(line, "]") {
				section = strings.TrimSpace(line[1 : len(line)-1])
			}
			continue
		}
		if section != "" || !strings.HasPrefix(line, "default_targets") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 || strings.TrimSpace(line[:eq]) != "default_targets" {
			continue
		}
		keyFound = true
		if len(targets) == 0 {
			lines[i] = ""
		} else {
			indent := raw[:len(raw)-len(strings.TrimLeft(raw, " \t"))]
			withoutNewline := strings.TrimSuffix(raw, "\n")
			comment := withoutNewline[len(stripComment(withoutNewline)):]
			newline := ""
			if strings.HasSuffix(raw, "\n") {
				newline = "\n"
			}
			lines[i] = indent + "default_targets = " + array(targets) + comment + newline
		}
		break
	}
	if !keyFound && len(targets) > 0 {
		insertAt := len(lines)
		for i, raw := range lines {
			line := strings.TrimSpace(stripComment(strings.TrimSuffix(raw, "\n")))
			if strings.HasPrefix(line, "[") {
				insertAt = i
				break
			}
		}
		entry := "default_targets = " + array(targets) + "\n"
		lines = append(lines, "")
		if insertAt > 0 && !strings.HasSuffix(lines[insertAt-1], "\n") {
			lines[insertAt-1] += "\n"
		}
		copy(lines[insertAt+1:], lines[insertAt:])
		lines[insertAt] = entry
	}
	return AtomicWrite(path, []byte(strings.Join(lines, "")), 0o644)
}

func EnsureDirs(p Paths) error {
	for _, dir := range []string{p.ConfigDir, p.DataDir, p.ContextsDir(), p.SkillsDir(), p.MetadataDir(), p.MCPDir(), p.CacheDir} {
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
