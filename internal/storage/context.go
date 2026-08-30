package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ikts/cms/internal/model"
)

var contextNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

func ValidateContextName(name string) error {
	if !contextNameRE.MatchString(name) {
		return fmt.Errorf("context name %q must use lowercase letters, numbers, _ or - and be at most 63 characters", name)
	}
	return nil
}

type ContextStore struct {
	Paths    Paths
	Registry Registry
}

func (s ContextStore) Path(name string) string {
	return filepath.Join(s.Paths.ContextsDir(), name+".toml")
}
func (s ContextStore) Get(name string) (model.Context, error) {
	b, err := os.ReadFile(s.Path(name))
	if os.IsNotExist(err) {
		return model.Context{}, fmt.Errorf("context %q was not found", name)
	}
	if err != nil {
		return model.Context{}, err
	}
	d, err := parseDocument(string(b))
	if err != nil {
		return model.Context{}, err
	}
	n, err := required(d, "", "name")
	if err != nil {
		return model.Context{}, err
	}
	desc, _ := optional(d, "", "description", "")
	version := intValue(d.scalars[""]["schema_version"], 1)
	c := model.Context{SchemaVersion: version, Name: n, Description: desc, MCPs: []string{}}
	for _, row := range d.arrayTables["skills"] {
		id, err := parseString(row["id"])
		if err != nil {
			return c, err
		}
		c.Skills = append(c.Skills, model.SkillRef{ID: id})
	}
	if ids := d.arrays[""]["mcps"]; ids != nil {
		c.MCPs = ids
		for _, id := range ids {
			c.MCPRefs = append(c.MCPRefs, model.MCPRef{ID: id})
		}
	}
	for _, row := range d.arrayTables["mcps"] {
		id, e := parseString(row["id"])
		if e != nil || id == "" {
			return c, fmt.Errorf("invalid context MCP reference")
		}
		ref := model.MCPRef{ID: id}
		ref.Alias, _ = parseString(row["alias"])
		for _, list := range []string{"allow", "deny"} {
			if raw := row[list]; raw != "" {
				vals, pe := parseArray(raw)
				if pe != nil {
					return c, pe
				}
				if list == "allow" {
					ref.Tools.Allow = vals
				} else {
					ref.Tools.Deny = vals
				}
			}
		}
		c.MCPRefs = append(c.MCPRefs, ref)
		c.MCPs = append(c.MCPs, id)
	}
	if c.SchemaVersion == 0 {
		c.SchemaVersion = 1
	}
	return c, nil
}

func (s ContextStore) List() ([]model.Context, error) {
	entries, err := os.ReadDir(s.Paths.ContextsDir())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var out []model.Context
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		c, err := s.Get(strings.TrimSuffix(e.Name(), ".toml"))
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s ContextStore) Validate(c model.Context) error {
	if err := ValidateContextName(c.Name); err != nil {
		return err
	}
	if len(c.Skills) == 0 && len(c.MCPRefs) == 0 && len(c.MCPs) == 0 {
		return fmt.Errorf("context %q must contain at least one skill or MCP", c.Name)
	}
	seenIDs, seenNames := map[string]bool{}, map[string]bool{}
	for _, ref := range c.Skills {
		if seenIDs[ref.ID] {
			return fmt.Errorf("skill %q is listed more than once", ref.ID)
		}
		seenIDs[ref.ID] = true
		m, err := s.Registry.Get(ref.ID)
		if err != nil {
			return err
		}
		if seenNames[m.Name] {
			return fmt.Errorf("skills in a context cannot share harness name %q", m.Name)
		}
		seenNames[m.Name] = true
	}
	mcpRegistry := MCPRegistry{Paths: s.Paths}
	refs := c.MCPRefs
	if len(refs) == 0 {
		for _, id := range c.MCPs {
			refs = append(refs, model.MCPRef{ID: id})
		}
	}
	seenMCP, seenNames := map[string]bool{}, map[string]bool{}
	for _, ref := range refs {
		if ref.ID == "" || seenMCP[ref.ID] {
			return fmt.Errorf("MCP %q is listed more than once", ref.ID)
		}
		seenMCP[ref.ID] = true
		m, err := mcpRegistry.Get(ref.ID)
		if err != nil {
			return err
		}
		if err := model.ValidateMCPName(ref.Alias); ref.Alias != "" && err != nil {
			return err
		}
		if err := ref.Tools.Validate(); err != nil {
			return err
		}
		name := m.ExposedName(ref)
		if seenNames[name] {
			return fmt.Errorf("MCPs in a context cannot share exposed name %q", name)
		}
		seenNames[name] = true
		for _, deny := range ref.Tools.Deny {
			for _, allow := range ref.Tools.Allow {
				if deny == allow { /* valid; adapter reports conflict/warning */
				}
			}
		}
	}
	return nil
}

func (s ContextStore) Save(c model.Context) error {
	if err := s.Validate(c); err != nil {
		return err
	}
	return s.write(c, s.Path(c.Name))
}

// Rename updates the content at the old path before atomically moving that
// single file to its new name. A failed write leaves the original context
// untouched; a failed rename leaves a complete, retryable context at oldName.
func (s ContextStore) Rename(oldName string, c model.Context) error {
	if err := s.Validate(c); err != nil {
		return err
	}
	if oldName == c.Name {
		return s.Save(c)
	}
	if _, err := os.Stat(s.Path(c.Name)); err == nil {
		return fmt.Errorf("context %q already exists", c.Name)
	}
	if err := s.write(c, s.Path(oldName)); err != nil {
		return err
	}
	return os.Rename(s.Path(oldName), s.Path(c.Name))
}

func (s ContextStore) write(c model.Context, path string) error {
	var b strings.Builder
	version := c.SchemaVersion
	if version < 2 {
		version = 2
	}
	fmt.Fprintf(&b, "schema_version = %d\nname = %s\ndescription = %s\n\n", version, quote(c.Name), quote(c.Description))
	for _, ref := range c.Skills {
		fmt.Fprintf(&b, "[[skills]]\nid = %s\n\n", quote(ref.ID))
	}
	refs := c.MCPRefs
	if len(refs) == 0 {
		for _, id := range c.MCPs {
			refs = append(refs, model.MCPRef{ID: id})
		}
	}
	for _, ref := range refs {
		fmt.Fprintf(&b, "[[mcps]]\nid = %s\nalias = %s\nallow = %s\ndeny = %s\n\n", quote(ref.ID), quote(ref.Alias), array(ref.Tools.Allow), array(ref.Tools.Deny))
	}
	return AtomicWrite(path, []byte(b.String()), 0o644)
}
