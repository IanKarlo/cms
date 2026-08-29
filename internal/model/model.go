package model

import "time"

const SchemaVersion = 1

type SkillSource struct {
	Type       string
	Repository string
	Path       string
	Ref        string
	Commit     string
}

type SkillMetadata struct {
	SchemaVersion  int
	ID             string
	Name           string
	Description    string
	InstalledAt    time.Time
	InstallPath    string
	Source         SkillSource
	HasScripts     bool
	HasExecutables bool
}

type SkillRef struct{ ID string }

type Context struct {
	SchemaVersion int
	Name          string
	Description   string
	Skills        []SkillRef
	MCPs          []string
}

type ProjectLink struct {
	SkillID string
	Target  string
}

// ProjectManifest is the versioned, shareable dependency declaration for a
// project. Unlike ProjectState, it contains enough source information to
// reinstall missing skills on another machine.
type ProjectManifest struct {
	SchemaVersion int
	Context       string
	Description   string
	Targets       []string
	Skills        []PinnedSkill
}

type PinnedSkill struct {
	ID          string
	Name        string
	Description string
	Source      SkillSource
}

type ProjectState struct {
	SchemaVersion int
	Context       string
	Targets       []string
	Links         []ProjectLink
}
