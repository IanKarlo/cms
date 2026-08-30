package tui

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/ikts/cms/internal/model"
)

type MCPListModel struct {
	MCPs      []model.MCPMetadata
	Labels    map[string]string
	Filter    string
	Cursor    int
	Filtering bool
	Width     int
	NoColor   bool
}

func NewMCPListModel(mcps []model.MCPMetadata) *MCPListModel {
	return &MCPListModel{MCPs: mcps, Labels: model.MCPDisplayLabels(mcps)}
}

func (m *MCPListModel) Init() tea.Cmd { return nil }

func (m *MCPListModel) displayName(mcp model.MCPMetadata) string {
	if m.Labels != nil && m.Labels[mcp.ID] != "" {
		return m.Labels[mcp.ID]
	}
	if mcp.Name != "" {
		return mcp.Name
	}
	return mcp.ID
}

func (m *MCPListModel) filtered() []model.MCPMetadata {
	needle := normalizeSkillFilter(m.Filter)
	if needle == "" {
		return m.MCPs
	}
	rawNeedle := strings.ToLower(strings.TrimSpace(m.Filter))
	result := make([]model.MCPMetadata, 0, len(m.MCPs))
	for _, mcp := range m.MCPs {
		fields := []string{
			m.displayName(mcp),
			mcp.Name,
			mcp.ID,
			mcp.Description,
			string(mcp.Transport),
			string(mcp.Source.Type),
			mcp.Source.RegistryName,
			mcp.Source.PackageIdentifier,
			mcp.Source.Version,
			mcp.Source.Variant,
			mcp.DisplayOrigin(),
		}
		if mcp.Command != nil {
			fields = append(fields, mcp.Command.Command, strings.Join(mcp.Command.Args, " "), mcp.Command.Cwd)
		}
		if mcp.Remote != nil {
			fields = append(fields, mcp.Remote.URL, mcp.Remote.Auth.Env)
		}
		for _, requirement := range mcp.Requirements {
			fields = append(fields, requirement.Name, requirement.Description)
		}
		matched := false
		for _, field := range fields {
			if strings.Contains(normalizeSkillFilter(field), needle) || strings.Contains(strings.ToLower(field), rawNeedle) {
				matched = true
				break
			}
		}
		if matched {
			result = append(result, mcp)
		}
	}
	return result
}

func (m *MCPListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m *MCPListModel) View() string {
	if m.Width == 0 {
		m.Width = 100
	}
	items := m.filtered()
	if m.Cursor < 0 {
		m.Cursor = 0
	}
	if m.Cursor >= len(items) {
		m.Cursor = maxInt(0, len(items)-1)
	}

	var b strings.Builder
	b.WriteString(renderHeading("Registered MCPs", m.NoColor) + "\n")
	if m.Filtering {
		b.WriteString("Filter: " + m.Filter + "_\n")
	} else {
		b.WriteString("Filter: " + m.Filter + "  (press / to edit)\n")
	}
	start, end := mcpVisibleRange(m.Cursor, len(items), m.visibleCount())
	visible := items[start:end]
	if len(items) == 0 {
		b.WriteString(fmt.Sprintf("Showing 0 of %d\n\n", len(m.MCPs)))
	} else {
		b.WriteString(fmt.Sprintf("Showing %d–%d of %d\n\n", start+1, end, len(items)))
	}

	nameWidth, idWidth, transportWidth, sourceWidth := mcpListTableWidths(m.Width, visible, m)
	b.WriteString(skillTableBorder('┌', '┬', '┐', nameWidth, idWidth, transportWidth, sourceWidth) + "\n")
	b.WriteString(mcpListTableRow("NAME", "ID", "TRANSPORT", "SOURCE", nameWidth, idWidth, transportWidth, sourceWidth) + "\n")
	b.WriteString(skillTableBorder('├', '┼', '┤', nameWidth, idWidth, transportWidth, sourceWidth) + "\n")
	for i, mcp := range visible {
		name := m.displayName(mcp)
		if start+i == m.Cursor {
			name = "▸ " + name
		}
		b.WriteString(mcpListTableRow(name, mcp.ID, string(mcp.Transport), mcpListSource(mcp), nameWidth, idWidth, transportWidth, sourceWidth) + "\n")
	}
	if len(items) == 0 {
		b.WriteString(mcpListTableRow("No MCPs match the filter", "", "", "", nameWidth, idWidth, transportWidth, sourceWidth) + "\n")
	}
	b.WriteString(skillTableBorder('└', '┴', '┘', nameWidth, idWidth, transportWidth, sourceWidth) + "\n")

	if len(items) > 0 {
		b.WriteString("\n" + renderHeading("Details", m.NoColor) + "\n")
		b.WriteString(renderMCPListDetails(items[m.Cursor], maxInt(1, m.Width-2)))
	}
	if m.Filtering {
		b.WriteString("\nType to filter • Enter browse • Esc quit")
	} else {
		b.WriteString("\n↑↓ or j/k navigate • / edit filter • Esc quit")
	}
	return b.String()
}

