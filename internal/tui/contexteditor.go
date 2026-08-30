package tui

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/ikts/cms/internal/harness"
	"github.com/ikts/cms/internal/mcps"
	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/skills"
	"github.com/ikts/cms/internal/storage"
)

type SkillOption struct {
	ID, Name, Description string
	Missing               bool
}
type MCPOption struct {
	ID, Name, Description string
	Missing               bool
}

type ContextEditorModel struct {
	Registry    skills.Registry
	Context     model.Context
	Options     []SkillOption
	MCPOptions  []MCPOption
	Selected    map[string]bool
	MCPSelected map[string]bool
	MCPDrafts   map[string]model.MCPRef
	MCPTouched  map[string][3]bool
	Cursor      int
	Field       int
	Filter      string
	Filtering   bool
	Touched     [2]bool
	Err         error
	Saved       bool
	Canceled    bool
	NoColor     bool
}

func NewContextEditor(registry skills.Registry, initial model.Context) *ContextEditorModel {
	return newContextEditor(registry, nil, initial)
}
func NewContextEditorWithMCP(registry skills.Registry, mcpRegistry mcps.Registry, initial model.Context) *ContextEditorModel {
	return newContextEditor(registry, &mcpRegistry, initial)
}
func newContextEditor(registry skills.Registry, mcpRegistry *mcps.Registry, initial model.Context) *ContextEditorModel {
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
	mcpOptions := []MCPOption{}
	mcpSelected := map[string]bool{}
	mcpDrafts := map[string]model.MCPRef{}
	mcpTouched := map[string][3]bool{}
	if mcpRegistry != nil {
		installed, _ := mcpRegistry.List()
		labels := model.MCPDisplayLabels(installed)
		seenM := map[string]bool{}
		for _, m := range installed {
			mcpOptions = append(mcpOptions, MCPOption{ID: m.ID, Name: labels[m.ID], Description: m.Description})
			seenM[m.ID] = true
		}
		for _, r := range initial.MCPRefs {
			mcpSelected[r.ID] = true
			mcpDrafts[r.ID] = r
			if !seenM[r.ID] {
				mcpOptions = append(mcpOptions, MCPOption{ID: r.ID, Name: "[missing] " + r.ID, Missing: true})
			}
		}
		if len(initial.MCPRefs) == 0 {
			for _, id := range initial.MCPs {
				mcpSelected[id] = true
				mcpDrafts[id] = model.MCPRef{ID: id}
				if !seenM[id] {
					mcpOptions = append(mcpOptions, MCPOption{ID: id, Name: "[missing] " + id, Missing: true})
				}
			}
		}
		sort.Slice(mcpOptions, func(i, j int) bool { return mcpOptions[i].Name < mcpOptions[j].Name })
	}
	return &ContextEditorModel{Registry: registry, Context: initial, Options: options, MCPOptions: mcpOptions, Selected: selected, MCPSelected: mcpSelected, MCPDrafts: mcpDrafts, MCPTouched: mcpTouched}
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
		if m.Field < 2 || m.Field >= 4 {
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
				switch m.Field {
				case 0:
					m.Field = 1
				case 1:
					m.Field = 2
				case 4:
					m.Field = 5
				case 5:
					m.Field = 6
				case 6:
					m.Field = 0
				}
				return m, nil
			case "shift+tab":
				switch m.Field {
				case 1:
					m.Field = 0
				case 4:
					m.Field = 3
				case 5:
					m.Field = 4
				case 6:
					m.Field = 5
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
			if m.Field == 2 {
				m.Field = 3
				m.Cursor = 0
				return m, nil
			}
			if m.Field == 3 && len(m.MCPOptions) > 0 {
				m.Field = 4
				m.loadMCPDraft()
				return m, nil
			}
			if m.Field == 3 {
				m.Field = 0
				return m, nil
			}
		case "shift+tab":
			switch m.Field {
			case 2:
				m.Field = 1
			case 3:
				m.Field = 2
			case 4:
				m.Field = 3
			case 5:
				m.Field = 4
			case 6:
				m.Field = 5
			}
			return m, nil
		case "up", "k":
			if (m.Field == 2 || m.Field == 3) && m.Cursor > 0 {
				m.Cursor--
			}
		case "down", "j":
			limit := len(m.filtered())
			if m.Field == 3 {
				limit = len(m.MCPOptions)
			}
			if (m.Field == 2 || m.Field == 3) && m.Cursor < limit-1 {
				m.Cursor++
			}
		case " ":
			if m.Field == 2 {
				list := m.filtered()
				if len(list) > 0 {
					m.Selected[list[m.Cursor].ID] = !m.Selected[list[m.Cursor].ID]
				}
			} else if m.Field == 3 && len(m.MCPOptions) > 0 {
				m.MCPSelected[m.MCPOptions[m.Cursor].ID] = !m.MCPSelected[m.MCPOptions[m.Cursor].ID]
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
	} else if m.Field >= 4 && m.Field <= 6 {
		m.loadMCPDraft()
		id := m.currentMCPID()
		draft := m.MCPDrafts[id]
		idx := m.Field - 4
		value := mcpDraftValue(draft, m.Field)
		touched := m.MCPTouched[id]
		if !touched[idx] {
			value = ""
			touched[idx] = true
		}
		value += string(runes)
		setMCPDraftValue(&draft, m.Field, value)
		m.MCPDrafts[draft.ID] = draft
		m.MCPTouched[id] = touched
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
	if m.Field >= 4 && m.Field <= 6 {
		m.loadMCPDraft()
		draft := m.MCPDrafts[m.currentMCPID()]
		value := mcpDraftValue(draft, m.Field)
		if len(value) > 0 {
			runes := []rune(value)
			setMCPDraftValue(&draft, m.Field, string(runes[:len(runes)-1]))
			m.MCPDrafts[draft.ID] = draft
		}
	}
}

func (m *ContextEditorModel) currentMCPID() string {
	if m.Cursor < 0 || m.Cursor >= len(m.MCPOptions) {
		return ""
	}
	return m.MCPOptions[m.Cursor].ID
}

func (m *ContextEditorModel) loadMCPDraft() {
	id := m.currentMCPID()
	if id != "" {
		if _, ok := m.MCPDrafts[id]; !ok {
			m.MCPDrafts[id] = model.MCPRef{ID: id}
		}
	}
}

func mcpDraftValue(ref model.MCPRef, field int) string {
	switch field {
	case 4:
		return ref.Alias
	case 5:
		return strings.Join(ref.Tools.Allow, ",")
	case 6:
		return strings.Join(ref.Tools.Deny, ",")
	default:
		return ""
	}
}

func setMCPDraftValue(ref *model.MCPRef, field int, value string) {
	items := func() []string {
		var out []string
		for _, item := range strings.Split(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	}
	switch field {
	case 4:
		ref.Alias = value
	case 5:
		ref.Tools.Allow = items()
	case 6:
		ref.Tools.Deny = items()
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
	var mcpRefs []model.MCPRef
	for _, o := range m.MCPOptions {
		if m.MCPSelected[o.ID] {
			ref := m.MCPDrafts[o.ID]
			ref.ID = o.ID
			mcpRefs = append(mcpRefs, ref)
		}
	}
	if len(refs) == 0 && len(mcpRefs) == 0 && len(m.Context.MCPRefs) == 0 && len(m.Context.MCPs) == 0 {
		return fmt.Errorf("select at least one skill or MCP")
	}
	m.Context.Skills = refs
	if len(m.MCPOptions) > 0 {
		m.Context.MCPRefs = mcpRefs
		m.Context.MCPs = nil
		for _, r := range mcpRefs {
			m.Context.MCPs = append(m.Context.MCPs, r.ID)
		}
	}
	return nil
}
func (m *ContextEditorModel) View() string {
	var b strings.Builder
	b.WriteString(renderHeading("Context editor", m.NoColor) + "\n")
	b.WriteString(contextEditorSteps(m.contextEditorPage(), m.NoColor) + "\n\n")
	switch m.contextEditorPage() {
	case 0:
		b.WriteString(renderHeading("1. Name and definition", m.NoColor) + "\n\n")
		b.WriteString(fmt.Sprintf("Name: %s%s\nDescription: %s%s\n", m.Context.Name, fieldMark(m.Field == 0), m.Context.Description, fieldMark(m.Field == 1)))
		b.WriteString("\nUse Tab to move between fields and continue to Skills.\n")
	case 1:
		b.WriteString(renderHeading("2. Skills", m.NoColor) + "\n\n")
		list := m.filtered()
		if m.Filtering {
			b.WriteString("Filter: " + m.Filter + "_\n\n")
		} else if m.Filter != "" {
			b.WriteString("Filter: " + m.Filter + "  (press / to edit)\n\n")
		}
		if len(list) == 0 {
			b.WriteString("No skills available for selection.\n")
		}
		for i, o := range list {
			marker := "  "
			if i == m.Cursor {
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
		b.WriteString("\nSpace selects • / filters • Tab moves to MCPs\n")
	case 2:
		b.WriteString(renderHeading("3. MCPs", m.NoColor) + "\n\n")
		if len(m.MCPOptions) == 0 {
			b.WriteString("No MCPs registered. Use cms mcp install before adding one to a context.\n")
		} else {
			for i, o := range m.MCPOptions {
				marker := "  "
				if m.Field == 3 && i == m.Cursor {
					marker = "> "
				}
				check := "[ ]"
				if m.MCPSelected[o.ID] {
					check = "[x]"
				}
				b.WriteString(fmt.Sprintf("%s%s %s\n", marker, check, o.Name))
			}
			if m.Field >= 4 && m.currentMCPID() != "" {
				m.loadMCPDraft()
				draft := m.MCPDrafts[m.currentMCPID()]
				label := m.currentMCPID()
				for _, option := range m.MCPOptions {
					if option.ID == m.currentMCPID() {
						label = option.Name
						break
					}
				}
				b.WriteString(fmt.Sprintf("\nMCP %s\n  ID: %s\n  Alias: %s%s\n  Allow: %s%s\n  Deny: %s%s\n", label, m.currentMCPID(), draft.Alias, fieldMark(m.Field == 4), strings.Join(draft.Tools.Allow, ","), fieldMark(m.Field == 5), strings.Join(draft.Tools.Deny, ","), fieldMark(m.Field == 6)))
				b.WriteString("  Tool-filter compatibility:\n")
				for _, target := range []string{"codex", "claude", "antigravity", "cursor", "opencode"} {
					b.WriteString(fmt.Sprintf("    %-12s %s\n", target, toolFilterCompatibility(target)))
				}
			}
		}
		b.WriteString("\nSpace selects • Tab navigates details and returns to the beginning • Shift+Tab goes back\n")
	}
	if m.Err != nil {
		b.WriteString("\nError: " + m.Err.Error())
	}
	b.WriteString("\nCtrl+S save • Esc cancel")
	return b.String()
}

func (m *ContextEditorModel) contextEditorPage() int {
	switch {
	case m.Field <= 1:
		return 0
	case m.Field == 2:
		return 1
	default:
		return 2
	}
}

func contextEditorSteps(active int, noColor bool) string {
	steps := []string{"1. Name and definition", "2. Skills", "3. MCPs"}
	for i, step := range steps {
		if i == active {
			steps[i] = "[ " + renderHeading(step, noColor) + " ]"
		} else {
			steps[i] = "  " + step + "  "
		}
	}
	return strings.Join(steps, "  →  ")
}

func toolFilterCompatibility(target string) string {
	adapter, ok := harness.Builtins()[target].(harness.MCPAdapter)
	if !ok {
		return "unsupported"
	}
	support := adapter.ToolFilterSupport()
	switch {
	case support.Allow && support.Deny:
		return "complete (allow + deny)"
	case support.Allow:
		return "partial (allow only)"
	case support.Deny:
		return "partial (deny only)"
	default:
		return "unsupported"
	}
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
func RunContextEditorWithMCP(registry skills.Registry, mcpRegistry mcps.Registry, initial model.Context, in io.Reader, out io.Writer) (model.Context, bool, error) {
	m := NewContextEditorWithMCP(registry, mcpRegistry, initial)
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
