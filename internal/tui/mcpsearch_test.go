package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/ikts/cms/internal/providers"
)

func TestMCPRegistrySearchViewRendersTableLinkAndDetails(t *testing.T) {
	m := NewMCPRegistrySearchModel(nil, 30)
	m.Width = 100
	m.Query = "demo"
	m.InputMode = false
	m.Results = []providers.MCPSearchResult{{
		Name: "io.example/demo", Title: "Demo MCP", Version: "2.0.0", Description: "A useful demo server.",
		WebsiteURL: "https://demo.test", RepositoryURL: "https://github.com/example/demo", RepositorySource: "github",
		Status: "active", IsLatest: true,
		Packages: []providers.MCPRegistryPackage{{RegistryType: "npm", Identifier: "@example/demo", Version: "2.0.0", RuntimeHint: "npx"}},
		Remotes:  []providers.MCPRegistryRemote{{Type: "streamable-http", URL: "https://demo.test/mcp"}},
	}}
	view := m.View()
	for _, want := range []string{"NAME", "VERSION", "LINK", "io.example/demo", "https://github.com/example/demo", "Details", "Demo MCP", "Packages:", "npm: @example/demo", "Remote transports:", "active"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestMCPRegistrySearchSelectsHighlightedResult(t *testing.T) {
	m := NewMCPRegistrySearchModel(nil, 30)
	m.InputMode = false
	m.Results = []providers.MCPSearchResult{{Name: "demo", Version: "1.0.0"}}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("selecting an MCP should quit the TUI")
	}
	result := updated.(*MCPRegistrySearchModel)
	if result.Chosen == nil || result.Chosen.Name != "demo" {
		t.Fatalf("chosen result=%#v", result.Chosen)
	}
}

func TestMCPRegistrySearchTableKeepsHeadingsVisibleInNarrowTerminals(t *testing.T) {
	nameWidth, versionWidth, linkWidth := mcpTableWidths(50, []providers.MCPSearchResult{{
		Name: "io.example/a-very-long-server-name", Version: "2026.08.29", RepositoryURL: "https://github.com/example/server",
	}})
	if nameWidth < len("NAME") || versionWidth < len("VERSION") || linkWidth < len("LINK") {
		t.Fatalf("column widths=%d,%d,%d cannot display headings", nameWidth, versionWidth, linkWidth)
	}
	view := NewMCPRegistrySearchModel(nil, 30)
	view.Width = 50
	view.InputMode = false
	view.Results = []providers.MCPSearchResult{{Name: "io.example/a-very-long-server-name", Version: "2026.08.29", RepositoryURL: "https://github.com/example/server"}}
	output := view.View()
	for _, heading := range []string{"NAME", "VERSION", "LINK"} {
		if !strings.Contains(output, heading) {
			t.Fatalf("narrow view lost %q:\n%s", heading, output)
		}
	}
}

func TestMCPRegistrySearchUsesSlidingResultWindow(t *testing.T) {
	results := make([]providers.MCPSearchResult, 15)
	for i := range results {
		results[i] = providers.MCPSearchResult{Name: fmt.Sprintf("server-%02d", i), Version: "1.0.0", RepositoryURL: "https://example.test"}
	}
	m := NewMCPRegistrySearchModel(nil, 30)
	m.Width = 100
	m.Height = 60 // a large terminal must not expand the result window
	m.InputMode = false
	m.Results = results
	view := m.View()
	if !strings.Contains(view, "Showing 1–8 of 15") || strings.Contains(view, "server-08") {
		t.Fatalf("initial result window is incorrect:\n%s", view)
	}
	for i := 0; i < 8; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(*MCPRegistrySearchModel)
	}
	view = m.View()
	if !strings.Contains(view, "Showing 2–9 of 15") || !strings.Contains(view, "server-08") || strings.Contains(view, "server-00") {
		t.Fatalf("sliding result window is incorrect:\n%s", view)
	}
}
