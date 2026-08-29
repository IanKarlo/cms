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
	s := model.ProjectState{SchemaVersion: 1, Context: c, Targets: d.arrays[""]["targets"]}
	for _, row := range d.arrayTables["links"] {
		skill, e1 := parseString(row["skill_id"])
		target, e2 := parseString(row["target"])
		if e1 != nil || e2 != nil {
			return s, fmt.Errorf("invalid state link")
		}
		s.Links = append(s.Links, model.ProjectLink{SkillID: skill, Target: target})
	}
	return s, nil
}

func SaveState(projectRoot string, s model.ProjectState) error {
	b := fmt.Sprintf("schema_version = 1\ncontext = %s\ntargets = %s\n\n", quote(s.Context), array(s.Targets))
	for _, link := range s.Links {
		b += fmt.Sprintf("[[links]]\nskill_id = %s\ntarget = %s\n\n", quote(link.SkillID), quote(link.Target))
	}
	return AtomicWrite(StatePath(projectRoot), []byte(b), 0o644)
}
