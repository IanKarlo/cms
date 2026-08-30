package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikts/cms/internal/harness"
	"github.com/ikts/cms/internal/mcps"
	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/providers"
	"github.com/ikts/cms/internal/storage"
)

type fixedMCPProvider struct{ variants []providers.MCPVariant }

func (p fixedMCPProvider) Search(context.Context, string, int) ([]providers.MCPSearchResult, error) {
	return nil, nil
}
func (p fixedMCPProvider) Resolve(context.Context, providers.MCPProviderRef) ([]providers.MCPVariant, error) {
	return p.variants, nil
}

func TestImportCodexMultilineTOML(t *testing.T) {
	data := []byte(`model = "gpt-5"

[mcp_servers."demo-server"]
command = "npx"
args = [
  "-y",
  "@example/demo,with-comma",
]
env = {
  "TOKEN" = "${DEMO_TOKEN}",
  "LABEL" = "${PUBLIC_LABEL}",
}

[mcp_servers.remote]
url = "https://demo.test/mcp"
bearer_token_env_var = "DEMO_TOKEN"
http_headers = {
  "X-Label" = "prefix,${PUBLIC_LABEL}",
}
`)
	got, err := importMCPText(data, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("imported %d MCPs: %#v", len(got), got)
	}
	if got[0].Name != "demo-server" || !reflect.DeepEqual(got[0].Command.Args, []string{"-y", "@example/demo,with-comma"}) || got[0].Command.Env["TOKEN"] != "${DEMO_TOKEN}" {
		t.Fatalf("stdio import=%#v", got[0])
	}
	if got[1].Transport != model.MCPTransportHTTP || got[1].Remote.Auth.Env != "DEMO_TOKEN" || got[1].Remote.Headers["X-Label"] != "prefix,${PUBLIC_LABEL}" {
		t.Fatalf("HTTP import=%#v", got[1])
	}
}

func TestImportJSONForEveryJSONHarness(t *testing.T) {
	tests := []struct {
		target string
		data   string
		want   string
	}{
		{"claude", `{"mcpServers":{"demo":{"type":"stdio","command":"npx","args":["-y","demo"],"env":{"TOKEN":"${TOKEN}"}}}}`, "${TOKEN}"},
		{"antigravity", `{"mcpServers":{"demo":{"serverUrl":"https://demo.test/mcp"}}}`, "https://demo.test/mcp"},
		{"cursor", `{"mcpServers":{"demo":{"type":"http","url":"https://demo.test/mcp","headers":{"Authorization":"Bearer ${env:TOKEN}"}}}}`, "Bearer ${TOKEN}"},
		{"opencode", `{"mcp":{"demo":{"type":"local","command":["npx","-y","demo"],"environment":{"TOKEN":"{env:TOKEN}"}}}}`, "${TOKEN}"},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			got, err := importMCPJSON([]byte(tt.data), tt.target)
			if err != nil || len(got) != 1 {
				t.Fatalf("import=%#v err=%v", got, err)
			}
			serialized := commandValues(got[0])
			if !strings.Contains(serialized, tt.want) {
				t.Fatalf("import=%#v does not contain %q", got[0], tt.want)
			}
		})
	}
}

func commandValues(m model.MCPMetadata) string {
	var out strings.Builder
	if m.Command != nil {
		out.WriteString(m.Command.Command)
		out.WriteString(strings.Join(m.Command.Args, ""))
		for _, value := range m.Command.Env {
			out.WriteString(value)
		}
	}
	if m.Remote != nil {
		out.WriteString(m.Remote.URL)
		for _, value := range m.Remote.Headers {
			out.WriteString(value)
		}
	}
	return out.String()
}

func TestInferMCPTargetFromStandardPaths(t *testing.T) {
	tests := map[string]string{
		"/project/.mcp.json":                        "claude",
		"/home/user/.claude.json":                   "claude",
		"/project/.agents/mcp_config.json":          "antigravity",
		"/project/.cursor/mcp.json":                 "cursor",
		"/home/user/.config/opencode/opencode.json": "opencode",
		"/project/.codex/config.toml":               "codex",
	}
	for path, want := range tests {
		if got := inferMCPTarget(path); got != want {
			t.Errorf("inferMCPTarget(%q)=%q, want %q", path, got, want)
		}
	}
}

