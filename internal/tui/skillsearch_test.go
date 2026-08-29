package tui

import (
	"context"
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/providers"
	"github.com/ikts/cms/internal/skills"
)

type searchTestProvider struct{}

func (searchTestProvider) Search(_ context.Context, _ string, _ int) ([]providers.SearchResult, error) {
	return nil, nil
}

func (searchTestProvider) Resolve(context.Context, model.SkillSource) (model.SkillSource, error) {
	return model.SkillSource{}, nil
}

func (searchTestProvider) Download(context.Context, model.SkillSource, string) error { return nil }

func TestSkillSearchAcceptsSpaces(t *testing.T) {
	m := NewSkillSearchModelWithLimit(searchTestProvider{}, skills.Registry{}, 10)
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("go")},
		{Type: tea.KeySpace, Runes: []rune(" ")},
		{Type: tea.KeyRunes, Runes: []rune("testing")},
	} {
		updated, _ := m.Update(key)
		m = updated.(*SkillSearchModel)
	}
	if m.Query != "go testing" {
		t.Fatalf("query = %q, want %q", m.Query, "go testing")
	}
}

func TestSkillSearchAcceptsJAndK(t *testing.T) {
	m := NewSkillSearchModelWithLimit(searchTestProvider{}, skills.Registry{}, 10)
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("j")},
		{Type: tea.KeySpace, Runes: []rune(" ")},
		{Type: tea.KeyRunes, Runes: []rune("k")},
	} {
		updated, _ := m.Update(key)
		m = updated.(*SkillSearchModel)
	}
	if m.Query != "j k" {
		t.Fatalf("query = %q, want %q", m.Query, "j k")
	}
}
