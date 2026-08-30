package providers

import (
	"context"

	"github.com/ikts/cms/internal/model"
)

type MCPProvider interface {
	Search(context.Context, string, int) ([]MCPSearchResult, error)
	Resolve(context.Context, MCPProviderRef) ([]MCPVariant, error)
}
type MCPProviderRef struct{ Name, Version, Variant string }
type MCPRegistryPackage struct {
	RegistryType string
	Identifier   string
	Version      string
	RuntimeHint  string
}
type MCPRegistryRemote struct {
	Type string
	URL  string
}
type MCPRegistryIcon struct {
	Source   string
	MimeType string
	Sizes    []string
	Theme    string
}
type MCPSearchResult struct {
	Name, Title, Description, Version             string
	WebsiteURL, RepositoryURL, RepositorySource   string
	Status, StatusMessage, PublishedAt, UpdatedAt string
	IsLatest                                      bool
	Packages                                      []MCPRegistryPackage
	Remotes                                       []MCPRegistryRemote
	Icons                                         []MCPRegistryIcon
}
type MCPVariant struct {
	Name, Description, Version, Variant string
	Source                              model.MCPSource
	Transport                           model.MCPTransport
	Command                             *model.MCPCommand
	Remote                              *model.MCPRemote
	Requirements                        []model.MCPRequirement
	Reproducible                        bool
}

// SkillProvider keeps the application independent from a specific catalog.
// The limit argument lets a UI or config constrain provider work without
// changing the domain model.
type SkillProvider interface {
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
	Resolve(ctx context.Context, source model.SkillSource) (model.SkillSource, error)
	Download(ctx context.Context, source model.SkillSource, dest string) error
}

// BatchSkillProvider is optionally implemented by providers that can install
// every skill in a source package when no specific skill path was requested.
// SkillProvider remains small so providers that only expose single-skill
// downloads do not need to implement batch behavior.
type BatchSkillProvider interface {
	DownloadAll(ctx context.Context, source model.SkillSource, dest string) ([]DownloadedSkill, error)
}

type DownloadedSkill struct {
	Source    model.SkillSource
	Directory string
}

type SearchResult struct {
	Name, Description, Repository, Path, Ref, Commit, License string
	Score                                                     int
}