func TestApplyMCPPlansRollsBackEarlierConfig(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.json")
	original := []byte("original\n")
	if err := os.WriteFile(first, original, 0o640); err != nil {
		t.Fatal(err)
	}
	blockedParent := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blockedParent, []byte("block"), 0o644); err != nil {
		t.Fatal(err)
	}
	action := []harness.MCPAction{{Kind: harness.MCPUpdate, Name: "demo"}}
	plans := []plannedMCP{
		{path: first, plan: harness.MCPPlan{Actions: action, Data: []byte("changed\n")}},
		{path: filepath.Join(blockedParent, "second.json"), plan: harness.MCPPlan{Actions: action, Data: []byte("second\n")}},
	}
	if _, err := applyMCPPlans(plans); err == nil {
		t.Fatal("applyMCPPlans() succeeded despite second write failure")
	}
	got, err := os.ReadFile(first)
	if err != nil || !reflect.DeepEqual(got, original) {
		t.Fatalf("first config was not rolled back: %q err=%v", got, err)
	}
}

func TestPreflightRejectsAbsoluteNonExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime")
	if err := os.WriteFile(path, []byte("not executable"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := preflightMCP(model.MCPMetadata{Name: "demo", Transport: model.MCPTransportStdio, Command: &model.MCPCommand{Command: path}})
	var preflight MCPPreflightError
	if !errors.As(err, &preflight) || preflight.Code != "mcp_runtime_missing" || preflight.Runtime != path {
		t.Fatalf("preflight error=%#v", err)
	}
}

func TestRegistryVariantErrorsAreArgumentErrorsAndListChoices(t *testing.T) {
	app := &App{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, In: strings.NewReader(""), MCPProvider: fixedMCPProvider{variants: []providers.MCPVariant{{Variant: "npm"}, {Variant: "remote"}}}}
	err := app.mcpInstall([]string{"registry:demo", "--variant", "missing"})
	if codeOf(err) != 2 {
		t.Fatalf("code=%d err=%v, want argument error", codeOf(err), err)
	}
	var selection MCPVariantSelectionError
	if !errors.As(err, &selection) || !reflect.DeepEqual(selection.Variants, []string{"npm", "remote"}) || selection.Requested != "missing" {
		t.Fatalf("variant error=%#v", err)
	}
}

func TestMCPListPlainIncludesDisplayNameAndMetadata(t *testing.T) {
	app := &App{
		Out:  &bytes.Buffer{},
		In:   strings.NewReader(""),
		MCPs: mcps.NewRegistry(storage.Paths{DataDir: t.TempDir()}),
	}
	first, _, err := app.MCPs.Register(model.MCPMetadata{
		Name: "mcp", Source: model.MCPSource{Type: model.MCPSourceRegistry, RegistryName: "com.figma/mcp"},
		Transport: model.MCPTransportHTTP, Remote: &model.MCPRemote{URL: "https://figma.test/mcp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := app.MCPs.Register(model.MCPMetadata{
		Name: "mcp", Source: model.MCPSource{Type: model.MCPSourceRegistry, RegistryName: "com.supabase/mcp"},
		Transport: model.MCPTransportHTTP, Remote: &model.MCPRemote{URL: "https://supabase.test/mcp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.mcpList([]string{"--plain"}); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(app.Out.(*bytes.Buffer).String()), "\n")
	if len(lines) != 3 || !reflect.DeepEqual(strings.Fields(lines[0]), []string{"DISPLAY_NAME", "ID", "TRANSPORT", "SOURCE"}) {
		t.Fatalf("plain output has invalid header or line count:\n%s", app.Out.(*bytes.Buffer).String())
	}
	got := map[string]string{}
	for _, line := range lines[1:] {
		for _, id := range []string{first.ID, second.ID} {
			if strings.Contains(line, id) {
				got[id] = line
			}
		}
	}
	for id, wantName := range map[string]string{
		first.ID:  "mcp · com.figma/mcp",
		second.ID: "mcp · com.supabase/mcp",
	} {
		line, ok := got[id]
		if !ok || !strings.Contains(line, wantName) || !strings.Contains(line, "streamable-http") || !strings.Contains(line, "registry:") {
			t.Fatalf("plain metadata for %s = %q", id, line)
		}
	}
}
