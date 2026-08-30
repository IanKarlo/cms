package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ikts/cms/internal/providers"
)

type mcpSearchFinished struct {
	results []providers.MCPSearchResult
	err     error
}
type mcpSearchSpinnerTick struct{}

// MCPRegistrySearchModel is intentionally a browser, not an installer. It
// only searches and selects a registry record; the CLI resolves the selected
// record and performs the normal validation/registration flow afterwards.
type MCPRegistrySearchModel struct {
	Provider  providers.MCPProvider
	Query     string
	Results   []providers.MCPSearchResult
	Selected  int
	InputMode bool
	Busy      bool
	Status    string
	Err       error
	Width     int
	Height    int
	NoColor   bool
	Spinner   int
	Chosen    *providers.MCPSearchResult
	Limit     int
}

func NewMCPRegistrySearchModel(provider providers.MCPProvider, limit int) *MCPRegistrySearchModel {
	if limit <= 0 {
		limit = 30
	}
	return &MCPRegistrySearchModel{Provider: provider, InputMode: true, Limit: limit}
}

func (m *MCPRegistrySearchModel) Init() tea.Cmd { return nil }

func (m *MCPRegistrySearchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
	case mcpSearchFinished:
		m.Busy = false
		m.Results = msg.results
		m.Selected = 0
		m.InputMode = false
		m.Err = msg.err
		if msg.err == nil {
			m.Status = fmt.Sprintf("%d resultado(s). Enter seleciona, / faz nova busca, Esc sai.", len(msg.results))
		}
	case mcpSearchSpinnerTick:
		if m.Busy {
			m.Spinner = (m.Spinner + 1) % 4
			return m, tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return mcpSearchSpinnerTick{} })
		}
	}
	if m.Busy {
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "ctrl+c", "esc":
		return m, tea.Quit
	case "q":
		if m.InputMode {
			m.Query += "q"
			return m, nil
		}
		return m, tea.Quit
	case "enter":
		if m.InputMode {
			if strings.TrimSpace(m.Query) == "" {
				m.Err = fmt.Errorf("digite uma busca para o Official MCP Registry")
				return m, nil
			}
			m.Busy, m.Err, m.Status = true, nil, "Buscando no Official MCP Registry..."
			query := m.Query
			searchCmd := func() tea.Msg {
				results, err := m.Provider.Search(context.Background(), query, m.Limit)
				return mcpSearchFinished{results: results, err: err}
			}
			return m, tea.Batch(searchCmd, tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return mcpSearchSpinnerTick{} }))
		}
		if len(m.Results) > 0 {
			chosen := m.Results[m.Selected]
			m.Chosen = &chosen
			return m, tea.Quit
		}
	case "/":
		m.InputMode = true
	case "up", "k":
		if !m.InputMode && m.Selected > 0 {
			m.Selected--
		}
	case "down", "j":
		if !m.InputMode && m.Selected < len(m.Results)-1 {
			m.Selected++
		}
	case "backspace":
		if m.InputMode && len(m.Query) > 0 {
			runes := []rune(m.Query)
			m.Query = string(runes[:len(runes)-1])
		}
	default:
		if m.InputMode && key.Type == tea.KeySpace {
			m.Query += " "
		} else if m.InputMode && key.Type == tea.KeyRunes {
			m.Query += string(key.Runes)
		}
	}
	return m, nil
}

