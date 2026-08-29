package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var skillNameRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Manifest struct {
	Name            string
	Description     string
	HasAllowedTools bool
}

func ValidateName(name string) error {
	if len(name) < 1 || len(name) > 64 {
		return fmt.Errorf("SKILL.md field \"name\" must contain 1 to 64 characters")
	}
	if !skillNameRE.MatchString(name) {
		return fmt.Errorf("SKILL.md field \"name\" must use lowercase letters, numbers and single hyphens")
	}
	return nil
}

func ParseManifest(data []byte) (Manifest, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return Manifest{}, fmt.Errorf("SKILL.md is missing YAML frontmatter")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" || strings.TrimSpace(lines[i]) == "..." {
			end = i
			break
		}
	}
	if end < 0 {
		return Manifest{}, fmt.Errorf("SKILL.md has unterminated YAML frontmatter")
	}
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &raw); err != nil {
		return Manifest{}, fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	var fields struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &fields); err != nil {
		return Manifest{}, fmt.Errorf("invalid SKILL.md metadata: %w", err)
	}
	name, description := fields.Name, fields.Description
	if err := ValidateName(name); err != nil {
		return Manifest{}, err
	}
	if strings.TrimSpace(description) == "" {
		return Manifest{}, fmt.Errorf("SKILL.md is missing required frontmatter field \"description\"")
	}
	_, hasTools := raw["allowed-tools"]
	return Manifest{Name: name, Description: description, HasAllowedTools: hasTools}, nil
}

func ValidateDirectory(root string) (Manifest, error) {
	info, err := os.Stat(root)
	if err != nil {
		return Manifest{}, err
	}
	if !info.IsDir() {
		return Manifest{}, fmt.Errorf("skill source is not a directory")
	}
	manifestPath := filepath.Join(root, "SKILL.md")
	manifestInfo, err := os.Lstat(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{}, fmt.Errorf("SKILL.md is missing")
		}
		return Manifest{}, err
	}
	if manifestInfo.Mode()&os.ModeSymlink != 0 {
		return Manifest{}, fmt.Errorf("SKILL.md must be a regular file, not a symlink")
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{}, fmt.Errorf("SKILL.md is missing")
		}
		return Manifest{}, err
	}
	return ParseManifest(data)
}

func InspectFiles(root string) (hasScripts, hasExecutables bool, err error) {
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		if info.IsDir() {
			if rel == "scripts" || strings.HasPrefix(rel, "scripts"+string(filepath.Separator)) {
				hasScripts = true
			}
			return nil
		}
		if strings.HasPrefix(rel, "scripts"+string(filepath.Separator)) || rel == "scripts" {
			hasScripts = true
		}
		if info.Mode()&0o111 != 0 {
			hasExecutables = true
		}
		return nil
	})
	return
}
