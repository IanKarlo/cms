package cli

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeCommand(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "skill list", args: []string{"skill", "list", "--plain"}, want: []string{"skill-list", "--plain"}},
		{name: "mcp import", args: []string{"mcp", "import", "config.json", "--target", "cursor"}, want: []string{"mcp-import", "config.json", "--target", "cursor"}},
		{name: "context edit", args: []string{"context", "edit", "backend", "--name", "api"}, want: []string{"context-edit", "backend", "--name", "api"}},
		{name: "global init", args: []string{"global", "init", "--target", "codex"}, want: []string{"global-init", "--target", "codex"}},
		{name: "project clear", args: []string{"project", "clear", "--yes"}, want: []string{"clear", "--yes"}},
		{name: "shell completion", args: []string{"shell", "completion", "bash"}, want: []string{"completion", "bash"}},
		{name: "config remains nested", args: []string{"config", "show"}, want: []string{"config", "show"}},
		{name: "legacy alias", args: []string{"context-list", "--plain"}, want: []string{"context-list", "--plain"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeCommand(tt.args)
			if err != nil {
				t.Fatalf("normalizeCommand() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("normalizeCommand() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestNormalizeCommandRejectsMissingOrUnknownAction(t *testing.T) {
	for _, args := range [][]string{{"mcp"}, {"context", "publish"}} {
		got, err := normalizeCommand(args)
		if err == nil {
			t.Fatalf("normalizeCommand(%#v) error = nil, want an argument error", args)
		}
		if got != nil || codeOf(err) != 2 {
			t.Fatalf("normalizeCommand(%#v) = %#v, error code %d; want nil and code 2", args, got, codeOf(err))
		}
	}
}

func TestPrintModuleHelp(t *testing.T) {
	for _, module := range []string{"skill", "mcp", "context", "global", "project", "config", "shell"} {
		t.Run(module, func(t *testing.T) {
			var out bytes.Buffer
			if !printModuleHelp(&out, module) {
				t.Fatalf("printModuleHelp(%q) = false", module)
			}
			if !strings.Contains(out.String(), "Available actions:") || !strings.Contains(out.String(), "cms "+module) {
				t.Fatalf("module help for %q is incomplete: %s", module, out.String())
			}
		})
	}

	var out bytes.Buffer
	if printModuleHelp(&out, "unknown") {
		t.Fatal("printModuleHelp(unknown) = true")
	}
}
