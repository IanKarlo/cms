package tui

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/skills"
	"github.com/ikts/cms/internal/storage"
)

type SkillOption struct {
	ID, Name, Description string
	Missing               bool
}

type ContextEditorModel struct {
	Registry  skills.Registry
	Context   model.Context
	Options   []SkillOption
	Selected  map[string]bool
	Cursor    int
	Field     int
	Filter    string
	Filtering bool
	Touched   [2]bool
	Err       error
	Saved     bool
	Canceled  bool
	NoColor   bool
}

func NewContextEditor(registry skills.Registry, initial model.Context) *ContextEditorModel {
	options := []SkillOption{}
	installed, _ := registry.List()
	seen := map[string]bool{}
	for _, m := range installed {
		options = append(options, SkillOption{ID: m.ID, Name: m.Name, Description: m.Description})
		seen[m.ID] = true
	}
	for _, ref := range initial.Skills {
		if !seen[ref.ID] {
			options = append(options, SkillOption{ID: ref.ID, Name: "[missing] " + ref.ID, Missing: true})
		}
	}
	sort.Slice(options, func(i, j int) bool { return options[i].Name < options[j].Name })
	selected := map[string]bool{}
	for _, ref := range initial.Skills {
		selected[ref.ID] = true
	}
	return &ContextEditorModel{Registry: registry, Context: initial, Options: options, Selected: selected}
}
func (m *ContextEditorModel) Init() tea.Cmd { return nil }
func (m *ContextEditorModel) filtered() []SkillOption {
	var out []SkillOption
	needle := strings.ToLower(m.Filter)
	for _, o := range m.Options {
		if needle == "" || strings.Contains(strings.ToLower(o.Name), needle) || strings.Contains(strings.ToLower(o.ID), needle) {
			out = append(out, o)
		}
	}
	return out
}
func (m *ContextEditorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		if m.Filtering {
			switch key.String() {
			case "esc":
				m.Filtering = false
			case "enter":
				m.Filtering = false
			case "backspace":
				if len(m.Filter) > 0 {
					runes := []rune(m.Filter)
					m.Filter = string(runes[:len(runes)-1])
				}
			default:
				if key.Type == tea.KeyRunes {
					m.Filter += string(key.Runes)
				}
			}
			return m, nil
		}
		if m.Field < 2 {
			switch key.String() {
			case "ctrl+c", "esc":
				m.Canceled = true
				return m, tea.Quit
			case "ctrl+s":
				if err := m.save(); err != nil {
					m.Err = err
					return m, nil
				}
				m.Saved = true
				return m, tea.Quit
			case "tab", "enter":
				m.Field++
				return m, nil
			case "shift+tab":
				if m.Field > 0 {
					m.Field--
				}
				return m, nil
			case "backspace":
				m.removeTextRune()
				return m, nil
			default:
				if key.Type == tea.KeySpace {
					m.appendText([]rune{' '})
				} else if key.Type == tea.KeyRunes {
					m.appendText(key.Runes)
				}
				return m, nil
			}
		}
		switch key.String() {
		case "ctrl+c", "esc":
			m.Canceled = true
			return m, tea.Quit
		case "ctrl+s":
			if err := m.save(); err != nil {
				m.Err = err
				return m, nil
			}
			m.Saved = true
			return m, tea.Quit
		case "tab", "enter":
			if m.Field < 2 {
				m.Field++
				return m, nil
			}
		case "shift+tab":
			if m.Field > 0 {
				m.Field--
				return m, nil
			}
		case "up", "k":
			if m.Field == 2 && m.Cursor > 0 {
				m.Cursor--
			}
		case "down", "j":
			if m.Field == 2 && m.Cursor < len(m.filtered())-1 {
				m.Cursor++
			}
		case " ":
			if m.Field == 2 {
				list := m.filtered()
				if len(list) > 0 {
					m.Selected[list[m.Cursor].ID] = !m.Selected[list[m.Cursor].ID]
				}
			}
		case "/":
			if m.Field == 2 {
				m.Filtering = true
			}
		}
	}
	return m, nil
}

func (m *ContextEditorModel) appendText(runes []rune) {
	if m.Field == 0 {
		if !m.Touched[0] {
			m.Context.Name = ""
			m.Touched[0] = true
		}
		m.Context.Name += string(runes)
	} else if m.Field == 1 {
		if !m.Touched[1] {
			m.Context.Description = ""
			m.Touched[1] = true
		}
		m.Context.Description += string(runes)
	}
}

func (m *ContextEditorModel) removeTextRune() {
	if m.Field == 0 && len(m.Context.Name) > 0 {
		runes := []rune(m.Context.Name)
		m.Context.Name = string(runes[:len(runes)-1])
		m.Touched[0] = true
	}
	if m.Field == 1 && len(m.Context.Description) > 0 {
		runes := []rune(m.Context.Description)
		m.Context.Description = string(runes[:len(runes)-1])
		m.Touched[1] = true
	}
}
func (m *ContextEditorModel) save() error {
	if err := storage.ValidateContextName(m.Context.Name); err != nil {
		return err
	}
	var refs []model.SkillRef
	for _, o := range m.Options {
		if m.Selected[o.ID] {
			refs = append(refs, model.SkillRef{ID: o.ID})
		}
	}
	if len(refs) == 0 {
		return fmt.Errorf("select at least one skill")
	}
	m.Context.Skills = refs
	return nil
}
func (m *ContextEditorModel) View() string {
	var b strings.Builder
	b.WriteString(renderHeading("Context editor", m.NoColor) + "\n\n")
	b.WriteString(fmt.Sprintf("Name: %s%s\nDescription: %s%s\n\n", m.Context.Name, fieldMark(m.Field == 0), m.Context.Description, fieldMark(m.Field == 1)))
	b.WriteString(renderHeading("Installed skills", m.NoColor) + "\n")
	list := m.filtered()
	for i, o := range list {
		marker := "  "
		if m.Field == 2 && i == m.Cursor {
			marker = "> "
		}
		check := "[ ]"
		if m.Selected[o.ID] {
			check = "[x]"
		}
		suffix := ""
		if o.Missing {
			suffix = " (missing)"
		}
		b.WriteString(fmt.Sprintf("%s%s %s%s\n", marker, check, o.Name, suffix))
	}
	if m.Filtering {
		b.WriteString("\nFilter: " + m.Filter + "_")
	}
	if m.Err != nil {
		b.WriteString("\nError: " + m.Err.Error())
	}
	b.WriteString("\nTab/Enter next • Space select • / filter • Ctrl+S save • Esc cancel")
	return b.String()
}
func fieldMark(active bool) string {
	if active {
		return " _"
	}
	return ""
}
func RunContextEditor(registry skills.Registry, initial model.Context, in io.Reader, out io.Writer) (model.Context, bool, error) {
	m := NewContextEditor(registry, initial)
	program, err := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out)).Run()
	if err != nil {
		return model.Context{}, false, err
	}
	result := program.(*ContextEditorModel)
	if result.Canceled {
		return model.Context{}, false, nil
	}
	if !result.Saved {
		return model.Context{}, false, result.Err
	}
	return result.Context, true, nil
}
