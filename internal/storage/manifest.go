package storage

import (
	"encoding/json"
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
	for _, row := range d.arrayTables["mcps"] {
		var pinned model.PinnedMCP
		if raw := row["metadata_json"]; raw != "" {
			value, _ := parseString(raw)
			if err := json.Unmarshal([]byte(value), &pinned.Metadata); err != nil {
				return model.ProjectManifest{}, err
			}
		} else {
			pinned.Metadata.ID = mustManifestString(row, "id")
			pinned.Metadata.Name = mustManifestString(row, "name")
			pinned.Metadata.Description = mustManifestString(row, "description")
			pinned.Metadata.Source.Type = model.MCPSourceType(mustManifestString(row, "source_type"))
			pinned.Metadata.Source.RegistryName = mustManifestString(row, "registry_name")
			pinned.Metadata.Source.Version = mustManifestString(row, "version")
			pinned.Metadata.Source.Variant = mustManifestString(row, "variant")
			pinned.Metadata.Transport = model.MCPTransport(mustManifestString(row, "transport"))
			pinned.Metadata.Reproducible = boolValue(row["reproducible"])
		}
		manifest.MCPs = append(manifest.MCPs, pinned)
	}
	for _, row := range d.arrayTables["context_mcps"] {
		ref := model.MCPRef{ID: mustManifestString(row, "id"), Alias: mustManifestString(row, "alias")}
		if row["allow"] != "" {
			ref.Tools.Allow, _ = parseArray(row["allow"])
		}
		if row["deny"] != "" {
			ref.Tools.Deny, _ = parseArray(row["deny"])
		}
		manifest.ContextMCPs = append(manifest.ContextMCPs, ref)
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
	version := manifest.SchemaVersion
	if version < 2 {
		version = 2
	}
	content += fmt.Sprintf("schema_version = %d\ncontext = %s\ndescription = %s\ntargets = %s\n\n", version, quote(manifest.Context), quote(manifest.Description), array(manifest.Targets))
	for _, skill := range manifest.Skills {
		content += fmt.Sprintf("[[skills]]\nid = %s\nname = %s\ndescription = %s\ntype = %s\nrepository = %s\npath = %s\nref = %s\ncommit = %s\n\n", quote(skill.ID), quote(skill.Name), quote(skill.Description), quote(skill.Source.Type), quote(skill.Source.Repository), quote(skill.Source.Path), quote(skill.Source.Ref), quote(skill.Source.Commit))
	}
	for _, p := range manifest.MCPs {
		raw, _ := json.Marshal(p.Metadata)
		content += fmt.Sprintf("[[mcps]]\nid = %s\nname = %s\ndescription = %s\nsource_type = %s\nregistry_name = %s\nversion = %s\nvariant = %s\ntransport = %s\nreproducible = %t\nmetadata_json = %s\n\n", quote(p.Metadata.ID), quote(p.Metadata.Name), quote(p.Metadata.Description), quote(string(p.Metadata.Source.Type)), quote(p.Metadata.Source.RegistryName), quote(p.Metadata.Source.Version), quote(p.Metadata.Source.Variant), quote(string(p.Metadata.Transport)), p.Metadata.Reproducible, quote(string(raw)))
	}
	for _, r := range manifest.ContextMCPs {
		content += fmt.Sprintf("[[context_mcps]]\nid = %s\nalias = %s\nallow = %s\ndeny = %s\n\n", quote(r.ID), quote(r.Alias), array(r.Tools.Allow), array(r.Tools.Deny))
	}
	return AtomicWrite(ManifestPath(projectRoot), []byte(content), 0o644)
}

func ValidateManifest(manifest model.ProjectManifest) error {
	if manifest.SchemaVersion != 1 && manifest.SchemaVersion != 2 {
		return fmt.Errorf("unsupported cms.toml schema_version %d", manifest.SchemaVersion)
	}
	if manifest.Context == "" {
		return errors.New("cms.toml context is required")
	}
	if err := ValidateContextName(manifest.Context); err != nil {
		return err
	}
	if len(manifest.Skills) == 0 && len(manifest.MCPs) == 0 {
		return errors.New("cms.toml must contain at least one skill or MCP")
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
	seenMCP := map[string]bool{}
	for _, p := range manifest.MCPs {
		if p.Metadata.ID == "" || p.Metadata.Name == "" {
			return errors.New("each cms.toml MCP requires id and name")
		}
		if seenMCP[p.Metadata.ID] {
			return fmt.Errorf("MCP %q is listed more than once in cms.toml", p.Metadata.ID)
		}
		seenMCP[p.Metadata.ID] = true
		if p.Metadata.CanonicalID() != p.Metadata.ID {
			return fmt.Errorf("MCP %q has an ID that does not match its definition", p.Metadata.ID)
		}
	}
	seenRef := map[string]bool{}
	for _, r := range manifest.ContextMCPs {
		if r.ID == "" || seenRef[r.ID] {
			return fmt.Errorf("MCP context reference %q is duplicated", r.ID)
		}
		seenRef[r.ID] = true
		if r.Alias != "" {
			if err := model.ValidateMCPName(r.Alias); err != nil {
				return err
			}
		}
		if err := r.Tools.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func mustManifestString(row map[string]string, key string) string {
	s, _ := parseString(row[key])
	return s
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
