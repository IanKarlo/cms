package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/ikts/cms/internal/model"
)

func TestContextListViewHasTableAndDetails(t *testing.T) {
	m := NewContextListModelWithSkillNames([]model.Context{{
		Name:        "backend",
		Description: "Backend development context",
		Skills:      []model.SkillRef{{ID: "go-testing@abc123"}},
	}}, map[string]string{"go-testing@abc123": "go-testing"})
	m.Width = 100
	view := m.View()
	for _, expected := range []string{"┌", "NAME", "SKILLS", "backend", "Backend development context", "Details", "go-testing", "└"} {
		if !strings.Contains(view, expected) {
			t.Errorf("view does not contain %q:\n%s", expected, view)
		}
	}
}

func TestContextListFiltersByName(t *testing.T) {
	m := NewContextListModel([]model.Context{{Name: "backend"}, {Name: "frontend"}})
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("back")},
	} {
		updated, _ := m.Update(key)
		m = updated.(*ContextListModel)
	}
	if items := m.filtered(); len(items) != 1 || items[0].Name != "backend" {
		t.Fatalf("filtered contexts = %#v", items)
	}
}

func TestContextListNavigatesWithArrowsAndVimKeys(t *testing.T) {
	m := NewContextListModel([]model.Context{{Name: "one"}, {Name: "two"}})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*ContextListModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(*ContextListModel)
	if m.Cursor != 1 {
		t.Fatalf("cursor after down = %d, want 1", m.Cursor)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(*ContextListModel)
	if m.Cursor != 0 {
		t.Fatalf("cursor after k = %d, want 0", m.Cursor)
	}
}
