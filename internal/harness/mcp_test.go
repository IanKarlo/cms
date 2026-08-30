package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikts/cms/internal/model"
)

func TestCursorMCPPlanConvertsEnvAndPreservesManualEntry(t *testing.T) {
	current := []byte(`{"settings":{"keep":true},"mcpServers":{"manual":{"url":"https://manual.test"}}}`)
	m := model.MCPMetadata{ID: "github@12345678", Name: "github", Transport: model.MCPTransportHTTP, Remote: &model.MCPRemote{URL: "https://example.test/mcp", Headers: map[string]string{"Authorization": "Bearer ${GITHUB_TOKEN}"}}}
	p, err := (Cursor{}).PlanMCPMutation(current, []model.ResolvedMCP{{Metadata: m, Ref: model.MCPRef{ID: m.ID, Tools: model.MCPToolFilter{Allow: []string{"search"}}}}}, nil, model.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Conflicts()) != 0 {
		t.Fatal(p.Conflicts())
	}
	if !strings.Contains(string(p.Data), "${env:GITHUB_TOKEN}") {
		t.Fatalf("data=%s", p.Data)
	}
	var root map[string]any
	if err := json.Unmarshal(p.Data, &root); err != nil {
		t.Fatal(err)
	}
	if root["settings"].(map[string]any)["keep"] != true {
		t.Fatal("manual settings were not preserved")
	}
	if _, ok := root["mcpServers"].(map[string]any)["manual"]; !ok {
		t.Fatal("manual MCP was removed")
	}
	if len(p.Warnings) != 1 {
		t.Fatalf("warnings=%#v", p.Warnings)
	}
}

func TestCodexPlanPreservesUnrelatedTOML(t *testing.T) {
	current := []byte("model = \"gpt\"\n\n[features]\nturn = true\n\n[mcp_servers.manual]\ncommand = \"manual\"\n")
	m := model.MCPMetadata{ID: "demo@12345678", Name: "demo", Transport: model.MCPTransportStdio, Command: &model.MCPCommand{Command: "npx", Args: []string{"-y", "demo"}}}
	p, err := (Codex{}).PlanMCPMutation(current, []model.ResolvedMCP{{Metadata: m, Ref: model.MCPRef{ID: m.ID}}}, nil, model.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Conflicts()) != 0 {
		t.Fatal(p.Conflicts())
	}
	for _, want := range []string{"model = \"gpt\"", "turn = true", "[mcp_servers.manual]", "[mcp_servers.demo]"} {
		if !strings.Contains(string(p.Data), want) {
			t.Fatalf("missing %q in %s", want, p.Data)
		}
	}
}

func TestCodexQuotedManualServerNameConflicts(t *testing.T) {
	current := []byte("[mcp_servers.\"demo-server\"]\ncommand = \"manual\"\n")
	mcp := model.MCPMetadata{ID: "demo-server@12345678", Name: "demo-server", Transport: model.MCPTransportStdio, Command: &model.MCPCommand{Command: "npx"}}
	plan, err := (Codex{}).PlanMCPMutation(current, []model.ResolvedMCP{{Metadata: mcp, Ref: model.MCPRef{ID: mcp.ID}}}, nil, model.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts()) != 1 {
		t.Fatalf("quoted manual server actions=%#v, want conflict", plan.Actions)
	}
}

