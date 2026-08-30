package tui

import (
	"io"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/ikts/cms/internal/model"
)

type ContextListModel struct {
	Contexts   []model.Context
	SkillNames map[string]string
	Filter     string
	Cursor     int
	Width      int
	Filtering  bool
	NoColor    bool
}

func NewContextListModel(contexts []model.Context) *ContextListModel {
	return NewContextListModelWithSkillNames(contexts, nil)
}

func NewContextListModelWithSkillNames(contexts []model.Context, skillNames map[string]string) *ContextListModel {
	return &ContextListModel{Contexts: contexts, SkillNames: skillNames}
}
func (m *ContextListModel) Init() tea.Cmd { return nil }
func (m *ContextListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if window, ok := msg.(tea.WindowSizeMsg); ok {
		m.Width = window.Width
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.Filtering {
		switch key.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "enter":
			m.Filtering = false
		case "backspace":
			if len(m.Filter) > 0 {
				runes := []rune(m.Filter)
				m.Filter = string(runes[:len(runes)-1])
				m.Cursor = 0
			}
		case "q":
			m.Filter += "q"
			m.Cursor = 0
		default:
			if key.Type == tea.KeySpace {
				m.Filter += " "
				m.Cursor = 0
			} else if key.Type == tea.KeyRunes {
				m.Filter += string(key.Runes)
				m.Cursor = 0
			}
		}
		return m, nil
	}

	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "/":
			m.Filtering = true
		case "up", "k":
			if m.Cursor > 0 {
				m.Cursor--
			}
		case "down", "j":
			if m.Cursor < len(m.filtered())-1 {
				m.Cursor++
			}
		}
	}
	return m, nil
}
func (m *ContextListModel) View() string {
	if m.Width == 0 {
		m.Width = 100
	}
	items := m.filtered()
	if m.Cursor >= len(items) {
		m.Cursor = maxInt(0, len(items)-1)
	}
	nameWidth, countWidth, descriptionWidth := contextTableWidths(m.Width, items)
	var b strings.Builder
	b.WriteString(renderHeading("Contexts", m.NoColor) + "\n")
	if m.Filtering {
		b.WriteString("Filter: " + m.Filter + "_\n")
	} else {
		b.WriteString("Filter: " + m.Filter + "  (press / to edit)\n")
	}
	b.WriteString("Showing " + itoa(len(items)) + " of " + itoa(len(m.Contexts)) + " context(s)\n\n")
	b.WriteString(skillTableBorder('┌', '┬', '┐', nameWidth, countWidth, descriptionWidth) + "\n")
	b.WriteString(skillTableRow("NAME", "SKILLS", "DESCRIPTION", nameWidth, countWidth, descriptionWidth) + "\n")
	b.WriteString(skillTableBorder('├', '┼', '┤', nameWidth, countWidth, descriptionWidth) + "\n")
	for i, c := range items {
		name := c.Name
		if i == m.Cursor {
			name = "▸ " + name
		}
		b.WriteString(skillTableRow(name, itoa(len(c.Skills)), c.Description, nameWidth, countWidth, descriptionWidth) + "\n")
	}
	if len(items) == 0 {
		b.WriteString(skillTableRow("No contexts found", "", "", nameWidth, countWidth, descriptionWidth) + "\n")
	}
	b.WriteString(skillTableBorder('└', '┴', '┘', nameWidth, countWidth, descriptionWidth) + "\n")
	if len(items) > 0 {
		selected := items[m.Cursor]
		b.WriteString("\n" + renderHeading("Details", m.NoColor) + "\n")
		b.WriteString("Name: " + selected.Name + "\n")
		b.WriteString("Description:\n")
		for _, line := range wrapSkillText(selected.Description, maxInt(1, m.Width-2)) {
			b.WriteString("  " + line + "\n")
		}
		b.WriteString("Skills:\n")
		for _, ref := range selected.Skills {
			name := ref.ID
			if m.SkillNames != nil && m.SkillNames[ref.ID] != "" {
				name = m.SkillNames[ref.ID]
			}
			b.WriteString("  • " + name + "\n")
		}
	}
	if m.Filtering {
		b.WriteString("\nType to filter • Enter browse • Esc quit")
	} else {
		b.WriteString("\n↑↓ or j/k navigate • / edit filter • Esc quit")
	}
	return b.String()
}

func (m *ContextListModel) filtered() []model.Context {
	needle := normalizeSkillFilter(m.Filter)
	if needle == "" {
		return m.Contexts
	}
	result := make([]model.Context, 0, len(m.Contexts))
	for _, context := range m.Contexts {
		if strings.Contains(normalizeSkillFilter(context.Name), needle) {
			result = append(result, context)
		}
	}
	return result
}

func contextTableWidths(terminalWidth int, contexts []model.Context) (int, int, int) {
	nameWidth, countWidth := 18, 8
	for _, context := range contexts {
		nameWidth = maxInt(nameWidth, len([]rune(context.Name))+2)
	}
	nameWidth = minInt(nameWidth, 30)
	descriptionWidth := terminalWidth - nameWidth - countWidth - 8
	if descriptionWidth < 20 {
		descriptionWidth = 20
	}
	return nameWidth, countWidth, descriptionWidth
}
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var chars [20]byte
	i := len(chars)
	for n > 0 {
		i--
		chars[i] = byte('0' + n%10)
		n /= 10
	}
	return string(chars[i:])
}
func RunContextList(contexts []model.Context, skillNames map[string]string, in io.Reader, out io.Writer, noColor bool) error {
	m := NewContextListModelWithSkillNames(contexts, skillNames)
	m.NoColor = noColor
	_, err := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out)).Run()
	return err
}
