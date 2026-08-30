package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/skills"
)

func TestContextEditorSelectsAndSaves(t *testing.T) {
	m := NewContextEditor(skills.Registry{}, model.Context{Name: "ctx", Description: "desc"})
	m.Options = []SkillOption{{ID: "one", Name: "one"}}
	m.Field = 2
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(*ContextEditorModel)
	if !m.Selected["one"] {
		t.Fatal("space should select a skill")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(*ContextEditorModel)
	if !m.Saved || len(m.Context.Skills) != 1 {
		t.Fatalf("expected saved context: %#v", m.Context)
	}
}

func TestContextEditorShowsMCPFilterCompatibility(t *testing.T) {
	m := NewContextEditor(skills.Registry{}, model.Context{})
	m.MCPOptions = []MCPOption{{ID: "demo@123", Name: "demo"}}
	m.MCPDrafts = map[string]model.MCPRef{"demo@123": {ID: "demo@123"}}
	m.Field = 4
	view := m.View()
	for _, want := range []string{"Tool-filter compatibility", "codex        complete", "claude       unsupported", "antigravity  partial", "opencode     partial"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestContextEditorUsesThreePagesAndTabNavigation(t *testing.T) {
	m := NewContextEditor(skills.Registry{}, model.Context{Name: "backend", Description: "Backend"})
	m.Options = []SkillOption{{ID: "skill@1", Name: "skill"}}
	m.MCPOptions = []MCPOption{{ID: "mcp@1", Name: "mcp"}}
	m.MCPDrafts = map[string]model.MCPRef{"mcp@1": {ID: "mcp@1"}}

	if view := m.View(); !strings.Contains(view, "1. Name and definition") || strings.Contains(view, "2. Skills\n\n  [ ] skill") {
		t.Fatalf("definition page is not isolated:\n%s", view)
	}
	for _, wantField := range []int{1, 2, 3, 4} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = updated.(*ContextEditorModel)
		if m.Field != wantField {
			t.Fatalf("Tab moved to field %d, want %d", m.Field, wantField)
		}
	}
	if view := m.View(); !strings.Contains(view, "3. MCPs") || strings.Contains(view, "skill@1") {
		t.Fatalf("MCP page is not isolated:\n%s", view)
	}

	for _, wantField := range []int{3, 2, 1} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
		m = updated.(*ContextEditorModel)
		if m.Field != wantField {
			t.Fatalf("Shift+Tab moved to field %d, want %d", m.Field, wantField)
		}
	}
}

func TestContextEditorCancel(t *testing.T) {
	m := NewContextEditor(skills.Registry{}, model.Context{Name: "ctx"})
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil || updated.(*ContextEditorModel).Canceled != true {
		t.Fatal("escape should cancel")
	}
}

func TestContextEditorAllowsNavigationKeysInDescription(t *testing.T) {
	m := NewContextEditor(skills.Registry{}, model.Context{})
	m.Field = 1
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("j")},
		{Type: tea.KeySpace, Runes: []rune(" ")},
		{Type: tea.KeyRunes, Runes: []rune("k")},
	} {
		updated, _ := m.Update(key)
		m = updated.(*ContextEditorModel)
	}
	if m.Context.Description != "j k" {
		t.Fatalf("description = %q, want %q", m.Context.Description, "j k")
	}
}
