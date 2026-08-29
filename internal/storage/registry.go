package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ikts/cms/internal/model"
)

type Registry struct{ Paths Paths }

func (r Registry) ContentPath(id string) string {
	return filepath.Join(r.Paths.SkillsDir(), id, "content")
}
func (r Registry) MetadataPath(id string) string {
	return filepath.Join(r.Paths.MetadataDir(), id+".toml")
}

func (r Registry) Get(id string) (model.SkillMetadata, error) {
	b, err := os.ReadFile(r.MetadataPath(id))
	if err != nil {
		return model.SkillMetadata{}, fmt.Errorf("skill %q not found", id)
	}
	d, err := parseDocument(string(b))
	if err != nil {
		return model.SkillMetadata{}, err
	}
	name, err := required(d, "", "name")
	if err != nil {
		return model.SkillMetadata{}, err
	}
	desc, _ := optional(d, "", "description", "")
	idValue, err := required(d, "", "id")
	if err != nil {
		return model.SkillMetadata{}, err
	}
	installed, _ := optional(d, "", "installed_at", "")
	tm, _ := time.Parse(time.RFC3339, installed)
	installPath, _ := optional(d, "", "install_path", filepath.Join("skills", idValue, "content"))
	sp := model.SkillSource{}
	sp.Type, _ = optional(d, "source", "type", "")
	sp.Repository, _ = optional(d, "source", "repository", "")
	sp.Path, _ = optional(d, "source", "path", "")
	sp.Ref, _ = optional(d, "source", "ref", "")
	sp.Commit, _ = optional(d, "source", "commit", "")
	return model.SkillMetadata{SchemaVersion: 1, ID: idValue, Name: name, Description: desc, InstalledAt: tm, InstallPath: installPath, Source: sp, HasScripts: boolValue(d.scalars[""]["has_scripts"]), HasExecutables: boolValue(d.scalars[""]["has_executables"])}, nil
}

func (r Registry) List() ([]model.SkillMetadata, error) {
	entries, err := os.ReadDir(r.Paths.MetadataDir())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var out []model.SkillMetadata
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		s, err := r.Get(strings.TrimSuffix(e.Name(), ".toml"))
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r Registry) Save(m model.SkillMetadata) error {
	data := fmt.Sprintf("schema_version = 1\nid = %s\nname = %s\ndescription = %s\ninstalled_at = %s\ninstall_path = %s\nhas_scripts = %t\nhas_executables = %t\n\n[source]\ntype = %s\nrepository = %s\npath = %s\nref = %s\ncommit = %s\n", quote(m.ID), quote(m.Name), quote(m.Description), quote(m.InstalledAt.UTC().Format(time.RFC3339)), quote(filepath.Join("skills", m.ID, "content")), m.HasScripts, m.HasExecutables, quote(m.Source.Type), quote(m.Source.Repository), quote(m.Source.Path), quote(m.Source.Ref), quote(m.Source.Commit))
	return AtomicWrite(r.MetadataPath(m.ID), []byte(data), 0o644)
}
