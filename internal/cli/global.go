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
	targetNames, skillIDs, mcpIDs, dry, err := parseGlobalInitOptions(args)
	if err != nil {
		return err
	}
	if a.Config.LinkMode != "symlink" {
		return fail(2, fmt.Errorf("unsupported linking mode %q; the MVP supports symlink", a.Config.LinkMode))
	}

	global, created, err := a.globalContext(skillIDs, mcpIDs)
	if err != nil {
		return fail(3, err)
	}
	if len(targetNames) == 0 {
		targetNames = a.defaultTargetNames()
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
	mcpPlans, err := a.buildMCPPlans(root, state, global, adapters, model.ScopeGlobal)
	if err != nil {
		return err
	}
	var mcpActions []harness.MCPAction
	var mcpWarnings []harness.MCPWarning
	var mcpEntries []model.MCPStateEntry
	for _, item := range mcpPlans {
		mcpActions = append(mcpActions, item.plan.Actions...)
		mcpWarnings = append(mcpWarnings, item.plan.Warnings...)
		mcpEntries = append(mcpEntries, item.plan.Entries...)
	}
	plan, err := linker.BuildPlan(root, state, global, adapters, a.Registry, true)
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
	for _, action := range mcpActions {
		if action.Kind == harness.MCPConflict {
			fmt.Fprintf(a.Err, "CONFLICT %s: %s\n", action.Name, action.Reason)
		}
	}
	for _, w := range mcpWarnings {
		if !a.JSON {
			fmt.Fprintf(a.Err, "warning: %s\n", w.Message)
		}
	}
	for _, item := range mcpPlans {
		if len(item.plan.Conflicts()) > 0 {
			return fail(4, errors.New("MCP config conflicts found; no files were changed"))
		}
	}
	if a.JSON {
		if len(mcpActions) > 0 {
			return jsonEncode(a.Out, struct {
				Skills   any                  `json:"skills"`
				MCP      []harness.MCPAction  `json:"mcp"`
				Warnings []harness.MCPWarning `json:"warnings"`
			}{plan.Actions, mcpActions, mcpWarnings})
		}
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
	rollbackMCP, mcpErr := applyMCPPlans(mcpPlans)
	if mcpErr != nil {
		if created {
			_ = os.Remove(a.Contexts.Path(globalContextName))
		}
		return fail(4, mcpErr)
	}
	rollbackLinks, applyErr := linker.ApplyWithRollback(root, plan, a.Registry)
	if applyErr != nil {
		if rollbackMCP != nil {
			rollbackMCP()
		}
		if created {
			_ = os.Remove(a.Contexts.Path(globalContextName))
		}
		return fail(4, applyErr)
	}
	finalState, loadErr := storage.LoadState(root)
	if loadErr != nil {
		if rollbackLinks != nil {
			rollbackLinks()
		}
		if rollbackMCP != nil {
			rollbackMCP()
		}
		if created {
			_ = os.Remove(a.Contexts.Path(globalContextName))
		}
		return loadErr
	}
	finalState.SchemaVersion = 2
	finalState.MCPEntries = mcpEntries
	if saveErr := storage.SaveState(root, finalState); saveErr != nil {
		if rollbackLinks != nil {
			rollbackLinks()
		}
		if rollbackMCP != nil {
			rollbackMCP()
		}
		if created {
			_ = os.Remove(a.Contexts.Path(globalContextName))
		}
		return saveErr
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

func (a *App) globalContext(skillIDs, mcpIDs []string) (model.Context, bool, error) {
	path := a.Contexts.Path(globalContextName)
	if _, err := os.Stat(path); err == nil {
		if len(skillIDs) > 0 || len(mcpIDs) > 0 {
			return model.Context{}, false, errors.New("global context already exists; use context edit global to change its skills")
		}
		global, getErr := a.Contexts.Get(globalContextName)
		return global, false, getErr
	} else if !os.IsNotExist(err) {
		return model.Context{}, false, err
	}

	if len(skillIDs) == 0 && len(mcpIDs) == 0 {
		return model.Context{}, false, errors.New("global context was not found; create it with cms global init --skill or --mcp <id>")
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
	for _, id := range mcpIDs {
		global.MCPRefs = append(global.MCPRefs, model.MCPRef{ID: id})
		global.MCPs = append(global.MCPs, id)
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

func parseGlobalInitOptions(args []string) ([]string, []string, []string, bool, error) {
	var targets, skillIDs, mcpIDs []string
	dry := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			dry = true
		case "--target":
			if i+1 >= len(args) {
				return nil, nil, nil, false, errors.New("--target requires a value")
			}
			targets = append(targets, args[i+1])
			i++
		case "--skill":
			if i+1 >= len(args) {
				return nil, nil, nil, false, errors.New("--skill requires a value")
			}
			skillIDs = append(skillIDs, args[i+1])
			i++
		case "--mcp":
			if i+1 >= len(args) {
				return nil, nil, nil, false, errors.New("--mcp requires a value")
			}
			mcpIDs = append(mcpIDs, args[i+1])
			i++
		default:
			return nil, nil, nil, false, fmt.Errorf("unknown global init option %q", args[i])
		}
	}
	return targets, skillIDs, mcpIDs, dry, nil
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
			return fail(2, fmt.Errorf("unknown global remove option %q", arg))
		}
	}
	if a.JSON && !yes && !dry {
		return fail(2, errors.New("global remove --json requires --yes"))
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
	var globalAdapters []harness.Adapter
	targets := state.Targets
	if len(targets) == 0 {
		targets = a.defaultTargetNames()
	}
	for _, name := range targets {
		if ad, ok := a.Harnesses[name]; ok {
			globalAdapters = append(globalAdapters, ad)
		}
	}
	mcpPlans, planErr := a.buildMCPPlans(root, state, model.Context{}, globalAdapters, model.ScopeGlobal)
	if planErr != nil {
		return planErr
	}
	for _, item := range mcpPlans {
		for _, action := range item.plan.Actions {
			if action.Kind == harness.MCPConflict {
				fmt.Fprintf(a.Err, "CONFLICT %s: %s\n", action.Name, action.Reason)
			}
		}
		if len(item.plan.Conflicts()) > 0 {
			return fail(4, errors.New("MCP config conflicts found; no files were changed"))
		}
	}
	if !contextExists && state.Context != globalContextName && len(state.Links) == 0 && len(state.MCPEntries) == 0 {
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
		var mcpActions []harness.MCPAction
		for _, item := range mcpPlans {
			mcpActions = append(mcpActions, item.plan.Actions...)
		}
		if len(mcpActions) > 0 {
			if err := jsonEncode(a.Out, struct {
				Skills []linker.Action     `json:"skills"`
				MCP    []harness.MCPAction `json:"mcp"`
			}{plan.Actions, mcpActions}); err != nil {
				return err
			}
		} else if err := jsonEncode(a.Out, plan.Actions); err != nil {
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
	var rollbackLinks func()
	if state.Context != "" || len(state.Targets) > 0 || len(state.Links) > 0 {
		var applyErr error
		rollbackLinks, applyErr = linker.ApplyWithRollback(root, plan, a.Registry)
		if applyErr != nil {
			return fail(4, applyErr)
		}
	}
	rollbackMCP, mcpErr := applyMCPPlans(mcpPlans)
	if mcpErr != nil {
		if rollbackLinks != nil {
			rollbackLinks()
		}
		return fail(4, mcpErr)
	}
	finalState, loadErr := storage.LoadState(root)
	if loadErr != nil {
		if rollbackMCP != nil {
			rollbackMCP()
		}
		if rollbackLinks != nil {
			rollbackLinks()
		}
		return loadErr
	}
	finalState.MCPEntries = nil
	finalState.SchemaVersion = 2
	if saveErr := storage.SaveState(root, finalState); saveErr != nil {
		if rollbackMCP != nil {
			rollbackMCP()
		}
		if rollbackLinks != nil {
			rollbackLinks()
		}
		return saveErr
	}
	if contextExists {
		if err := os.Remove(a.Contexts.Path(globalContextName)); err != nil && !os.IsNotExist(err) {
			if rollbackMCP != nil {
				rollbackMCP()
			}
			if rollbackLinks != nil {
				rollbackLinks()
			}
			return fail(3, err)
		}
	}
	if a.JSON || a.Quiet {
		return nil
	}
	fmt.Fprintln(a.Out, "Removed global context and its CMS-managed links.")
	return nil
}
