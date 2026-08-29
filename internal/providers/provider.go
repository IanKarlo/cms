package providers

import (
	"context"

	"github.com/ikts/cms/internal/model"
)

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
