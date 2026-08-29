package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/providers"
	"github.com/ikts/cms/internal/skills"
)

var heading = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))

type searchFinished struct {
	results []providers.SearchResult
	err     error
}
type installFinished struct {
	metadata model.SkillMetadata
	already  bool
	err      error
}
type spinnerTick struct{}

type SkillSearchModel struct {
	Provider       providers.SkillProvider
	Registry       skills.Registry
	Query          string
	Results        []providers.SearchResult
	Selected       int
	InputMode      bool
	Busy           bool
	Status         string
	Err            error
	Width          int
	Installed      *model.SkillMetadata
	InstalledNames map[string]bool
	Limit          int
	NoColor        bool
	Spinner        int
}

func NewSkillSearchModel(provider providers.SkillProvider, registry skills.Registry) *SkillSearchModel {
	return NewSkillSearchModelWithLimit(provider, registry, 30)
}

func NewSkillSearchModelWithLimit(provider providers.SkillProvider, registry skills.Registry, limit int) *SkillSearchModel {
	names := map[string]bool{}
	if list, err := registry.List(); err == nil {
		for _, item := range list {
			names[item.Name] = true
		}
	}
	return &SkillSearchModel{Provider: provider, Registry: registry, InputMode: true, InstalledNames: names, Limit: limit}
}

func (m *SkillSearchModel) Init() tea.Cmd { return nil }

func (m *SkillSearchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
	case searchFinished:
		m.Busy = false
		m.Results = msg.results
		m.Selected = 0
		m.InputMode = false
		m.Err = msg.err
		if msg.err == nil {
			m.Status = fmt.Sprintf("%d result(s). Enter installs, / searches again, Esc quits.", len(msg.results))
		}
	case installFinished:
		m.Busy = false
		m.Err = msg.err
		if msg.err == nil {
			m.Installed = &msg.metadata
			m.Status = fmt.Sprintf("Installed %s. Press q or Esc to exit.", msg.metadata.ID)
		}
	case spinnerTick:
		if m.Busy {
			m.Spinner = (m.Spinner + 1) % 4
			return m, tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return spinnerTick{} })
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
	case "ctrl+c", "esc", "q":
		if key.String() == "q" && m.InputMode {
			m.Query += "q"
			return m, nil
		}
		return m, tea.Quit
	case "enter":
		if m.InputMode {
			if strings.TrimSpace(m.Query) == "" {
				m.Err = fmt.Errorf("type a search query")
				return m, nil
			}
			m.Busy, m.Err, m.Status = true, nil, "Searching GitHub..."
			q := m.Query
			searchCmd := func() tea.Msg {
				results, err := m.Provider.Search(context.Background(), q, m.Limit)
				return searchFinished{results: results, err: err}
			}
			return m, tea.Batch(searchCmd, tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return spinnerTick{} }))
		}
		if len(m.Results) > 0 {
			m.Busy, m.Err, m.Status = true, nil, "Downloading selected skill..."
			result := m.Results[m.Selected]
			installCmd := func() tea.Msg {
				source := model.SkillSource{Type: "github", Repository: result.Repository, Path: result.Path, Ref: result.Ref, Commit: result.Commit}
				source, err := m.Provider.Resolve(context.Background(), source)
				if err != nil {
					return installFinished{err: err}
				}
				tmp, err := os.MkdirTemp("", "cms-tui-skill-")
				if err != nil {
					return installFinished{err: err}
				}
				defer os.RemoveAll(tmp)
				if err = m.Provider.Download(context.Background(), source, tmp); err != nil {
					return installFinished{err: err}
				}
				metadata, already, err := m.Registry.InstallDirectory(tmp, source)
				return installFinished{metadata: metadata, already: already, err: err}
			}
			return m, tea.Batch(installCmd, tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return spinnerTick{} }))
		}
	case "/":
		m.InputMode = true
	case "up":
		if !m.InputMode && m.Selected > 0 {
			m.Selected--
		}
	case "down":
		if !m.InputMode && m.Selected < len(m.Results)-1 {
			m.Selected++
		}
	case "k":
		if m.InputMode {
			m.Query += "k"
		} else if m.Selected > 0 {
			m.Selected--
		}
	case "j":
		if m.InputMode {
			m.Query += "j"
		} else if m.Selected < len(m.Results)-1 {
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

func (m *SkillSearchModel) View() string {
	if m.Width == 0 {
		m.Width = 80
	}
	var b strings.Builder
	b.WriteString(renderHeading("Search skills", m.NoColor) + "\n")
	b.WriteString("Query: " + m.Query + "_")
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
	if len(m.Results) == 0 && !m.Busy {
		b.WriteString("No results yet. Press Enter to search.\n")
	}
	for i, r := range m.Results {
		marker := "  "
		if i == m.Selected && !m.InputMode {
			marker = "> "
		}
		installed := ""
		if m.InstalledNames[r.Name] {
			installed = " [installed]"
		}
		b.WriteString(marker + r.Name + installed + " — " + r.Repository + "/" + r.Path + "\n")
	}
	if len(m.Results) > 0 && !m.InputMode {
		r := m.Results[m.Selected]
		b.WriteString("\n" + renderHeading("Details", m.NoColor) + "\nName: " + r.Name + "\nRepository: " + r.Repository + "\nPath: " + r.Path + "\nDescription: " + r.Description + "\nLicense: " + r.License + "\n")
	}
	if m.Status != "" {
		b.WriteString("\n" + m.Status + "\n")
	}
	b.WriteString("\nEnter search/install • / search • ↑↓ select • Esc quit")
	return b.String()
}

func RunSkillSearch(provider providers.SkillProvider, registry skills.Registry, in io.Reader, out io.Writer, limit int, noColor bool) (model.SkillMetadata, error) {
	m := NewSkillSearchModelWithLimit(provider, registry, limit)
	m.NoColor = noColor
	program, err := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out)).Run()
	if err != nil {
		return model.SkillMetadata{}, err
	}
	result := program.(*SkillSearchModel)
	if result.Err != nil {
		return model.SkillMetadata{}, result.Err
	}
	if result.Installed != nil {
		return *result.Installed, nil
	}
	return model.SkillMetadata{}, nil
}