func (m *MCPRegistrySearchModel) View() string {
	if m.Width == 0 {
		m.Width = 100
	}
	if m.Selected >= len(m.Results) {
		m.Selected = maxInt(0, len(m.Results)-1)
	}
	var b strings.Builder
	b.WriteString(renderHeading("Search Official MCP Registry", m.NoColor) + "\n")
	if m.InputMode {
		b.WriteString("Query: " + m.Query + "_")
	} else {
		b.WriteString("Query: " + m.Query + "  (press / to edit)")
	}
	if m.Busy {
		b.WriteString("  [" + []string{"-", "\\", "|", "/"}[m.Spinner] + "]")
	}
	b.WriteString("\n\n")
	if m.Err != nil {
		if m.NoColor {
			b.WriteString("Error: " + m.Err.Error() + "\n\n")
		} else {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("Error: "+m.Err.Error()) + "\n\n")
		}
	}
	start, end := mcpVisibleRange(m.Selected, len(m.Results), m.visibleCount())
	visible := m.Results[start:end]
	if len(m.Results) == 0 {
		b.WriteString("Showing 0 result(s)\n\n")
	} else {
		b.WriteString(fmt.Sprintf("Showing %d–%d of %d result(s)\n\n", start+1, end, len(m.Results)))
	}
	nameWidth, versionWidth, linkWidth := mcpTableWidths(m.Width, visible)
	b.WriteString(skillTableBorder('┌', '┬', '┐', nameWidth, versionWidth, linkWidth) + "\n")
	b.WriteString(skillTableRow("NAME", "VERSION", "LINK", nameWidth, versionWidth, linkWidth) + "\n")
	b.WriteString(skillTableBorder('├', '┼', '┤', nameWidth, versionWidth, linkWidth) + "\n")
	for i, result := range visible {
		name := result.Name
		if start+i == m.Selected && !m.InputMode {
			name = "▸ " + name
		}
		b.WriteString(skillTableRow(name, result.Version, mcpResultLink(result), nameWidth, versionWidth, linkWidth) + "\n")
	}
	if len(m.Results) == 0 {
		b.WriteString(skillTableRow("No MCPs match the search", "", "", nameWidth, versionWidth, linkWidth) + "\n")
	}
	b.WriteString(skillTableBorder('└', '┴', '┘', nameWidth, versionWidth, linkWidth) + "\n")

	if len(m.Results) > 0 && !m.InputMode {
		b.WriteString("\n" + renderHeading("Details", m.NoColor) + "\n")
		b.WriteString(renderMCPRegistryDetails(m.Results[m.Selected], maxInt(1, m.Width-2)))
	}
	if m.Status != "" {
		b.WriteString("\n" + m.Status + "\n")
	}
	b.WriteString("\nType to search • Enter search/select • / edit search • ↑↓ navigate • Esc quit")
	return b.String()
}

func (m *MCPRegistrySearchModel) visibleCount() int {
	// Keep the result window compact and predictable. The terminal height is
	// still tracked for future layout decisions, but never expands this list.
	return 8
}

func mcpVisibleRange(selected, total, count int) (int, int) {
	if total == 0 {
		return 0, 0
	}
	if count < 1 {
		count = 1
	}
	if count > total {
		count = total
	}
	if selected < 0 {
		selected = 0
	}
	if selected >= total {
		selected = total - 1
	}
	// Keep the cursor in the current window until it reaches an edge, then
	// advance one row at a time as navigation continues.
	start := selected - count + 1
	if start < 0 {
		start = 0
	}
	if start+count > total {
		start = total - count
	}
	return start, start + count
}

func mcpTableWidths(width int, results []providers.MCPSearchResult) (int, int, int) {
	// Keep enough room for every heading. Long registry names and URLs should
	// shrink the cells, never push the table past the terminal edge.
	nameWidth, versionWidth, linkWidth := 18, 12, 24
	for _, result := range results {
		nameWidth = maxInt(nameWidth, len([]rune(result.Name))+2)
		versionWidth = maxInt(versionWidth, len([]rune(result.Version))+2)
	}
	nameWidth = minInt(nameWidth, 30)
	versionWidth = minInt(versionWidth, 20)
	available := maxInt(0, width-10) // row borders, separators and cell padding
	minimumName, minimumVersion, minimumLink := 4, 7, 4
	if nameWidth+versionWidth+linkWidth > available {
		// The link is informational and can be shortened first.
		linkWidth = maxInt(minimumLink, available-nameWidth-versionWidth)
	}
	if nameWidth+versionWidth+linkWidth > available {
		nameWidth = maxInt(minimumName, available-versionWidth-linkWidth)
	}
	if nameWidth+versionWidth+linkWidth > available {
		versionWidth = maxInt(minimumVersion, available-nameWidth-linkWidth)
	}
	return nameWidth, versionWidth, linkWidth
}

