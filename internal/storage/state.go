package storage

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ikts/cms/internal/model"
)

func StatePath(projectRoot string) string { return filepath.Join(projectRoot, ".cms", "state.toml") }

func LoadState(projectRoot string) (model.ProjectState, error) {
	b, err := os.ReadFile(StatePath(projectRoot))
	if os.IsNotExist(err) {
		return model.ProjectState{SchemaVersion: 1}, nil
	}
	if err != nil {
		return model.ProjectState{}, err
	}
	d, err := parseDocument(string(b))
	if err != nil {
		return model.ProjectState{}, err
	}
	c, _ := optional(d, "", "context", "")
	version := intValue(d.scalars[""]["schema_version"], 1)
	s := model.ProjectState{SchemaVersion: version, Context: c, Targets: d.arrays[""]["targets"]}
	for _, row := range d.arrayTables["links"] {
		skill, e1 := parseString(row["skill_id"])
		target, e2 := parseString(row["target"])
		if e1 != nil || e2 != nil {
			return s, fmt.Errorf("invalid state link")
		}
		s.Links = append(s.Links, model.ProjectLink{SkillID: skill, Target: target})
	}
	for _, row := range d.arrayTables["mcp_entries"] {
		e := model.MCPStateEntry{}
		e.MCPID, _ = parseString(row["mcp_id"])
		e.Target, _ = parseString(row["target"])
		e.Scope, _ = parseString(row["scope"])
		e.Name, _ = parseString(row["name"])
		e.ConfigPath, _ = parseString(row["config_path"])
		e.Fingerprint, _ = parseString(row["fingerprint"])
		e.ToolFilterStatus, _ = parseString(row["tool_filter_status"])
		e.ManagedKeys, _ = parseArray(row["managed_keys"])
		if e.MCPID != "" {
			s.MCPEntries = append(s.MCPEntries, e)
		}
	}
	return s, nil
}

func SaveState(projectRoot string, s model.ProjectState) error {
	b := fmt.Sprintf("schema_version = 2\ncontext = %s\ntargets = %s\n\n", quote(s.Context), array(s.Targets))
	for _, link := range s.Links {
		b += fmt.Sprintf("[[links]]\nskill_id = %s\ntarget = %s\n\n", quote(link.SkillID), quote(link.Target))
	}
	for _, e := range s.MCPEntries {
		b += fmt.Sprintf("[[mcp_entries]]\nmcp_id = %s\ntarget = %s\nscope = %s\nname = %s\nconfig_path = %s\nfingerprint = %s\ntool_filter_status = %s\nmanaged_keys = %s\n\n", quote(e.MCPID), quote(e.Target), quote(e.Scope), quote(e.Name), quote(e.ConfigPath), quote(e.Fingerprint), quote(e.ToolFilterStatus), array(e.ManagedKeys))
	}
	return AtomicWrite(StatePath(projectRoot), []byte(b), 0o644)
}
