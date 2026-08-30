package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ikts/cms/internal/storage"
)

// defaultTargetNames returns the configured harnesses or, when the setting is
// absent/empty, every harness known by this CMS version.
func (a *App) defaultTargetNames() []string {
	if len(a.Config.DefaultTargets) > 0 {
		return append([]string(nil), a.Config.DefaultTargets...)
	}
	targets := make([]string, 0, len(a.Harnesses))
	for name := range a.Harnesses {
		targets = append(targets, name)
	}
	sort.Strings(targets)
	return targets
}

func (a *App) config(args []string) error {
	if len(args) == 0 {
		return a.configShow()
	}
	command := strings.ToLower(args[0])
	switch command {
	case "show", "list", "get":
		if command == "get" && (len(args) != 2 || normalizeConfigKey(args[1]) != "default_targets") {
			return fail(2, errors.New("usage: cms config get default-targets"))
		}
		if command != "get" && len(args) > 1 {
			return fail(2, errors.New("usage: cms config [show|list]"))
		}
		return a.configShow()
	case "set":
		return a.configSet(args[1:])
	case "unset", "remove", "reset":
		return a.configUnset(args[1:])
	default:
		return fail(2, errors.New("usage: cms config [show|list|get|set|unset]"))
	}
}

func (a *App) configShow() error {
	configured := append([]string(nil), a.Config.DefaultTargets...)
	effective := a.defaultTargetNames()
	if a.JSON {
		return jsonEncode(a.Out, struct {
			ConfigPath       string   `json:"config_path"`
			DefaultTargets   []string `json:"default_targets,omitempty"`
			EffectiveTargets []string `json:"effective_targets"`
		}{a.Paths.ConfigFile(), configured, effective})
	}
	if len(configured) == 0 {
		fmt.Fprintln(a.Out, "default-targets: all")
	} else {
		fmt.Fprintf(a.Out, "default-targets: %s\n", strings.Join(configured, ", "))
	}
	fmt.Fprintf(a.Out, "effective-targets: %s\n", strings.Join(effective, ", "))
	fmt.Fprintf(a.Out, "config: %s\n", a.Paths.ConfigFile())
	return nil
}

func (a *App) configSet(args []string) error {
	if len(args) < 2 || normalizeConfigKey(args[0]) != "default_targets" {
		return fail(2, errors.New("usage: cms config set default-targets <harness>[,<harness> ...]"))
	}
	targets, err := parseTargetValues(args[1:])
	if err != nil {
		return fail(2, err)
	}
	if len(targets) == 0 {
		return fail(2, errors.New("at least one harness is required; use cms config unset default-targets to use all"))
	}
	for _, name := range targets {
		if _, ok := a.Harnesses[name]; !ok {
			return fail(2, fmt.Errorf("unknown harness target %q", name))
		}
	}
	if err := storage.SaveDefaultTargets(a.Paths, targets); err != nil {
		return fail(3, err)
	}
	a.Config.DefaultTargets = targets
	if a.JSON {
		return jsonEncode(a.Out, map[string]any{"default_targets": targets, "config_path": a.Paths.ConfigFile()})
	}
	fmt.Fprintf(a.Out, "default harnesses saved: %s\n", strings.Join(targets, ", "))
	return nil
}

func (a *App) configUnset(args []string) error {
	if len(args) != 1 || normalizeConfigKey(args[0]) != "default_targets" {
		return fail(2, errors.New("usage: cms config unset default-targets"))
	}
	if err := storage.SaveDefaultTargets(a.Paths, nil); err != nil {
		return fail(3, err)
	}
	a.Config.DefaultTargets = nil
	if a.JSON {
		return jsonEncode(a.Out, map[string]any{"default_targets": nil, "effective_targets": a.defaultTargetNames(), "config_path": a.Paths.ConfigFile()})
	}
	fmt.Fprintln(a.Out, "default harnesses cleared; all harnesses will be considered")
	return nil
}

func normalizeConfigKey(key string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(key)), "-", "_")
}

func parseTargetValues(values []string) ([]string, error) {
	seen := map[string]bool{}
	var targets []string
	for _, value := range values {
		for _, raw := range strings.Split(value, ",") {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			if !seen[name] {
				seen[name] = true
				targets = append(targets, name)
			}
		}
	}
	return targets, nil
}
