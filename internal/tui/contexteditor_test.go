package tui

import (
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
