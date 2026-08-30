package cli

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestModuleActionArgs(t *testing.T) {
	tests := []struct {
		name   string
		module string
		args   []string
		action string
		want   []string
	}{
		{name: "skill list", module: "skill", args: []string{"list", "--plain"}, action: "list", want: []string{"--plain"}},
		{name: "mcp import", module: "mcp", args: []string{"import", "config.json", "--target", "cursor"}, action: "import", want: []string{"config.json", "--target", "cursor"}},
		{name: "context edit", module: "context", args: []string{"edit", "backend", "--name", "api"}, action: "edit", want: []string{"backend", "--name", "api"}},
		{name: "global init", module: "global", args: []string{"init", "--target", "codex"}, action: "init", want: []string{"--target", "codex"}},
		{name: "project clear", module: "project", args: []string{"clear", "--yes"}, action: "clear", want: []string{"--yes"}},
		{name: "shell completion", module: "shell", args: []string{"completion", "bash"}, action: "completion", want: []string{"bash"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, got, err := moduleActionArgs(tt.module, tt.args)
			if err != nil {
				t.Fatalf("moduleActionArgs() error = %v", err)
			}
			if name != tt.action || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("moduleActionArgs() = %q, %#v; want %q, %#v", name, got, tt.action, tt.want)
			}
		})
	}
}

func TestModuleActionArgsRejectsMissingOrUnknownAction(t *testing.T) {
	for _, test := range []struct {
		module string
		args   []string
	}{{module: "mcp", args: nil}, {module: "context", args: []string{"publish"}}} {
		name, got, err := moduleActionArgs(test.module, test.args)
		if err == nil {
			t.Fatalf("moduleActionArgs(%q, %#v) error = nil, want an argument error", test.module, test.args)
		}
		if name != "" || got != nil || codeOf(err) != 2 {
			t.Fatalf("moduleActionArgs(%q, %#v) = %q, %#v, error code %d; want empty, nil, and code 2", test.module, test.args, name, got, codeOf(err))
		}
	}
}

func TestRunRejectsUnsupportedCommandForms(t *testing.T) {
	for _, command := range []string{"skill-list", "mcp-list", "context-edit", "global-init", "init", "freeze", "sync", "clear", "completion"} {
		err := (&App{}).run([]string{command})
		if err == nil || codeOf(err) != 2 {
			t.Fatalf("run(%q) error = %v, want an argument error", command, err)
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
