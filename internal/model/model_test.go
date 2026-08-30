package model

import "testing"

func TestMCPDisplayLabelsDisambiguateDuplicateNames(t *testing.T) {
	items := []MCPMetadata{
		{ID: "mcp@11111111", Name: "mcp", Source: MCPSource{Type: MCPSourceRegistry, RegistryName: "com.figma/mcp"}},
		{ID: "mcp@22222222", Name: "mcp", Source: MCPSource{Type: MCPSourceRegistry, RegistryName: "com.supabase/mcp"}},
		{ID: "github@33333333", Name: "github", Source: MCPSource{Type: MCPSourceRegistry, RegistryName: "io.github/github"}},
	}
	labels := MCPDisplayLabels(items)
	if labels[items[0].ID] != "mcp · com.figma/mcp" || labels[items[1].ID] != "mcp · com.supabase/mcp" {
		t.Fatalf("duplicate labels = %#v", labels)
	}
	if labels[items[2].ID] != "github" {
		t.Fatalf("unique label = %q", labels[items[2].ID])
	}
}

func TestMCPDisplayLabelsFallbackToHashWhenOriginsMatch(t *testing.T) {
	items := []MCPMetadata{
		{ID: "mcp@11111111", Name: "mcp", Source: MCPSource{Type: MCPSourceRegistry, RegistryName: "same"}},
		{ID: "mcp@22222222", Name: "mcp", Source: MCPSource{Type: MCPSourceRegistry, RegistryName: "same"}},
	}
	labels := MCPDisplayLabels(items)
	if labels[items[0].ID] == labels[items[1].ID] || labels[items[0].ID] != "mcp · same" || labels[items[1].ID] != "mcp · same · 22222222" {
		t.Fatalf("same-origin labels = %#v", labels)
	}
}
