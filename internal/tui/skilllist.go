package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/ikts/cms/internal/model"
)

type SkillListModel struct {
	Skills    []model.SkillMetadata
	Filter    string
	Cursor    int
	Filtering bool
	Width     int
	NoColor   bool
}

func NewSkillListModel(skills []model.SkillMetadata) *SkillListModel {
	return &SkillListModel{Skills: skills, Filtering: true}
}

func (m *SkillListModel) Init() tea.Cmd { return nil }

func (m *SkillListModel) filtered() []model.SkillMetadata {
	needle := normalizeSkillFilter(m.Filter)
	if needle == "" {
		return m.Skills
	}
	result := make([]model.SkillMetadata, 0, len(m.Skills))
	for _, skill := range m.Skills {
		if strings.Contains(normalizeSkillFilter(skill.Name), needle) || strings.Contains(strings.ToLower(skill.ID), strings.ToLower(strings.TrimSpace(m.Filter))) {
			result = append(result, skill)
		}
	}
	return result
}

func normalizeSkillFilter(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("-", " ", "_", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

func (m *SkillListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	switch key.String() {
	case "ctrl+c", "esc", "q":
		return m, tea.Quit
	case "/":
		m.Filtering = true
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
		}
	case "down", "j":
		items := m.filtered()
		if m.Cursor < len(items)-1 {
			m.Cursor++
		}
	}
	return m, nil
}

func (m *SkillListModel) View() string {
	if m.Width == 0 {
		m.Width = 100
	}
	items := m.filtered()
	if m.Cursor >= len(items) {
		m.Cursor = maxInt(0, len(items)-1)
	}

	var b strings.Builder
	b.WriteString(renderHeading("Installed skills", m.NoColor) + "\n")
	if m.Filtering {
		b.WriteString("Filter: " + m.Filter + "_\n")
	} else {
		b.WriteString("Filter: " + m.Filter + "  (press / to edit)\n")
	}
	b.WriteString(fmt.Sprintf("Showing %d of %d\n\n", len(items), len(m.Skills)))

	nameWidth, idWidth, sourceWidth := skillTableWidths(m.Width, items)
	b.WriteString(skillTableBorder('┌', '┬', '┐', nameWidth, idWidth, sourceWidth) + "\n")
	b.WriteString(skillTableRow("NAME", "ID", "SOURCE", nameWidth, idWidth, sourceWidth) + "\n")
	b.WriteString(skillTableBorder('├', '┼', '┤', nameWidth, idWidth, sourceWidth) + "\n")
	for i, skill := range items {
		name := skill.Name
		if i == m.Cursor {
			name = "▸ " + name
		}
		source := skill.Source.Repository
		if skill.Source.Path != "" {
			source += "/" + skill.Source.Path
		}
		b.WriteString(skillTableRow(name, skill.ID, source, nameWidth, idWidth, sourceWidth) + "\n")
	}
	if len(items) == 0 {
		b.WriteString(skillTableRow("No skills match the filter", "", "", nameWidth, idWidth, sourceWidth) + "\n")
	}
	b.WriteString(skillTableBorder('└', '┴', '┘', nameWidth, idWidth, sourceWidth) + "\n")

	if len(items) > 0 {
		selected := items[m.Cursor]
		b.WriteString("\n" + renderHeading("Details", m.NoColor) + "\n")
		b.WriteString("Name: " + selected.Name + "\n")
		b.WriteString("Description:\n")
		descriptionWidth := maxInt(1, m.Width-2)
		for _, line := range wrapSkillText(selected.Description, descriptionWidth) {
			b.WriteString("  " + line + "\n")
		}
		b.WriteString("ID: " + selected.ID + "\n")
	}
	b.WriteString("\nType to filter • Enter browse • / edit filter • ↑↓ navigate • Esc quit")
	return b.String()
}

func skillTableWidths(terminalWidth int, items []model.SkillMetadata) (int, int, int) {
	nameWidth, idWidth := 18, 24
	for _, skill := range items {
		nameWidth = maxInt(nameWidth, len([]rune(skill.Name))+2)
		idWidth = maxInt(idWidth, len([]rune(skill.ID))+2)
	}
	nameWidth = minInt(nameWidth, 30)
	idWidth = minInt(idWidth, 30)
	sourceWidth := terminalWidth - nameWidth - idWidth - 8
	if sourceWidth < 20 {
		sourceWidth = 20
	}
	return nameWidth, idWidth, sourceWidth
}

func skillTableBorder(left, middle, right rune, widths ...int) string {
	parts := make([]string, len(widths))
	for i, width := range widths {
		parts[i] = strings.Repeat("─", width+2)
	}
	return string(left) + strings.Join(parts, string(middle)) + string(right)
}

func skillTableRow(name, id, source string, nameWidth, idWidth, sourceWidth int) string {
	return "│ " + skillTableCell(name, nameWidth) + " │ " + skillTableCell(id, idWidth) + " │ " + skillTableCell(source, sourceWidth) + " │"
}

func skillTableCell(value string, width int) string {
	value = truncateRunes(value, width)
	return value + strings.Repeat(" ", width-len([]rune(value)))
}

func truncateRunes(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

func wrapSkillText(value string, width int) []string {
	if width < 1 {
		width = 1
	}
	var lines []string
	paragraphs := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	for _, paragraph := range paragraphs {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			continue
		}
		line := ""
		for _, word := range words {
			for len([]rune(word)) > width {
				wordRunes := []rune(word)
				if line != "" {
					lines = append(lines, line)
					line = ""
				}
				lines = append(lines, string(wordRunes[:width]))
				word = string(wordRunes[width:])
			}
			if line == "" {
				line = word
			} else if len([]rune(line))+1+len([]rune(word)) <= width {
				line += " " + word
			} else {
				lines = append(lines, line)
				line = word
			}
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func RunSkillList(skills []model.SkillMetadata, in io.Reader, out io.Writer, noColor bool) error {
	m := NewSkillListModel(skills)
	m.NoColor = noColor
	_, err := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out)).Run()
	return err
}