func (m *MCPListModel) visibleCount() int { return 8 }

func mcpListSource(mcp model.MCPMetadata) string {
	origin := mcp.DisplayOrigin()
	if origin == "" {
		return string(mcp.Source.Type)
	}
	return string(mcp.Source.Type) + ": " + origin
}

func mcpListTableRow(name, id, transport, source string, nameWidth, idWidth, transportWidth, sourceWidth int) string {
	return "│ " + skillTableCell(name, nameWidth) + " │ " + skillTableCell(id, idWidth) + " │ " + skillTableCell(transport, transportWidth) + " │ " + skillTableCell(source, sourceWidth) + " │"
}

func mcpListTableWidths(width int, items []model.MCPMetadata, m *MCPListModel) (int, int, int, int) {
	nameWidth, idWidth, transportWidth, sourceWidth := 18, 24, 12, 24
	for _, mcp := range items {
		nameWidth = maxInt(nameWidth, len([]rune(m.displayName(mcp)))+2)
		idWidth = maxInt(idWidth, len([]rune(mcp.ID))+2)
		sourceWidth = maxInt(sourceWidth, len([]rune(mcpListSource(mcp)))+2)
	}
	nameWidth = minInt(nameWidth, 32)
	idWidth = minInt(idWidth, 30)
	sourceWidth = minInt(sourceWidth, 36)
	available := maxInt(0, width-10)
	minimumName, minimumID, minimumSource := 4, 2, len("SOURCE")
	if nameWidth+idWidth+transportWidth+sourceWidth > available {
		sourceWidth = maxInt(minimumSource, available-nameWidth-idWidth-transportWidth)
	}
	if nameWidth+idWidth+transportWidth+sourceWidth > available {
		nameWidth = maxInt(minimumName, available-idWidth-transportWidth-sourceWidth)
	}
	if nameWidth+idWidth+transportWidth+sourceWidth > available {
		idWidth = maxInt(minimumID, available-nameWidth-transportWidth-sourceWidth)
	}
	return nameWidth, idWidth, transportWidth, sourceWidth
}

func renderMCPListDetails(mcp model.MCPMetadata, width int) string {
	var b strings.Builder
	b.WriteString("Name: " + mcp.Name + "\n")
	b.WriteString("Description:\n")
	for _, line := range wrapSkillText(mcp.Description, width) {
		b.WriteString("  " + line + "\n")
	}
	b.WriteString("ID: " + mcp.ID + "\n")
	b.WriteString("Transport: " + string(mcp.Transport) + "\n")
	b.WriteString("Source: " + mcpListSource(mcp) + "\n")
	if mcp.Source.Version != "" {
		b.WriteString("Version: " + mcp.Source.Version + "\n")
	}
	if mcp.Source.Variant != "" {
		b.WriteString("Variant: " + mcp.Source.Variant + "\n")
	}
	if mcp.Source.PackageIdentifier != "" {
		b.WriteString("Package: " + mcp.Source.PackageType + ":" + mcp.Source.PackageIdentifier)
		if mcp.Source.PackageVersion != "" {
			b.WriteString(" @ " + mcp.Source.PackageVersion)
		}
		b.WriteByte('\n')
	}
	if mcp.Command != nil {
		command := append([]string{mcp.Command.Command}, mcp.Command.Args...)
		b.WriteString("Command: " + strings.Join(command, " ") + "\n")
		if mcp.Command.Cwd != "" {
			b.WriteString("CWD: " + mcp.Command.Cwd + "\n")
		}
	}
	if mcp.Remote != nil {
		b.WriteString("URL: " + mcp.Remote.URL + "\n")
		if mcp.Remote.Auth.Mode != "" {
			b.WriteString("Auth: " + mcp.Remote.Auth.Mode)
			if mcp.Remote.Auth.Env != "" {
				b.WriteString(" (" + mcp.Remote.Auth.Env + ")")
			}
			b.WriteByte('\n')
		}
		if len(mcp.Remote.Headers) > 0 {
			headers := make([]string, 0, len(mcp.Remote.Headers))
			for name := range mcp.Remote.Headers {
				headers = append(headers, name)
			}
			sort.Strings(headers)
			b.WriteString("Headers: " + strings.Join(headers, ", ") + "\n")
		}
	}
	if len(mcp.Requirements) > 0 {
		b.WriteString("Requirements:\n")
		for _, requirement := range mcp.Requirements {
			label := requirement.Kind + ": " + requirement.Name
			if requirement.Required {
				label += " (required)"
			}
			if requirement.Secret {
				label += " (secret)"
			}
			b.WriteString("  • " + label + "\n")
		}
	}
	reproducible := "no"
	if mcp.Reproducible {
		reproducible = "yes"
	}
	b.WriteString("Reproducible: " + reproducible + "\n")
	return b.String()
}

func RunMCPList(mcps []model.MCPMetadata, in io.Reader, out io.Writer, noColor bool) error {
	m := NewMCPListModel(mcps)
	m.NoColor = noColor
	_, err := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out)).Run()
	return err
}