func TestOpenCodeDenyUsesTopLevelManagedToolRules(t *testing.T) {
	m := model.MCPMetadata{ID: "demo@12345678", Name: "demo", Transport: model.MCPTransportHTTP, Remote: &model.MCPRemote{URL: "https://demo.test/mcp"}}
	p, err := (OpenCode{}).PlanMCPMutation(nil, []model.ResolvedMCP{{Metadata: m, Ref: model.MCPRef{ID: m.ID, Tools: model.MCPToolFilter{Deny: []string{"delete"}}}}}, nil, model.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(p.Data, &root); err != nil {
		t.Fatal(err)
	}
	if _, ok := root["mcp"].(map[string]any)["demo"].(map[string]any)["tools"]; ok {
		t.Fatal("tool rule should not be nested in MCP entry")
	}
	if got := root["tools"].(map[string]any)["demo_delete"]; got != false {
		t.Fatalf("got tool rule %#v", got)
	}
	if len(p.Entries) != 1 || len(p.Entries[0].ManagedKeys) != 2 {
		t.Fatalf("state keys=%#v", p.Entries)
	}
}

func TestOpenCodeDenyUpdateRemovesStaleManagedRule(t *testing.T) {
	m := model.MCPMetadata{ID: "demo@12345678", Name: "demo", Transport: model.MCPTransportHTTP, Remote: &model.MCPRemote{URL: "https://demo.test/mcp"}}
	current := []byte(`{"mcp":{"demo":{"type":"remote","url":"https://demo.test/mcp","enabled":true}},"tools":{"demo_old":false}}`)
	managed := []model.MCPStateEntry{{MCPID: m.ID, Target: "opencode", Scope: "project", Name: "demo", Fingerprint: fingerprintJSON(map[string]any{"type": "remote", "url": "https://demo.test/mcp", "enabled": true}), ManagedKeys: []string{"mcp.demo", "tools.demo_old"}}}
	p, err := (OpenCode{}).PlanMCPMutation(current, []model.ResolvedMCP{{Metadata: m, Ref: model.MCPRef{ID: m.ID, Tools: model.MCPToolFilter{Deny: []string{"new"}}}}}, managed, model.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Conflicts()) != 0 {
		t.Fatal(p.Conflicts())
	}
	var root map[string]any
	if err := json.Unmarshal(p.Data, &root); err != nil {
		t.Fatal(err)
	}
	tools := root["tools"].(map[string]any)
	if _, ok := tools["demo_old"]; ok {
		t.Fatal("stale managed rule was not removed")
	}
	if tools["demo_new"] != false {
		t.Fatalf("new rule=%#v", tools["demo_new"])
	}
}

func TestOpenCodeManualToolRuleConflicts(t *testing.T) {
	m := model.MCPMetadata{ID: "demo@12345678", Name: "demo", Transport: model.MCPTransportHTTP, Remote: &model.MCPRemote{URL: "https://demo.test/mcp"}}
	current := []byte(`{"mcp":{},"tools":{"demo_delete":false}}`)
	p, err := (OpenCode{}).PlanMCPMutation(current, []model.ResolvedMCP{{Metadata: m, Ref: model.MCPRef{ID: m.ID, Tools: model.MCPToolFilter{Deny: []string{"delete"}}}}}, nil, model.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Conflicts()) != 1 {
		t.Fatalf("expected conflict, got %#v", p.Actions)
	}
}

func TestJSONCommentsStayAnchoredToTheirNearbyKeys(t *testing.T) {
	original := []byte(`{
  // keep project settings here
  "settings": {"keep": true},
  "mcpServers": {
    /* manually maintained server */
    "manual": {"url": "https://manual.test/mcp"},
    "urlEntry": {"url": "https://url.test/mcp"} // preserve this note
  }
}`)
	rendered := []byte("{\n  \"mcpServers\": {\n    \"manual\": {\"url\": \"https://manual.test/mcp\"},\n    \"urlEntry\": {\"url\": \"https://url.test/mcp\"}\n  },\n  \"settings\": {\n    \"keep\": true\n  }\n}\n")
	got := string(preserveJSONComments(original, rendered))
	for _, comment := range []string{"// keep project settings here", "/* manually maintained server */", "// preserve this note"} {
		if !strings.Contains(got, comment) {
			t.Fatalf("comment %q was lost: %s", comment, got)
		}
	}
	if strings.Index(got, "// keep project settings here") > strings.Index(got, `"settings"`) {
		t.Fatal("settings comment was detached from its key")
	}
	if strings.Index(got, "/* manually maintained server */") > strings.Index(got, `"manual"`) {
		t.Fatal("manual server comment was detached from its key")
	}
	if strings.Index(got, `"urlEntry"`) > strings.Index(got, "// preserve this note") {
		t.Fatal("trailing comment was detached from its key")
	}
}

func TestJSONCPlanPreservesUnmanagedBytesExactly(t *testing.T) {
	current := []byte(`{
  // this entire setting is user-owned
  "settings": { "theme": "dark", "nested": [1, 2, 3] },
  "mcpServers": {
    /* manual server stays exactly as written */
    "manual": { "url": "https://manual.test/mcp", "custom": true } // trailing note
  },
  "tail": true
}
`)
	mcp := model.MCPMetadata{ID: "cms@12345678", Name: "cms", Transport: model.MCPTransportHTTP, Remote: &model.MCPRemote{URL: "https://cms.test/mcp"}}
	plan, err := (Claude{}).PlanMCPMutation(current, []model.ResolvedMCP{{Metadata: mcp, Ref: model.MCPRef{ID: mcp.ID}}}, nil, model.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	for _, exact := range []string{
		"// this entire setting is user-owned\n  \"settings\": { \"theme\": \"dark\", \"nested\": [1, 2, 3] },",
		"/* manual server stays exactly as written */\n    \"manual\": { \"url\": \"https://manual.test/mcp\", \"custom\": true }, // trailing note",
		"\"tail\": true",
	} {
		if !strings.Contains(string(plan.Data), exact) {
			t.Fatalf("unmanaged bytes changed; missing %q in:\n%s", exact, plan.Data)
		}
	}
	if err := json.Unmarshal(stripJSONComments(plan.Data), &map[string]any{}); err != nil {
		t.Fatalf("patched JSONC is invalid: %v\n%s", err, plan.Data)
	}
}

func TestFilterChangesAreUpdatesNotKeeps(t *testing.T) {
	mcp := model.MCPMetadata{ID: "demo@12345678", Name: "demo", Transport: model.MCPTransportHTTP, Remote: &model.MCPRemote{URL: "https://demo.test/mcp"}}
	first, err := (OpenCode{}).PlanMCPMutation(nil, []model.ResolvedMCP{{Metadata: mcp, Ref: model.MCPRef{ID: mcp.ID, Tools: model.MCPToolFilter{Deny: []string{"old"}}}}}, nil, model.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	second, err := (OpenCode{}).PlanMCPMutation(first.Data, []model.ResolvedMCP{{Metadata: mcp, Ref: model.MCPRef{ID: mcp.ID, Tools: model.MCPToolFilter{Deny: []string{"new"}}}}}, first.Entries, model.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Actions) != 1 || second.Actions[0].Kind != MCPUpdate {
		t.Fatalf("filter change actions=%#v, want UPDATE", second.Actions)
	}

	codexFirst, err := (Codex{}).PlanMCPMutation(nil, []model.ResolvedMCP{{Metadata: mcp, Ref: model.MCPRef{ID: mcp.ID, Tools: model.MCPToolFilter{Allow: []string{"old"}}}}}, nil, model.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	codexSecond, err := (Codex{}).PlanMCPMutation(codexFirst.Data, []model.ResolvedMCP{{Metadata: mcp, Ref: model.MCPRef{ID: mcp.ID, Tools: model.MCPToolFilter{Allow: []string{"new"}}}}}, codexFirst.Entries, model.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(codexSecond.Actions) != 1 || codexSecond.Actions[0].Kind != MCPUpdate {
		t.Fatalf("Codex filter change actions=%#v, want UPDATE", codexSecond.Actions)
	}
}

func TestHarnessFixturesCoverAllMCPAdapters(t *testing.T) {
	readFixture := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	codexMCP := model.MCPMetadata{Name: "cms-server", Transport: model.MCPTransportStdio, Command: &model.MCPCommand{Command: "npx", Args: []string{"-y", "@example/cms-mcp"}}}
	codexHTTP := model.MCPMetadata{Name: "cms-http", Transport: model.MCPTransportHTTP, Remote: &model.MCPRemote{URL: "https://cms.example/mcp", Headers: map[string]string{"X-Workspace": "${WORKSPACE}"}, Auth: model.MCPAuth{Mode: "bearer-env", Env: "CMS_TOKEN"}}}
	codexPlan, err := (Codex{}).PlanMCPMutation(readFixture("codex-config.toml"), []model.ResolvedMCP{{Metadata: codexMCP, Ref: model.MCPRef{ID: "cms-server@1"}}, {Metadata: codexHTTP, Ref: model.MCPRef{ID: "cms-http@1", Tools: model.MCPToolFilter{Allow: []string{"search"}, Deny: []string{"delete"}}}}}, nil, model.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"model = \"gpt-5\"", "[mcp_servers.manual-server]", "[mcp_servers.cms-server]", "bearer_token_env_var", "http_headers", "enabled_tools", "disabled_tools"} {
		if !strings.Contains(string(codexPlan.Data), want) {
			t.Fatalf("Codex fixture lost %q: %s", want, codexPlan.Data)
		}
	}

	claudeMCP := model.MCPMetadata{Name: "cms-server", Transport: model.MCPTransportStdio, Command: &model.MCPCommand{Command: "npx", Args: []string{"-y", "@example/cms-mcp"}}}
	claudePlan, err := (Claude{}).PlanMCPMutation(readFixture("claude-config.jsonc"), []model.ResolvedMCP{{Metadata: claudeMCP, Ref: model.MCPRef{ID: "claude@1", Tools: model.MCPToolFilter{Allow: []string{"search"}}}}}, nil, model.ScopeProject)
	if err != nil || len(claudePlan.Warnings) != 1 {
		t.Fatalf("Claude plan=%#v err=%v", claudePlan, err)
	}
	if !strings.Contains(string(claudePlan.Data), "Project settings are user-owned") || !strings.Contains(string(claudePlan.Data), "manual-server") {
		t.Fatalf("Claude fixture was not preserved: %s", claudePlan.Data)
	}

	antigravityMCP := model.MCPMetadata{Name: "cms-server", Transport: model.MCPTransportHTTP, Remote: &model.MCPRemote{URL: "https://cms.example/mcp"}}
	antigravityPlan, err := (Antigravity{}).PlanMCPMutation(readFixture("antigravity-config.jsonc"), []model.ResolvedMCP{{Metadata: antigravityMCP, Ref: model.MCPRef{ID: "antigravity@1", Tools: model.MCPToolFilter{Allow: []string{"search"}, Deny: []string{"delete_record"}}}}}, nil, model.ScopeProject)
	if err != nil || len(antigravityPlan.Warnings) != 1 || !strings.Contains(string(antigravityPlan.Data), "serverUrl") || !strings.Contains(string(antigravityPlan.Data), "disabledTools") {
		t.Fatalf("Antigravity plan=%#v err=%v data=%s", antigravityPlan, err, antigravityPlan.Data)
	}

	cursorMCP := model.MCPMetadata{Name: "cms-server", Transport: model.MCPTransportHTTP, Remote: &model.MCPRemote{URL: "https://cms.example/mcp", Headers: map[string]string{"Authorization": "Bearer ${CMS_TOKEN}"}}}
	cursorPlan, err := (Cursor{}).PlanMCPMutation(readFixture("cursor-config.jsonc"), []model.ResolvedMCP{{Metadata: cursorMCP, Ref: model.MCPRef{ID: "cursor@1", Tools: model.MCPToolFilter{Deny: []string{"delete"}}}}}, nil, model.ScopeProject)
	if err != nil || len(cursorPlan.Warnings) != 1 || !strings.Contains(string(cursorPlan.Data), "${env:CMS_TOKEN}") || !strings.Contains(string(cursorPlan.Data), "formatOnSave") {
		t.Fatalf("Cursor plan=%#v err=%v data=%s", cursorPlan, err, cursorPlan.Data)
	}

	openCodeMCP := model.MCPMetadata{Name: "cms-server", Transport: model.MCPTransportStdio, Command: &model.MCPCommand{Command: "npx", Args: []string{"-y", "@example/cms-mcp"}, Env: map[string]string{"CMS_TOKEN": "${CMS_TOKEN}"}}}
	openCodePlan, err := (OpenCode{}).PlanMCPMutation(readFixture("opencode-config.jsonc"), []model.ResolvedMCP{{Metadata: openCodeMCP, Ref: model.MCPRef{ID: "opencode@1", Tools: model.MCPToolFilter{Deny: []string{"delete"}}}}}, nil, model.ScopeProject)
	if err != nil || !strings.Contains(string(openCodePlan.Data), `"environment"`) || !strings.Contains(string(openCodePlan.Data), "{env:CMS_TOKEN}") || !strings.Contains(string(openCodePlan.Data), `"cms-server_delete": false`) || !strings.Contains(string(openCodePlan.Data), "manual_tool") {
		t.Fatalf("OpenCode plan=%#v err=%v data=%s", openCodePlan, err, openCodePlan.Data)
	}
}