func mcpResultLink(result providers.MCPSearchResult) string {
	if result.RepositoryURL != "" {
		return result.RepositoryURL
	}
	if result.WebsiteURL != "" {
		return result.WebsiteURL
	}
	if len(result.Remotes) > 0 {
		return result.Remotes[0].URL
	}
	if len(result.Packages) > 0 {
		return result.Packages[0].Identifier
	}
	return "—"
}

func renderMCPRegistryDetails(result providers.MCPSearchResult, width int) string {
	var b strings.Builder
	b.WriteString("Name: " + result.Name + "\n")
	if result.Title != "" && result.Title != result.Name {
		b.WriteString("Title: " + result.Title + "\n")
	}
	b.WriteString("Version: " + result.Version)
	if result.IsLatest {
		b.WriteString(" (latest)")
	}
	b.WriteString("\n")
	if result.Status != "" {
		b.WriteString("Status: " + result.Status + "\n")
	}
	b.WriteString("Description:\n")
	for _, line := range wrapSkillText(result.Description, width) {
		b.WriteString("  " + line + "\n")
	}
	if result.RepositoryURL != "" {
		b.WriteString("Repository: " + result.RepositoryURL)
		if result.RepositorySource != "" {
			b.WriteString(" (" + result.RepositorySource + ")")
		}
		b.WriteByte('\n')
	}
	if result.WebsiteURL != "" {
		b.WriteString("Website: " + result.WebsiteURL + "\n")
	}
	if len(result.Packages) > 0 {
		b.WriteString("Packages:\n")
		for _, packageInfo := range result.Packages {
			b.WriteString("  - " + packageInfo.RegistryType + ": " + packageInfo.Identifier)
			if packageInfo.Version != "" {
				b.WriteString(" @ " + packageInfo.Version)
			}
			if packageInfo.RuntimeHint != "" {
				b.WriteString(" [runtime: " + packageInfo.RuntimeHint + "]")
			}
			b.WriteByte('\n')
		}
	}
	if len(result.Remotes) > 0 {
		b.WriteString("Remote transports:\n")
		for _, remote := range result.Remotes {
			kind := remote.Type
			if kind == "" {
				kind = "streamable-http"
			}
			b.WriteString("  - " + kind + ": " + remote.URL + "\n")
		}
	}
	if result.StatusMessage != "" {
		b.WriteString("Registry note: " + result.StatusMessage + "\n")
	}
	if result.PublishedAt != "" {
		b.WriteString("Published: " + result.PublishedAt + "\n")
	}
	if result.UpdatedAt != "" {
		b.WriteString("Updated: " + result.UpdatedAt + "\n")
	}
	if len(result.Icons) > 0 {
		b.WriteString(fmt.Sprintf("Icons: %d available\n", len(result.Icons)))
	}
	return b.String()
}

func RunMCPRegistrySearch(provider providers.MCPProvider, in io.Reader, out io.Writer, limit int, noColor bool) (providers.MCPSearchResult, error) {
	m := NewMCPRegistrySearchModel(provider, limit)
	m.NoColor = noColor
	program, err := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out)).Run()
	if err != nil {
		return providers.MCPSearchResult{}, err
	}
	result := program.(*MCPRegistrySearchModel)
	if result.Err != nil {
		return providers.MCPSearchResult{}, result.Err
	}
	if result.Chosen == nil {
		return providers.MCPSearchResult{}, nil
	}
	return *result.Chosen, nil
}
