package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ikts/cms/internal/harness"
	"github.com/ikts/cms/internal/linker"
	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/storage"
)

type clearReport struct {
	ManifestRemoved bool     `json:"manifest_removed"`
	StateRemoved    bool     `json:"state_removed"`
	SkillLinks      int      `json:"skill_links"`
	MCPEntries      int      `json:"mcp_entries"`
	Targets         []string `json:"targets"`
	DryRun          bool     `json:"dry_run"`
}

type clearFile struct {
	path   string
	data   []byte
	mode   os.FileMode
	exists bool
}

func (a *App) clear(args []string) error {
	yes, dry := false, false
	for _, arg := range args {
		switch arg {
		case "--yes", "-y":
			yes = true
		case "--dry-run":
			dry = true
		default:
			return fail(2, fmt.Errorf("unknown clear option %q", arg))
		}
	}
	if a.JSON && !yes && !dry {
		return fail(2, errors.New("clear --json requires --yes or --dry-run"))
	}

	root, err := os.Getwd()
	if err != nil {
		return err
	}
	manifest, manifestExists, err := clearManifest(root)
	if err != nil {
		return fail(3, err)
	}
	state, err := storage.LoadState(root)
	if err != nil {
		return fail(3, err)
	}
	stateFile, err := readClearFile(storage.StatePath(root))
	if err != nil {
		return fail(3, err)
	}
	manifestFile, err := readClearFile(storage.ManifestPath(root))
	if err != nil {
		return fail(3, err)
	}

	targetNames := clearTargets(a, state, manifest, manifestExists)
	adapters, err := clearAdapters(a.Harnesses, targetNames)
	if err != nil {
		return fail(2, err)
	}

	plan, err := linker.BuildPlan(root, state, model.Context{}, adapters, a.Registry)
	if err != nil {
		return fail(4, err)
	}
	if conflicts := plan.Conflicts(); len(conflicts) > 0 {
		for _, conflict := range conflicts {
			fmt.Fprintf(a.Err, "CONFLICT %s: %s\n", conflict.Target, conflict.Reason)
		}
		return fail(4, errors.New("cannot clear while managed skill conflicts exist"))
	}
	mcpPlans, err := a.buildMCPPlans(root, state, model.Context{}, adapters, model.ScopeProject)
	if err != nil {
		return fail(4, err)
	}
	var mcpActions []harness.MCPAction
	for _, item := range mcpPlans {
		if conflicts := item.plan.Conflicts(); len(conflicts) > 0 {
			for _, conflict := range conflicts {
				fmt.Fprintf(a.Err, "CONFLICT %s: %s\n", conflict.Name, conflict.Reason)
			}
			return fail(4, errors.New("cannot clear while managed MCP conflicts exist"))
		}
		mcpActions = append(mcpActions, item.plan.Actions...)
	}

	report := clearReport{
		ManifestRemoved: manifestFile.exists,
		StateRemoved:    stateFile.exists,
		SkillLinks:      countClearSkillLinks(plan),
		MCPEntries:      countClearMCPEntries(mcpActions),
		Targets:         append([]string(nil), targetNames...),
		DryRun:          dry,
	}
	if !report.ManifestRemoved && !report.StateRemoved && report.SkillLinks == 0 && report.MCPEntries == 0 {
		if a.JSON {
			return jsonEncode(a.Out, report)
		}
		if !a.Quiet {
			fmt.Fprintln(a.Out, "nothing to clear")
		}
		return nil
	}

	if !dry && !yes {
		printClearWarning(a.Out, report)
		answer, readErr := bufio.NewReader(a.In).ReadString('\n')
		if readErr != nil && len(answer) == 0 {
			return fail(2, errors.New("clear cancelled; use --yes for non-interactive execution"))
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			if !a.Quiet && !a.JSON {
				fmt.Fprintln(a.Out, "Clear cancelled.")
			}
			return nil
		}
	}

	if dry {
		if a.JSON {
			return jsonEncode(a.Out, report)
		}
		if !a.Quiet {
			printClearReport(a.Out, report, true)
		}
		return nil
	}

	var rollbackLinks func()
	if len(state.Links) > 0 {
		rollbackLinks, err = linker.ApplyWithRollback(root, plan, a.Registry)
		if err != nil {
			return fail(4, err)
		}
	}
	rollbackMCP, err := applyMCPPlans(mcpPlans)
	if err != nil {
		if rollbackLinks != nil {
			rollbackLinks()
		}
		return fail(4, err)
	}

	if err := removeClearFile(manifestFile); err != nil {
		rollbackMCP()
		if rollbackLinks != nil {
			rollbackLinks()
		}
		return fail(3, err)
	}
	if err := removeClearFile(stateFile); err != nil {
		_ = restoreClearFile(manifestFile)
		rollbackMCP()
		if rollbackLinks != nil {
			rollbackLinks()
		}
		return fail(3, err)
	}
	clearEmptyDirectories(root, targetNames, a.Harnesses)

	if a.JSON {
		return jsonEncode(a.Out, report)
	}
	if !a.Quiet {
		printClearReport(a.Out, report, false)
	}
	return nil
}

