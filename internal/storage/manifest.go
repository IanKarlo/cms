package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ikts/cms/internal/model"
)

func ManifestPath(projectRoot string) string { return filepath.Join(projectRoot, "cms.toml") }

func LoadManifest(projectRoot string) (model.ProjectManifest, error) {
	b, err := os.ReadFile(ManifestPath(projectRoot))
	if err != nil {
		return model.ProjectManifest{}, err
	}
	d, err := parseDocument(string(b))
	if err != nil {
		return model.ProjectManifest{}, err
	}
	manifest := model.ProjectManifest{SchemaVersion: 1}
	if value, ok := d.scalars[""]["schema_version"]; ok {
		manifest.SchemaVersion = intValue(value, 1)
	}
	if manifest.Context, err = required(d, "", "context"); err != nil {
		return model.ProjectManifest{}, err
	}
	manifest.Description, _ = optional(d, "", "description", "")
	manifest.Targets = append([]string(nil), d.arrays[""]["targets"]...)
	for _, row := range d.arrayTables["skills"] {
		pinned, rowErr := parsePinnedSkill(row)
		if rowErr != nil {
			return model.ProjectManifest{}, rowErr
		}
		manifest.Skills = append(manifest.Skills, pinned)
	}
	if err := ValidateManifest(manifest); err != nil {
		return model.ProjectManifest{}, err
	}
	return manifest, nil
}

func SaveManifest(projectRoot string, manifest model.ProjectManifest) error {
	if err := ValidateManifest(manifest); err != nil {
		return err
	}
	sort.Strings(manifest.Targets)
	var content string
	content += fmt.Sprintf("schema_version = %d\ncontext = %s\ndescription = %s\ntargets = %s\n\n", manifest.SchemaVersion, quote(manifest.Context), quote(manifest.Description), array(manifest.Targets))
	for _, skill := range manifest.Skills {
		content += fmt.Sprintf("[[skills]]\nid = %s\nname = %s\ndescription = %s\ntype = %s\nrepository = %s\npath = %s\nref = %s\ncommit = %s\n\n", quote(skill.ID), quote(skill.Name), quote(skill.Description), quote(skill.Source.Type), quote(skill.Source.Repository), quote(skill.Source.Path), quote(skill.Source.Ref), quote(skill.Source.Commit))
	}
	return AtomicWrite(ManifestPath(projectRoot), []byte(content), 0o644)
}

func ValidateManifest(manifest model.ProjectManifest) error {
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("unsupported cms.toml schema_version %d", manifest.SchemaVersion)
	}
	if manifest.Context == "" {
		return errors.New("cms.toml context is required")
	}
	if err := ValidateContextName(manifest.Context); err != nil {
		return err
	}
	if len(manifest.Skills) == 0 {
		return errors.New("cms.toml must contain at least one skill")
	}
	seen := map[string]bool{}
	for _, skill := range manifest.Skills {
		if skill.ID == "" || skill.Name == "" {
			return errors.New("each cms.toml skill requires id and name")
		}
		if seen[skill.ID] {
			return fmt.Errorf("skill %q is listed more than once in cms.toml", skill.ID)
		}
		seen[skill.ID] = true
		if skill.Source.Type == "" || skill.Source.Repository == "" || skill.Source.Commit == "" {
			return fmt.Errorf("skill %q must have a pinned source and commit", skill.ID)
		}
	}
	return nil
}

func parsePinnedSkill(row map[string]string) (model.PinnedSkill, error) {
	get := func(key string) (string, error) {
		value, ok := row[key]
		if !ok {
			return "", fmt.Errorf("cms.toml skill is missing %s", key)
		}
		return parseString(value)
	}
	var skill model.PinnedSkill
	var err error
	if skill.ID, err = get("id"); err != nil {
		return skill, err
	}
	if skill.Name, err = get("name"); err != nil {
		return skill, err
	}
	skill.Description, err = get("description")
	if err != nil {
		return skill, err
	}
	if skill.Source.Type, err = get("type"); err != nil {
		return skill, err
	}
	if skill.Source.Repository, err = get("repository"); err != nil {
		return skill, err
	}
	if skill.Source.Path, err = get("path"); err != nil {
		return skill, err
	}
	if skill.Source.Ref, err = get("ref"); err != nil {
		return skill, err
	}
	if skill.Source.Commit, err = get("commit"); err != nil {
		return skill, err
	}
	return skill, nil
}
