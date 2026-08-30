package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/ikts/cms/internal/model"
)

func TestMCPListViewHasTableAndDetails(t *testing.T) {
	mcp := model.MCPMetadata{
		Name:        "demo",
		ID:          "demo@abc123",
		Description: "A useful demo server.",
		Source:      model.MCPSource{Type: model.MCPSourceRemote},
		Transport:   model.MCPTransportHTTP,
		Remote:      &model.MCPRemote{URL: "https://demo.test/mcp", Auth: model.MCPAuth{Mode: "bearer-env", Env: "DEMO_TOKEN"}, Headers: map[string]string{"Authorization": "${DEMO_TOKEN}"}},
		Requirements: []model.MCPRequirement{{
			Kind: "env", Name: "DEMO_TOKEN", Required: true, Secret: true,
		}},
		Reproducible: true,
	}
	m := NewMCPListModel([]model.MCPMetadata{mcp})
	m.Width = 100
	m.Filtering = false
	view := m.View()
	for _, expected := range []string{"Registered MCPs", "NAME", "TRANSPORT", "remote: demo.test", "A useful demo server.", "https://demo.test/mcp", "bearer-env", "DEMO_TOKEN", "Reproducible: yes"} {
		if !strings.Contains(view, expected) {
			t.Errorf("view does not contain %q:\n%s", expected, view)
		}
	}
	if strings.Contains(view, "${DEMO_TOKEN}") {
		t.Fatalf("view exposed a secret value:\n%s", view)
	}
}

func TestMCPListFiltersByRelevantFields(t *testing.T) {
	m := NewMCPListModel([]model.MCPMetadata{
		{Name: "remote-demo", ID: "remote-demo@abc123", Description: "HTTP server", Source: model.MCPSource{Type: model.MCPSourceRemote}, Transport: model.MCPTransportHTTP, Remote: &model.MCPRemote{URL: "https://demo.test/mcp"}},
		{Name: "local-tools", ID: "local-tools@def456", Description: "Local tools", Source: model.MCPSource{Type: model.MCPSourceCommand}, Transport: model.MCPTransportStdio, Command: &model.MCPCommand{Command: "node"}},
	})
	if m.Filtering {
		t.Fatal("MCP list should start focused on the list")
	}
	m.Filter = "demo.test"
	if items := m.filtered(); len(items) != 1 || items[0].Name != "remote-demo" {
		t.Fatalf("filtered MCPs = %#v", items)
	}

	m.Filtering = false
	m.Filter = ""
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := updated.(*MCPListModel).Cursor; got != 1 {
		t.Fatalf("cursor after down = %d, want 1", got)
	}
}

func TestMCPListUsesSlidingWindow(t *testing.T) {
	mcps := make([]model.MCPMetadata, 15)
	for i := range mcps {
		mcps[i] = model.MCPMetadata{
			Name:      fmt.Sprintf("server-%02d", i),
			ID:        fmt.Sprintf("server-%02d@abc123", i),
			Source:    model.MCPSource{Type: model.MCPSourceCommand},
			Transport: model.MCPTransportStdio,
			Command:   &model.MCPCommand{Command: "server"},
		}
	}
	m := NewMCPListModel(mcps)
	m.Width = 100
	m.Filtering = false
	view := m.View()
	if !strings.Contains(view, "Showing 1–8 of 15") || strings.Contains(view, "server-08") {
		t.Fatalf("initial MCP window is incorrect:\n%s", view)
	}

	for i := 0; i < 8; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(*MCPListModel)
	}
	view = m.View()
	if !strings.Contains(view, "Showing 2–9 of 15") || !strings.Contains(view, "server-08") || strings.Contains(view, "server-00") {
		t.Fatalf("sliding MCP window is incorrect:\n%s", view)
	}
}
