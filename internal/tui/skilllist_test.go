package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/ikts/cms/internal/model"
)

func TestSkillListFiltersByName(t *testing.T) {
	m := NewSkillListModel([]model.SkillMetadata{
		{Name: "go-testing", ID: "go-testing@abc123"},
		{Name: "api-design", ID: "api-design@def456"},
	})
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("/")},
		{Type: tea.KeyRunes, Runes: []rune("go")},
		{Type: tea.KeySpace, Runes: []rune(" ")},
		{Type: tea.KeyRunes, Runes: []rune("testing")},
	} {
		updated, _ := m.Update(key)
		m = updated.(*SkillListModel)
	}
	if m.Filter != "go testing" {
		t.Fatalf("filter = %q, want %q", m.Filter, "go testing")
	}
	if items := m.filtered(); len(items) != 1 || items[0].Name != "go-testing" {
		t.Fatalf("filtered skills = %#v", items)
	}
}

func TestSkillListViewHasTableAndDetails(t *testing.T) {
	m := NewSkillListModel([]model.SkillMetadata{{
		Name:        "go-testing",
		ID:          "go-testing@abc123",
		Description: "Go testing helpers",
		Source:      model.SkillSource{Repository: "org/repo", Path: "skills/go-testing"},
	}})
	m.Width = 100
	view := m.View()
	for _, expected := range []string{"┌", "NAME", "go-testing", "org/repo/skills/go-testing", "Go testing helpers", "└"} {
		if !strings.Contains(view, expected) {
			t.Errorf("view does not contain %q:\n%s", expected, view)
		}
	}
}

func TestSkillListWrapsLongDescription(t *testing.T) {
	m := NewSkillListModel([]model.SkillMetadata{{
		Name:        "long-description",
		ID:          "long-description@abc123",
		Description: "This is a long description that must be wrapped to remain readable in a narrow terminal.",
	}})
	m.Width = 40
	lines := wrapSkillText(m.Skills[0].Description, m.Width-2)
	if len(lines) < 2 {
		t.Fatalf("description was not wrapped: %#v", lines)
	}
	for _, line := range lines {
		if len([]rune(line)) > m.Width-2 {
			t.Fatalf("wrapped line has %d runes, want at most %d: %q", len([]rune(line)), m.Width-2, line)
		}
	}
	view := m.View()
	if !strings.Contains(view, "Description:\n  This is a long description") {
		t.Fatalf("wrapped description not rendered:\n%s", view)
	}
}

func TestSkillListUsesSlidingWindow(t *testing.T) {
	skills := make([]model.SkillMetadata, 15)
	for i := range skills {
		skills[i] = model.SkillMetadata{Name: fmt.Sprintf("skill-%02d", i), ID: fmt.Sprintf("skill-%02d@abc123", i)}
	}
	m := NewSkillListModel(skills)
	m.Width = 100
	m.Filtering = false
	view := m.View()
	if !strings.Contains(view, "Showing 1–8 of 15") || strings.Contains(view, "skill-08") {
		t.Fatalf("initial skill window is incorrect:\n%s", view)
	}

	for i := 0; i < 8; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(*SkillListModel)
	}
	view = m.View()
	if !strings.Contains(view, "Showing 2–9 of 15") || !strings.Contains(view, "skill-08") || strings.Contains(view, "skill-00") {
		t.Fatalf("sliding skill window is incorrect:\n%s", view)
	}
}