func clearManifest(root string) (model.ProjectManifest, bool, error) {
	manifest, err := storage.LoadManifest(root)
	if errors.Is(err, os.ErrNotExist) {
		return model.ProjectManifest{}, false, nil
	}
	if err != nil {
		return model.ProjectManifest{}, true, fmt.Errorf("cannot read cms.toml: %w", err)
	}
	return manifest, true, nil
}

func clearTargets(a *App, state model.ProjectState, manifest model.ProjectManifest, manifestExists bool) []string {
	if len(state.Targets) > 0 {
		return uniqueStrings(state.Targets)
	}
	if manifestExists && len(manifest.Targets) > 0 {
		return uniqueStrings(manifest.Targets)
	}
	if len(state.Links) > 0 || len(state.MCPEntries) > 0 {
		return a.defaultTargetNames()
	}
	return nil
}

func clearAdapters(all map[string]harness.Adapter, names []string) ([]harness.Adapter, error) {
	adapters := make([]harness.Adapter, 0, len(names))
	for _, name := range names {
		adapter, ok := all[name]
		if !ok {
			return nil, fmt.Errorf("unknown harness target %q in CMS state or manifest", name)
		}
		adapters = append(adapters, adapter)
	}
	return adapters, nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func readClearFile(path string) (clearFile, error) {
	file := clearFile{path: path, mode: 0o644}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return file, nil
	}
	if err != nil {
		return file, err
	}
	file.data, file.exists = data, true
	if info, statErr := os.Stat(path); statErr == nil {
		file.mode = info.Mode().Perm()
	}
	return file, nil
}

func removeClearFile(file clearFile) error {
	if !file.exists {
		return nil
	}
	if err := os.Remove(file.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func restoreClearFile(file clearFile) error {
	if !file.exists {
		return nil
	}
	return storage.AtomicWrite(file.path, file.data, file.mode)
}

func countClearSkillLinks(plan linker.Plan) int {
	count := 0
	for _, action := range plan.Actions {
		if action.Kind == linker.Remove {
			count++
		}
	}
	return count
}

func countClearMCPEntries(actions []harness.MCPAction) int {
	count := 0
	for _, action := range actions {
		if action.Kind == harness.MCPRemove {
			count++
		}
	}
	return count
}

func printClearWarning(out io.Writer, report clearReport) {
	fmt.Fprintln(out, "WARNING: cms clear removes project configuration managed by CMS.")
	fmt.Fprintln(out, "It may remove CMS-managed links and MCP entries from the listed harness targets.")
	fmt.Fprintln(out, "Harness files and directories created outside CMS are preserved; only empty directories may be removed.")
	fmt.Fprintln(out, "This removes cms.toml and .cms/state.toml and cannot be undone automatically.")
	fmt.Fprint(out, "Proceed with cms clear? [y/N]: ")
}

func printClearReport(out io.Writer, report clearReport, dry bool) {
	prefix := "Cleared"
	if dry {
		prefix = "Would clear"
	}
	fmt.Fprintf(out, "%s %d skill link(s) and %d MCP configuration item(s).\n", prefix, report.SkillLinks, report.MCPEntries)
	if report.ManifestRemoved {
		fmt.Fprintln(out, "  cms.toml")
	}
	if report.StateRemoved {
		fmt.Fprintln(out, "  .cms/state.toml")
	}
	if len(report.Targets) > 0 {
		fmt.Fprintf(out, "  targets: %s\n", strings.Join(report.Targets, ", "))
	}
}

func clearEmptyDirectories(root string, names []string, all map[string]harness.Adapter) {
	seen := map[string]bool{}
	for _, name := range names {
		adapter, ok := all[name]
		if !ok {
			continue
		}
		dir := filepath.Clean(adapter.SkillDir(root))
		if seen[dir] {
			continue
		}
		seen[dir] = true
		removeEmptyClearDirectory(root, dir)
		parent := filepath.Dir(dir)
		if filepath.Clean(parent) != filepath.Clean(root) {
			removeEmptyClearDirectory(root, parent)
		}
	}
	removeEmptyClearDirectory(root, filepath.Dir(storage.StatePath(root)))
}

func removeEmptyClearDirectory(root, path string) {
	path = filepath.Clean(path)
	rel, err := filepath.Rel(filepath.Clean(root), path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) || err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	_ = os.Remove(path)
}
