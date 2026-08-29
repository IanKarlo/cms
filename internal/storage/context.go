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
	c := model.Context{SchemaVersion: 1, Name: n, Description: desc, MCPs: []string{}}
	for _, row := range d.arrayTables["skills"] {
		id, err := parseString(row["id"])
		if err != nil {
			return c, err
		}
		c.Skills = append(c.Skills, model.SkillRef{ID: id})
	}
	if ids := d.arrays[""]["mcps"]; ids != nil {
		c.MCPs = ids
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
	if len(c.Skills) == 0 {
		return fmt.Errorf("context %q must contain at least one skill", c.Name)
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
	fmt.Fprintf(&b, "schema_version = 1\nname = %s\ndescription = %s\nmcps = %s\n\n", quote(c.Name), quote(c.Description), array(c.MCPs))
	for _, ref := range c.Skills {
		fmt.Fprintf(&b, "[[skills]]\nid = %s\n\n", quote(ref.ID))
	}
	return AtomicWrite(path, []byte(b.String()), 0o644)
}
