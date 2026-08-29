package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ikts/cms/internal/harness"
	"github.com/ikts/cms/internal/linker"
	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/storage"
)

const globalContextName = "global"

// globalInit exposes the reserved global context through the user's home
// directory. Its links are intentionally reconciled separately from a
// project, so running cms init in a repository can never touch them.
func (a *App) globalInit(args []string) error {
	root, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	targetNames, skillIDs, dry, err := parseGlobalInitOptions(args)
	if err != nil {
		return err
	}
	if a.Config.LinkMode != "symlink" {
		return fail(2, fmt.Errorf("unsupported linking mode %q; the MVP supports symlink", a.Config.LinkMode))
	}

	global, created, err := a.globalContext(skillIDs)
	if err != nil {
		return fail(3, err)
	}
	if len(targetNames) == 0 {
		targetNames = a.Config.DefaultTargets
	}
	adapters := make([]harness.Adapter, 0, len(targetNames))
	for _, name := range targetNames {
		adapter, ok := a.Harnesses[name]
		if !ok {
			return fail(2, fmt.Errorf("unknown harness target %q", name))
		}
		adapters = append(adapters, adapter)
	}
	state, err := storage.LoadState(root)
	if err != nil {
		return err
	}
	plan, err := linker.BuildPlan(root, state, global, adapters, a.Registry)
	if err != nil {
		return fail(3, err)
	}
	for _, action := range plan.Actions {
		if action.Kind == linker.Conflict {
			fmt.Fprintf(a.Err, "CONFLICT %s: %s\n", action.Target, action.Reason)
		}
	}
	if len(plan.Conflicts()) > 0 {
		return fail(4, errors.New("link conflicts found; no files were changed"))
	}
	if a.JSON {
		if err := jsonEncode(a.Out, plan.Actions); err != nil {
			return err
		}
	} else if !a.Quiet {
		if created {
			if dry {
				fmt.Fprintf(a.Out, "would create global context with %d skill(s)\n", len(global.Skills))
			} else {
				fmt.Fprintf(a.Out, "created global context with %d skill(s)\n", len(global.Skills))
			}
		}
		for _, action := range plan.Actions {
			fmt.Fprintf(a.Out, "%-8s %s\n", action.Kind, action.Target)
		}
	}
	if dry {
		return nil
	}
	if created {
		if err := a.Contexts.Save(global); err != nil {
			return fail(3, err)
		}
	}
	if err := linker.Apply(root, plan, a.Registry); err != nil {
		if created {
			_ = os.Remove(a.Contexts.Path(globalContextName))
		}
		return fail(4, err)
	}
	if a.JSON || a.Quiet {
		return nil
	}
	changed := false
	for _, action := range plan.Actions {
		if action.Kind == linker.Create || action.Kind == linker.Remove {
			changed = true
			break
		}
	}
	if changed {
		fmt.Fprintln(a.Out, "Applied global context. An already-open harness may need to be restarted to rediscover skills.")
	} else {
		fmt.Fprintln(a.Out, "Global context is up to date.")
	}
	return nil
}

func (a *App) globalContext(skillIDs []string) (model.Context, bool, error) {
	path := a.Contexts.Path(globalContextName)
	if _, err := os.Stat(path); err == nil {
		if len(skillIDs) > 0 {
			return model.Context{}, false, errors.New("global context already exists; use context-edit global to change its skills")
		}
		global, getErr := a.Contexts.Get(globalContextName)
		return global, false, getErr
	} else if !os.IsNotExist(err) {
		return model.Context{}, false, err
	}

	if len(skillIDs) == 0 {
		return model.Context{}, false, errors.New("global context was not found; create it with cms global-init --skill <id>")
	}
	global := model.Context{
		SchemaVersion: 1,
		Name:          globalContextName,
		Description:   "Skills available globally",
		MCPs:          []string{},
	}
	for _, id := range skillIDs {
		global.Skills = append(global.Skills, model.SkillRef{ID: id})
	}
	return global, true, nil
}

func parseInitOptions(args []string) ([]string, bool, error) {
	var targets []string
	dry := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			dry = true
		case "--target":
			if i+1 >= len(args) {
				return nil, false, errors.New("--target requires a value")
			}
			targets = append(targets, args[i+1])
			i++
		default:
			return nil, false, fmt.Errorf("unknown init option %q", args[i])
		}
	}
	return targets, dry, nil
}

func parseGlobalInitOptions(args []string) ([]string, []string, bool, error) {
	var targets, skillIDs []string
	dry := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			dry = true
		case "--target":
			if i+1 >= len(args) {
				return nil, nil, false, errors.New("--target requires a value")
			}
			targets = append(targets, args[i+1])
			i++
		case "--skill":
			if i+1 >= len(args) {
				return nil, nil, false, errors.New("--skill requires a value")
			}
			skillIDs = append(skillIDs, args[i+1])
			i++
		default:
			return nil, nil, false, fmt.Errorf("unknown global-init option %q", args[i])
		}
	}
	return targets, skillIDs, dry, nil
}

func (a *App) globalRemove(args []string) error {
	var yes, dry bool
	for _, arg := range args {
		switch arg {
		case "--yes", "-y":
			yes = true
		case "--dry-run":
			dry = true
		default:
			return fail(2, fmt.Errorf("unknown global-remove option %q", arg))
		}
	}
	if a.JSON && !yes && !dry {
		return fail(2, errors.New("global-remove --json requires --yes"))
	}
	root, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	_, contextErr := os.Stat(a.Contexts.Path(globalContextName))
	contextExists := contextErr == nil
	if contextErr != nil && !os.IsNotExist(contextErr) {
		return contextErr
	}
	state, err := storage.LoadState(root)
	if err != nil {
		return err
	}
	if !contextExists && state.Context != globalContextName && len(state.Links) == 0 {
		if a.JSON {
			return jsonEncode(a.Out, []linker.Action{})
		}
		if !a.Quiet {
			fmt.Fprintln(a.Out, "global context is not initialized")
		}
		return nil
	}
	plan, err := linker.BuildPlan(root, state, model.Context{}, nil, a.Registry)
	if err != nil {
		return fail(4, err)
	}
	if !dry && !yes {
		fmt.Fprintf(a.Out, "Remove global context and %d CMS-managed link(s)? [y/N]: ", len(plan.Actions))
		answer, readErr := bufio.NewReader(a.In).ReadString('\n')
		if readErr != nil && len(answer) == 0 {
			return fail(2, errors.New("removal cancelled; use --yes for non-interactive execution"))
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			if !a.Quiet && !a.JSON {
				fmt.Fprintln(a.Out, "Removal cancelled.")
			}
			return nil
		}
	}
	if a.JSON {
		if err := jsonEncode(a.Out, plan.Actions); err != nil {
			return err
		}
	} else if !a.Quiet {
		for _, action := range plan.Actions {
			fmt.Fprintf(a.Out, "%-8s %s\n", action.Kind, action.Target)
		}
		if dry {
			fmt.Fprintln(a.Out, "would remove the global context")
		}
	}
	if dry {
		return nil
	}
	if state.Context != "" || len(state.Targets) > 0 || len(state.Links) > 0 {
		if err := linker.Apply(root, plan, a.Registry); err != nil {
			return fail(4, err)
		}
	}
	if contextExists {
		if err := os.Remove(a.Contexts.Path(globalContextName)); err != nil && !os.IsNotExist(err) {
			return fail(3, err)
		}
	}
	if a.JSON || a.Quiet {
		return nil
	}
	fmt.Fprintln(a.Out, "Removed global context and its CMS-managed links.")
	return nil
}
