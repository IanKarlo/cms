package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikts/cms/internal/model"
)

func TestMCPRegistryIsIdempotentAndParametric(t *testing.T) {
	p := Paths{DataDir: filepath.Join(t.TempDir(), "data")}
	r := MCPRegistry{Paths: p}
	m := model.MCPMetadata{Name: "github", Transport: model.MCPTransportHTTP, Source: model.MCPSource{Type: model.MCPSourceRemote}, Remote: &model.MCPRemote{URL: "https://example.test/mcp", Headers: map[string]string{"Authorization": "Bearer ${GITHUB_TOKEN}"}}, Requirements: []model.MCPRequirement{{Kind: "env", Name: "GITHUB_TOKEN", Required: true, Secret: true}}}
	if err := r.Save(m); err != nil {
		t.Fatal(err)
	}
	id := m.CanonicalID()
	got, err := r.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != id || got.Remote.Headers["Authorization"] != "Bearer ${GITHUB_TOKEN}" {
		t.Fatalf("got %#v", got)
	}
	if err := r.Save(m); err != nil {
		t.Fatal(err)
	}
	list, err := r.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%#v err=%v", list, err)
	}
}

func TestValidateMCPRejectsInvalidDomainValues(t *testing.T) {
	valid := model.MCPMetadata{Name: "demo", Source: model.MCPSource{Type: model.MCPSourceRemote}, Transport: model.MCPTransportHTTP, Remote: &model.MCPRemote{URL: "https://demo.test/mcp"}}
	tests := []struct {
		name string
		edit func(*model.MCPMetadata)
		want string
	}{
		{"source", func(m *model.MCPMetadata) { m.Source.Type = "unknown" }, "source type"},
		{"URL scheme", func(m *model.MCPMetadata) { m.Remote.URL = "file:///tmp/mcp" }, "HTTP(S) URL"},
		{"package type", func(m *model.MCPMetadata) {
			m.Source = model.MCPSource{Type: model.MCPSourcePackage, PackageType: "cargo", PackageIdentifier: "demo"}
		}, "package type"},
		{"package identifier", func(m *model.MCPMetadata) {
			m.Source = model.MCPSource{Type: model.MCPSourcePackage, PackageType: "npm"}
		}, "package identifier"},
		{"requirement kind", func(m *model.MCPMetadata) { m.Requirements = []model.MCPRequirement{{Kind: "file", Name: "x"}} }, "requirement kind"},
		{"bearer env", func(m *model.MCPMetadata) { m.Remote.Auth.Mode = "bearer-env" }, "auth environment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := valid
			remote := *valid.Remote
			m.Remote = &remote
			tt.edit(&m)
			if err := ValidateMCP(m); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateMCP() error=%v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestContextReadsNestedMCPToolsTable(t *testing.T) {
	root := t.TempDir()
	p := Paths{DataDir: filepath.Join(root, "data")}
	if err := EnsureDirs(p); err != nil {
		t.Fatal(err)
	}
	mcp := model.MCPMetadata{Name: "demo", Transport: model.MCPTransportHTTP, Source: model.MCPSource{Type: model.MCPSourceRemote}, Remote: &model.MCPRemote{URL: "https://demo.test/mcp"}}
	mcp.ID = mcp.CanonicalID()
	if err := (MCPRegistry{Paths: p}).Save(mcp); err != nil {
		t.Fatal(err)
	}
	contextPath := filepath.Join(p.ContextsDir(), "demo.toml")
	content := "schema_version = 2\nname = \"demo\"\n\n[[mcps]]\nid = \"" + mcp.ID + "\"\n\n[mcps.tools]\nallow = [\"search\"]\ndeny = [\"delete\"]\n"
	if err := os.WriteFile(contextPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := (ContextStore{Paths: p, Registry: Registry{Paths: p}}).Get("demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.MCPRefs) != 1 || len(c.MCPRefs[0].Tools.Allow) != 1 || c.MCPRefs[0].Tools.Allow[0] != "search" || len(c.MCPRefs[0].Tools.Deny) != 1 {
		t.Fatalf("context=%#v", c)
	}
}
