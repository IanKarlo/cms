package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ikts/cms/internal/harness"
	"github.com/ikts/cms/internal/linker"
	"github.com/ikts/cms/internal/mcps"
	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/providers"
	gh "github.com/ikts/cms/internal/providers/github"
	mcpregistry "github.com/ikts/cms/internal/providers/mcpregistry"
	"github.com/ikts/cms/internal/skills"
	"github.com/ikts/cms/internal/storage"
	"github.com/ikts/cms/internal/tui"
)

type App struct {
	Out, Err    io.Writer
	In          io.Reader
	Paths       storage.Paths
	Config      storage.Config
	Registry    skills.Registry
	MCPs        mcps.Registry
	Contexts    storage.ContextStore
	Harnesses   map[string]harness.Adapter
	Provider    providers.SkillProvider
	MCPProvider providers.MCPProvider
	JSON        bool
	Quiet       bool
	Verbose     bool
	NoColor     bool
}

func New(out, errOut io.Writer, in io.Reader) (*App, error) {
	p, err := storage.ResolvePaths()
	if err != nil {
		return nil, err
	}
	if err = storage.EnsureDirs(p); err != nil {
		return nil, err
	}
	c, err := storage.LoadConfig(p)
	if err != nil {
		return nil, err
	}
	r := skills.NewRegistry(p)
	ttl, _ := time.ParseDuration(c.GitHubCacheTTL)
	provider := gh.NewWithCache(p.CacheDir, ttl)
	if provider.Token == "" {
		dotenvToken, dotenvErr := storage.LoadDotEnvValue(".env", "GITHUB_TOKEN")
		if dotenvErr != nil {
			return nil, fmt.Errorf("cannot read .env: %w", dotenvErr)
		}
		provider.Token = dotenvToken
	}
	mcpTTL, _ := time.ParseDuration(c.MCPRegistryCacheTTL)
	return &App{Out: out, Err: errOut, In: in, Paths: p, Config: c, Registry: r, MCPs: mcps.NewRegistry(p), Contexts: storage.ContextStore{Paths: p, Registry: r.Store}, Harnesses: harness.Builtins(), Provider: provider, MCPProvider: mcpregistry.New(c.MCPRegistryBaseURL, filepath.Join(p.CacheDir, "mcp-registry"), mcpTTL)}, nil
}

func Run(args []string, out, errOut io.Writer, in io.Reader) int {
	args, global := parseGlobalFlags(args)
	if len(args) == 0 {
		usage(out)
		return 2
	}
	if len(args) == 1 && printModuleHelp(out, args[0]) {
		return 0
	}
	app, err := New(out, errOut, in)
	if err != nil {
		fmt.Fprintln(errOut, "error:", err)
		return 1
	}
	app.JSON, app.Quiet, app.Verbose, app.NoColor = global.json, global.quiet, global.verbose, global.noColor
	err = app.run(args)
	if err != nil {
		if app.JSON {
			payload := map[string]any{"error": err.Error(), "code": codeOf(err)}
			var pre MCPPreflightError
			if errors.As(err, &pre) {
				payload["code"] = pre.Code
				payload["mcp"] = pre.MCP
				if pre.Variable != "" {
					payload["variable"] = pre.Variable
				}
				if pre.Runtime != "" {
					payload["runtime"] = pre.Runtime
				}
			}
			var variants MCPVariantSelectionError
			if errors.As(err, &variants) {
				payload["code"] = "mcp_variant_required"
				payload["variants"] = variants.Variants
				if variants.Requested != "" {
					payload["requested_variant"] = variants.Requested
				}
			}
			_ = json.NewEncoder(errOut).Encode(payload)
			return codeOf(err)
		}
		fmt.Fprintln(errOut, "error:", err)
		return codeOf(err)
	}
	return 0
}

type moduleAction struct {
	name        string
	usage       string
	description string
}

var moduleOrder = []string{"skill", "mcp", "context", "global", "project", "config", "shell"}

var moduleActionsHelp = map[string][]moduleAction{
	"skill": {
		{name: "install", usage: "install [github-url]", description: "installs a skill from GitHub or interactive search"},
		{name: "list", usage: "list [--plain|--json]", description: "lists installed skills"},
		{name: "remove", usage: "remove <id>... [--yes]", description: "removes installed skills"},
	},
	"mcp": {
		{name: "install", usage: "install [source] [options]", description: "registers an MCP from a package, URL, or command"},
		{name: "list", usage: "list [--plain|--json]", description: "lists registered MCPs"},
		{name: "remove", usage: "remove <id>...", description: "removes registered MCPs that are not in use"},
		{name: "import", usage: "import <path> --target <target>", description: "imports MCPs from a harness configuration"},
	},
	"context": {
		{name: "new", usage: "new [options]", description: "creates a context of skills and MCPs"},
		{name: "edit", usage: "edit <context> [options]", description: "edits an existing context"},
		{name: "list", usage: "list [--plain|--json]", description: "lists and lets you browse contexts"},
	},
	"global": {
		{name: "init", usage: "init [options]", description: "configures the global context in harnesses"},
		{name: "remove", usage: "remove [--yes] [--dry-run]", description: "removes the global context and its managed links"},
	},
	"project": {
		{name: "init", usage: "init [context] [options]", description: "applies a context to the project's harnesses"},
		{name: "freeze", usage: "freeze <context>", description: "writes the current context to cms.toml"},
		{name: "sync", usage: "sync", description: "synchronizes dependencies defined in cms.toml"},
		{name: "clear", usage: "clear [--yes] [--dry-run]", description: "removes project configuration and managed links"},
	},
	"config": {
		{name: "show", usage: "show", description: "shows current configuration and effective targets"},
		{name: "list", usage: "list", description: "shows current configuration and effective targets"},
		{name: "get", usage: "get default-targets", description: "shows configured default targets"},
		{name: "set", usage: "set default-targets <harness>...", description: "sets default targets"},
		{name: "unset", usage: "unset default-targets", description: "removes the default target configuration"},
	},
	"shell": {
		{name: "completion", usage: "completion <bash|zsh|fish|powershell>", description: "generates a shell completion script"},
	},
}

func moduleActionArgs(module string, args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, fail(2, fmt.Errorf("usage: cms %s <action>", module))
	}
	for _, action := range moduleActionsHelp[module] {
		if args[0] == action.name {
			return action.name, args[1:], nil
		}
	}
	return "", nil, fail(2, fmt.Errorf("unknown %s action %q", module, args[0]))
}

func printModuleHelp(w io.Writer, module string) bool {
	actions, ok := moduleActionsHelp[module]
	if !ok {
		return false
	}
	fmt.Fprintf(w, "CMS — %s module\n\nAvailable actions:\n", module)
	for _, action := range actions {
		command := fmt.Sprintf("cms %s %s", module, action.usage)
		fmt.Fprintf(w, "  %-56s %s\n", command, action.description)
	}
	fmt.Fprintf(w, "\nUse `cms %s <action>` to run an action.\n", module)
	return true
}

func (a *App) run(args []string) error {
	if len(args) == 0 {
		usage(a.Out)
		return fail(2, errors.New("a command is required"))
	}
	switch args[0] {
	case "config":
		return a.config(args[1:])
	case "skill":
		action, actionArgs, err := moduleActionArgs("skill", args[1:])
		if err != nil {
			return err
		}
		switch action {
		case "install":
			return a.skillInstall(actionArgs)
		case "list":
			return a.skillList(actionArgs)
		case "remove":
			return a.skillRemove(actionArgs)
		}
	case "mcp":
		action, actionArgs, err := moduleActionArgs("mcp", args[1:])
		if err != nil {
			return err
		}
		switch action {
		case "install":
			return a.mcpInstall(actionArgs)
		case "list":
			return a.mcpList(actionArgs)
		case "remove":
			return a.mcpRemove(actionArgs)
		case "import":
			return a.mcpImport(actionArgs)
		}
	case "context":
		action, actionArgs, err := moduleActionArgs("context", args[1:])
		if err != nil {
			return err
		}
		switch action {
		case "new":
			return a.contextEdit(actionArgs, false)
		case "edit":
			if len(actionArgs) < 1 {
				return fail(2, errors.New("usage: cms context edit <context>"))
			}
			return a.contextEdit(actionArgs[1:], true, actionArgs[0])
		case "list":
			return a.contextList(actionArgs)
		}
	case "global":
		action, actionArgs, err := moduleActionArgs("global", args[1:])
		if err != nil {
			return err
		}
		switch action {
		case "init":
			return a.globalInit(actionArgs)
		case "remove":
			return a.globalRemove(actionArgs)
		}
	case "project":
		action, actionArgs, err := moduleActionArgs("project", args[1:])
		if err != nil {
			return err
		}
		switch action {
		case "init":
			return a.init(actionArgs)
		case "freeze":
			return a.freeze(actionArgs)
		case "sync":
			return a.syncProject(actionArgs)
		case "clear":
			return a.clear(actionArgs)
		}
	case "shell":
		action, actionArgs, err := moduleActionArgs("shell", args[1:])
		if err != nil {
			return err
		}
		if action == "completion" {
			return a.completion(actionArgs)
		}
	case "help", "--help", "-h":
		usage(a.Out)
		return nil
	default:
		return fail(2, fmt.Errorf("unknown command %q", args[0]))
	}
	return fail(2, fmt.Errorf("unknown command %q", args[0]))
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "CMS — Context Management System\n\nUsage:\n  cms <module> <action> [options]\n  cms help\n\nModules and actions:")
	for _, module := range moduleOrder {
		fmt.Fprintf(w, "\n%s:\n", module)
		for _, action := range moduleActionsHelp[module] {
			command := fmt.Sprintf("cms %s %s", module, action.usage)
			fmt.Fprintf(w, "  %-56s %s\n", command, action.description)
		}
	}
	fmt.Fprintln(w, "\nRun cms <module> to see the available actions for a module.")
}

func (a *App) skillList(args []string) error {
	jsonOut := a.JSON || has(args, "--json")
	plain := has(args, "--plain")
	if jsonOut && plain {
		return fail(2, errors.New("skill list --plain cannot be combined with --json"))
	}
	list, err := a.Registry.List()
	if err != nil {
		return fail(1, err)
	}
	if jsonOut {
		return json.NewEncoder(a.Out).Encode(list)
	}
	if !plain && interactive(a.In) {
		return tui.RunSkillList(list, a.In, a.Out, a.NoColor)
	}
	fmt.Fprintln(a.Out, "NAME\tID\tSOURCE")
	for _, s := range list {
		fmt.Fprintf(a.Out, "%-16s %-26s github:%s/%s\n", s.Name, s.ID, s.Source.Repository, s.Source.Path)
	}
	return nil
}

func (a *App) skillRemove(args []string) error {
	var ids []string
	removeAll, yes := false, false
	for _, arg := range args {
		switch arg {
		case "--all", "-a":
			removeAll = true
		case "--yes", "-y":
			yes = true
		default:
			if strings.HasPrefix(arg, "-") {
				return fail(2, fmt.Errorf("unknown skill remove option %q", arg))
			}
			ids = append(ids, arg)
		}
	}
	if removeAll && len(ids) > 0 {
		return fail(2, errors.New("skill remove --all cannot be combined with skill IDs"))
	}
	if !removeAll && len(ids) == 0 {
		return fail(2, errors.New("usage: cms skill remove <id>... [--yes] or cms skill remove --all [--yes]"))
	}

	list, err := a.Registry.List()
	if err != nil {
		return fail(1, err)
	}
	selected := make([]model.SkillMetadata, 0, len(list))
	if removeAll {
		selected = list
	} else {
		byID := make(map[string]model.SkillMetadata, len(list))
		for _, item := range list {
			byID[item.ID] = item
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			item, ok := byID[id]
			if !ok {
				return fail(6, fmt.Errorf("skill %q was not found", id))
			}
			selected = append(selected, item)
			seen[id] = true
		}
	}
	if len(selected) == 0 {
		if a.JSON {
			return json.NewEncoder(a.Out).Encode([]model.SkillMetadata{})
		}
		if !a.Quiet {
			fmt.Fprintln(a.Out, "no installed skills to remove")
		}
		return nil
	}
	if a.JSON && !yes {
		return fail(2, errors.New("skill remove --json requires --yes"))
	}

	if !yes {
		fmt.Fprintf(a.Out, "Remove %d skill(s)? [y/N]: ", len(selected))
		reader := bufio.NewReader(a.In)
		answer, readErr := reader.ReadString('\n')
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

	for _, item := range selected {
		if err := a.Registry.Remove(item.ID); err != nil {
			return fail(1, fmt.Errorf("could not remove skill %q: %w", item.ID, err))
		}
	}
	if a.JSON {
		return json.NewEncoder(a.Out).Encode(selected)
	}
	if !a.Quiet {
		for _, item := range selected {
			fmt.Fprintf(a.Out, "removed %s\n", item.ID)
		}
	}
	return nil
}

func (a *App) skillInstall(args []string) error {
	if len(args) > 1 {
		return fail(2, errors.New("usage: cms skill install [github-url]"))
	}
	if len(args) == 0 {
		return a.skillSearch()
	}
	source, err := gh.ParseURL(args[0])
	if err != nil {
		return fail(2, err)
	}
	p := a.Provider
	source, _ = p.Resolve(context.Background(), source)
	if source.Path == "" {
		if batch, ok := p.(providers.BatchSkillProvider); ok {
			return a.skillInstallAll(batch, source)
		}
	}
	tmp, err := os.MkdirTemp("", "cms-skill-")
	if err != nil {
		return fail(1, err)
	}
	defer os.RemoveAll(tmp)
	if err = p.Download(context.Background(), source, tmp); err != nil {
		return fail(5, err)
	}
	m, already, err := a.Registry.InstallDirectory(tmp, source)
	if err != nil {
		return fail(3, fmt.Errorf("skill is invalid: %w", err))
	}
	if a.JSON {
		if err := json.NewEncoder(a.Out).Encode(m); err != nil {
			return err
		}
	} else if !a.Quiet {
		if already {
			fmt.Fprintf(a.Out, "skill already installed: %s\n", m.ID)
		} else {
			fmt.Fprintf(a.Out, "installed %s\n", m.ID)
		}
		if m.HasScripts || m.HasExecutables {
			fmt.Fprintln(a.Out, "warning: skill contains scripts or executable files; installation does not imply trust")
		}
	}
	return nil
}

func (a *App) skillInstallAll(provider providers.BatchSkillProvider, source model.SkillSource) error {
	tmp, err := os.MkdirTemp("", "cms-skills-")
	if err != nil {
		return fail(1, err)
	}
	defer os.RemoveAll(tmp)

	downloaded, err := provider.DownloadAll(context.Background(), source, tmp)
	if err != nil {
		return fail(5, err)
	}
	if len(downloaded) == 0 {
		return fail(5, errors.New("repository does not contain any valid skills"))
	}

	// Validate the complete batch before changing the registry. This avoids
	// leaving an earlier skill installed when a later manifest is invalid.
	for _, item := range downloaded {
		if _, err := skills.ValidateDirectory(item.Directory); err != nil {
			return fail(3, fmt.Errorf("skill %q is invalid: %w", item.Source.Path, err))
		}
	}

	installed := make([]model.SkillMetadata, 0, len(downloaded))
	alreadyInstalled := make([]bool, 0, len(downloaded))
	newIDs := make([]string, 0, len(downloaded))
	for _, item := range downloaded {
		metadata, already, installErr := a.Registry.InstallDirectory(item.Directory, item.Source)
		if installErr != nil {
			for _, id := range newIDs {
				_ = a.Registry.Remove(id)
			}
			return fail(3, fmt.Errorf("could not install skill %q: %w", item.Source.Path, installErr))
		}
		installed = append(installed, metadata)
		alreadyInstalled = append(alreadyInstalled, already)
		if !already {
			newIDs = append(newIDs, metadata.ID)
		}
	}

	if a.JSON {
		if len(installed) == 1 {
			return json.NewEncoder(a.Out).Encode(installed[0])
		}
		return json.NewEncoder(a.Out).Encode(installed)
	}
	if !a.Quiet {
		for i, metadata := range installed {
			if alreadyInstalled[i] {
				fmt.Fprintf(a.Out, "skill already installed: %s\n", metadata.ID)
			} else {
				fmt.Fprintf(a.Out, "installed %s\n", metadata.ID)
			}
		}
		if len(installed) > 1 {
			fmt.Fprintf(a.Out, "installed %d skills from %s\n", len(installed), source.Repository)
		}
		for _, metadata := range installed {
			if metadata.HasScripts || metadata.HasExecutables {
				fmt.Fprintf(a.Out, "warning: %s contains scripts or executable files; installation does not imply trust\n", metadata.ID)
			}
		}
	}
	return nil
}

func (a *App) skillSearch() error {
	if !interactive(a.In) {
		return fail(2, errors.New("skill install without a URL requires an interactive terminal"))
	}
	m, err := tui.RunSkillSearch(a.Provider, a.Registry, a.In, a.Out, a.Config.GitHubSearchLimit, a.NoColor)
	if err != nil {
		return fail(5, err)
	}
	if m.ID != "" && !a.Quiet {
		fmt.Fprintf(a.Out, "installed %s\n", m.ID)
	}
	return nil
}

func (a *App) init(args []string) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	manifest, manifestErr := storage.LoadManifest(root)
	hasManifest := manifestErr == nil
	if manifestErr != nil && !errors.Is(manifestErr, os.ErrNotExist) {
		return fail(3, manifestErr)
	}
	name := ""
	argStart := 0
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, argStart = args[0], 1
	}
	if has(args, "--global") {
		if name != "" {
			return fail(2, errors.New("cms project init --global cannot be combined with a context name"))
		}
		globalArgs := make([]string, 0, len(args)-1)
		for _, arg := range args {
			if arg != "--global" {
				globalArgs = append(globalArgs, arg)
			}
		}
		return a.globalInit(globalArgs)
	}
	if name == "" {
		if !hasManifest {
			return fail(2, errors.New("usage: cms project init [context] [--target name] [--dry-run], or add cms.toml"))
		}
		name = manifest.Context
	}
	if a.Config.LinkMode != "symlink" {
		return fail(2, fmt.Errorf("unsupported linking mode %q; the MVP supports symlink", a.Config.LinkMode))
	}
	var targetNames []string
	dry := false
	var syncedSkills []model.SkillMetadata
	targetNames, dry, err = parseInitOptions(args[argStart:])
	if err != nil {
		return fail(2, err)
	}
	if !dry && hasManifest && manifest.Context == name {
		var syncErr error
		syncedSkills, _, syncErr = a.syncManifest(manifest)
		if syncErr != nil {
			return syncErr
		}
	}
	c, err := a.Contexts.Get(name)
	if err != nil {
		return fail(6, err)
	}
	if err := a.Contexts.Validate(c); err != nil {
		return fail(3, err)
	}
	if len(targetNames) == 0 {
		if hasManifest && manifest.Context == name && len(manifest.Targets) > 0 {
			targetNames = manifest.Targets
		} else {
			targetNames = a.defaultTargetNames()
		}
	}
	var adapters []harness.Adapter
	for _, n := range targetNames {
		ad, ok := a.Harnesses[n]
		if !ok {
			return fail(2, fmt.Errorf("unknown harness target %q", n))
		}
		adapters = append(adapters, ad)
	}
	state, err := storage.LoadState(root)
	if err != nil {
		return err
	}
	plan, err := linker.BuildPlan(root, state, c, adapters, a.Registry)
	if err != nil {
		return fail(3, err)
	}
	mcpPlans, err := a.buildMCPPlans(root, state, c, adapters, model.ScopeProject)
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
	if len(mcpActions) > 0 {
		for _, w := range mcpWarnings {
			if !a.JSON {
				fmt.Fprintf(a.Err, "warning: %s\n", w.Message)
			}
		}
	}
	for _, item := range mcpPlans {
		if len(item.plan.Conflicts()) > 0 {
			return fail(4, errors.New("MCP config conflicts found; no files were changed"))
		}
	}
	if a.JSON {
		if len(mcpActions) == 0 {
			if err := json.NewEncoder(a.Out).Encode(plan.Actions); err != nil {
				return err
			}
		} else if err := json.NewEncoder(a.Out).Encode(struct {
			Skills   any                  `json:"skills"`
			MCP      []harness.MCPAction  `json:"mcp"`
			Warnings []harness.MCPWarning `json:"warnings"`
		}{plan.Actions, mcpActions, mcpWarnings}); err != nil {
			return err
		}
	} else if !a.Quiet {
		printSyncedSkills(a.Out, syncedSkills)
		for _, action := range plan.Actions {
			fmt.Fprintf(a.Out, "%-8s %s\n", action.Kind, action.Target)
		}
		for _, action := range mcpActions {
			fmt.Fprintf(a.Out, "%-8s %s (%s)\n", action.Kind, action.Name, action.Target)
		}
	}
	if dry {
		return nil
	}
	rollbackMCP, mcpErr := applyMCPPlans(mcpPlans)
	if mcpErr != nil {
		return fail(4, mcpErr)
	}
	rollbackLinks, applyErr := linker.ApplyWithRollback(root, plan, a.Registry)
	if applyErr != nil {
		if rollbackMCP != nil {
			rollbackMCP()
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
		fmt.Fprintln(a.Out, "Applied. An already-open harness may need to be restarted to rediscover skills.")
	} else {
		fmt.Fprintln(a.Out, "No changes.")
	}
	return nil
}

func printSyncedSkills(out io.Writer, installed []model.SkillMetadata) {
	if len(installed) == 0 {
		return
	}
	fmt.Fprintf(out, "installed %d skill(s) required by cms.toml:\n", len(installed))
	for _, skill := range installed {
		fmt.Fprintf(out, "  %s\n", skill.ID)
	}
}

func (a *App) contextList(args []string) error {
	plain, jsonOut := has(args, "--plain"), a.JSON || has(args, "--json")
	list, err := a.Contexts.List()
	if err != nil {
		return err
	}
	if jsonOut {
		return json.NewEncoder(a.Out).Encode(list)
	}
	if plain {
		for _, c := range list {
			fmt.Fprintln(a.Out, c.Name)
		}
		return nil
	}
	if !interactive(a.In) {
		return fail(2, errors.New("context list requires a TTY; use --plain or --json"))
	}
	installed, _ := a.Registry.List()
	skillNames := make(map[string]string, len(installed))
	for _, skill := range installed {
		skillNames[skill.ID] = skill.Name
	}
	return tui.RunContextList(list, skillNames, a.In, a.Out, a.NoColor)
}

func (a *App) contextEdit(args []string, editing bool, oldName ...string) error {
	if interactive(a.In) && len(args) == 0 {
		initial := model.Context{}
		if editing {
			loaded, err := a.Contexts.Get(oldName[0])
			if err != nil {
				return fail(6, err)
			}
			initial = loaded
		}
		result, saved, err := tui.RunContextEditorWithMCP(a.Registry, a.MCPs, initial, a.In, a.Out)
		if err != nil {
			return fail(3, err)
		}
		if !saved {
			return nil
		}
		if !editing {
			if _, err := os.Stat(a.Contexts.Path(result.Name)); err == nil {
				return fail(3, fmt.Errorf("context %q already exists", result.Name))
			}
		}
		var saveErr error
		if editing {
			saveErr = a.Contexts.Rename(oldName[0], result)
		} else {
			saveErr = a.Contexts.Save(result)
		}
		if err := saveErr; err != nil {
			return fail(3, err)
		}
		if !a.Quiet {
			fmt.Fprintf(a.Out, "saved context %s\n", result.Name)
		}
		return nil
	}
	name, desc, skillsArg, mcpArg := flagValue(args, "--name"), flagValue(args, "--description"), allFlagValues(args, "--skill"), allFlagValues(args, "--mcp")
	if editing {
		current, err := a.Contexts.Get(oldName[0])
		if err != nil {
			return fail(6, err)
		}
		if name == "" {
			name = current.Name
		}
		if desc == "" {
			desc = current.Description
		}
		if len(skillsArg) == 0 {
			for _, s := range current.Skills {
				skillsArg = append(skillsArg, s.ID)
			}
		}
		if len(mcpArg) == 0 {
			for _, r := range current.MCPRefs {
				mcpArg = append(mcpArg, r.ID)
			}
			if len(mcpArg) == 0 {
				mcpArg = append(mcpArg, current.MCPs...)
			}
		}
	} else if name == "" && !interactive(a.In) {
		return fail(2, errors.New("context new requires a TTY or --name/--skill/--mcp flags"))
	} else if name != "" {
		if _, err := os.Stat(a.Contexts.Path(name)); err == nil {
			return fail(3, fmt.Errorf("context %q already exists", name))
		}
	}
	if interactive(a.In) && (!editing || len(args) == 0) {
		reader := bufio.NewReader(a.In)
		if editing && len(args) == 0 {
			if value := promptReader(a, reader, fmt.Sprintf("Context name [%s]: ", name)); value != "" {
				name = value
			}
			if value := promptReader(a, reader, fmt.Sprintf("Description [%s]: ", desc)); value != "" {
				desc = value
			}
		}
		if name == "" {
			name = promptReader(a, reader, "Context name: ")
		}
		if desc == "" {
			desc = promptReader(a, reader, "Description: ")
		}
		if (len(skillsArg) == 0 && len(mcpArg) == 0) || (editing && len(args) == 0) {
			installed, _ := a.Registry.List()
			fmt.Fprintln(a.Out, "Installed skills:")
			for _, s := range installed {
				fmt.Fprintf(a.Out, "  %s (%s)\n", s.ID, s.Name)
			}
			for _, id := range skillsArg {
				if _, err := a.Registry.Get(id); err != nil {
					fmt.Fprintf(a.Out, "  %s [missing from registry]\n", id)
				}
			}
			mcps, _ := a.MCPs.List()
			if len(mcps) > 0 {
				fmt.Fprintln(a.Out, "Installed MCPs:")
				for _, m := range mcps {
					fmt.Fprintf(a.Out, "  %s (%s)\n", m.ID, m.Name)
				}
			}
			label := "Skill IDs (comma-separated): "
			if editing && len(args) == 0 {
				label = "Skill IDs (comma-separated, empty keeps current): "
			}
			raw := promptReader(a, reader, label)
			if strings.TrimSpace(raw) != "" {
				skillsArg = nil
				for _, v := range strings.Split(raw, ",") {
					if strings.TrimSpace(v) != "" {
						skillsArg = append(skillsArg, strings.TrimSpace(v))
					}
				}
			}
			if len(mcpArg) == 0 || (editing && len(args) == 0) {
				label := "MCP IDs (comma-separated): "
				if editing && len(args) == 0 {
					label = "MCP IDs (comma-separated, empty keeps current): "
				}
				raw := promptReader(a, reader, label)
				if strings.TrimSpace(raw) != "" {
					mcpArg = nil
					for _, v := range strings.Split(raw, ",") {
						if strings.TrimSpace(v) != "" {
							mcpArg = append(mcpArg, strings.TrimSpace(v))
						}
					}
				}
			}
		}
	}
	if name == "" {
		return fail(3, errors.New("context name is required"))
	}
	if !editing && name == globalContextName {
		return fail(3, fmt.Errorf("context %q is reserved; use cms global init", globalContextName))
	}
	if editing && ((oldName[0] == globalContextName && name != globalContextName) || (oldName[0] != globalContextName && name == globalContextName)) {
		return fail(3, fmt.Errorf("context %q is reserved and cannot be renamed", globalContextName))
	}
	var refs []model.SkillRef
	for _, id := range skillsArg {
		refs = append(refs, model.SkillRef{ID: id})
	}
	c := model.Context{Name: name, Description: desc, Skills: refs, MCPs: append([]string(nil), mcpArg...)}
	for _, id := range mcpArg {
		c.MCPRefs = append(c.MCPRefs, model.MCPRef{ID: id})
	}
	if !editing {
		if _, err := os.Stat(a.Contexts.Path(name)); err == nil {
			return fail(3, fmt.Errorf("context %q already exists", name))
		}
	}
	if editing && oldName[0] != name {
		if _, err := os.Stat(a.Contexts.Path(name)); err == nil {
			return fail(3, fmt.Errorf("context %q already exists", name))
		}
		if err := a.Contexts.Validate(c); err != nil {
			return fail(3, err)
		}
		if err := a.Contexts.Rename(oldName[0], c); err != nil {
			return fail(3, err)
		}
	} else if err := a.Contexts.Save(c); err != nil {
		return fail(3, err)
	}
	if !a.Quiet {
		fmt.Fprintf(a.Out, "saved context %s\n", name)
	}
	return nil
}

func (a *App) completion(args []string) error {
	if len(args) != 1 {
		return fail(2, errors.New("usage: cms shell completion <bash|zsh|fish|powershell>"))
	}
	switch args[0] {
	case "bash":
		fmt.Fprintln(a.Out, `_cms_complete() {
  local cur="${COMP_WORDS[COMP_CWORD]}" cmd="${COMP_WORDS[1]}" action="${COMP_WORDS[2]}" cms_bin="${COMP_WORDS[0]}"
  if (( COMP_CWORD == 1 )); then
    COMPREPLY=( $(compgen -W "config skill mcp context global project shell help" -- "$cur") )
  elif [[ "$cmd" == "context" ]]; then
    if [[ "$action" == "edit" && COMP_CWORD -ge 3 ]]; then
      COMPREPLY=( $(compgen -W "$("$cms_bin" context list --plain 2>/dev/null)" -- "$cur") )
    elif (( COMP_CWORD == 2 )); then
      COMPREPLY=( $(compgen -W "new edit list" -- "$cur") )
    fi
  elif [[ "$cmd" == "project" ]]; then
    if [[ ("$action" == "init" || "$action" == "freeze") && COMP_CWORD -ge 3 ]]; then
      COMPREPLY=( $(compgen -W "$("$cms_bin" context list --plain 2>/dev/null)" -- "$cur") )
    elif (( COMP_CWORD == 2 )); then
      COMPREPLY=( $(compgen -W "init freeze sync clear" -- "$cur") )
    fi
  elif [[ "$cmd" == "skill" ]]; then
    COMPREPLY=( $(compgen -W "install list remove" -- "$cur") )
  elif [[ "$cmd" == "mcp" ]]; then
    COMPREPLY=( $(compgen -W "install list remove import" -- "$cur") )
  elif [[ "$cmd" == "global" ]]; then
    COMPREPLY=( $(compgen -W "init remove" -- "$cur") )
  elif [[ "$cmd" == "shell" ]]; then
    COMPREPLY=( $(compgen -W "completion" -- "$cur") )
  fi
}
complete -F _cms_complete cms ./cms`)
	case "zsh":
		fmt.Fprintln(a.Out, `_cms_complete() {
  local cms_bin=$words[1]
  if (( CURRENT == 2 )); then
    compadd config skill mcp context global project shell help
  elif [[ $words[2] == context ]]; then
    if [[ $words[3] == edit && CURRENT -ge 4 ]]; then
      compadd -- ${(f)"$($cms_bin context list --plain 2>/dev/null)"}
    else
      compadd new edit list
    fi
  elif [[ $words[2] == project ]]; then
    if [[ ($words[3] == init || $words[3] == freeze) && CURRENT -ge 4 ]]; then
      compadd -- ${(f)"$($cms_bin context list --plain 2>/dev/null)"}
    else
      compadd init freeze sync clear
    fi
  elif [[ $words[2] == skill ]]; then
    compadd install list remove
  elif [[ $words[2] == mcp ]]; then
    compadd install list remove import
  elif [[ $words[2] == global ]]; then
    compadd init remove
  elif [[ $words[2] == shell ]]; then
    compadd completion
  fi
}
compdef _cms_complete cms ./cms`)
	case "fish":
		fmt.Fprintln(a.Out, `complete -c cms -n '__fish_seen_subcommand_from project context' -a '(cms context list --plain 2>/dev/null)'`)
	case "powershell":
		fmt.Fprintln(a.Out, `Register-ArgumentCompleter -CommandName cms -ScriptBlock { param($wordToComplete) cms context list --plain | Where-Object { $_ -like "$wordToComplete*" } }`)
	default:
		return fail(2, fmt.Errorf("unsupported shell %q", args[0]))
	}
	return nil
}

func interactive(in io.Reader) bool {
	if f, ok := in.(*os.File); ok {
		info, err := f.Stat()
		return err == nil && info.Mode()&os.ModeCharDevice != 0
	}
	return false
}
func prompt(a *App, label string) string {
	return promptReader(a, bufio.NewReader(a.In), label)
}
func promptReader(a *App, reader *bufio.Reader, label string) string {
	fmt.Fprint(a.Out, label)
	s, _ := reader.ReadString('\n')
	return strings.TrimSpace(s)
}
func has(args []string, v string) bool {
	for _, a := range args {
		if a == v {
			return true
		}
	}
	return false
}
func flagValue(args []string, name string) string {
	for i, v := range args {
		if v == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
func allFlagValues(args []string, name string) []string {
	var out []string
	for i, v := range args {
		if v == name && i+1 < len(args) {
			out = append(out, args[i+1])
		}
	}
	sort.Strings(out)
	return out
}

type globalFlags struct{ json, quiet, verbose, noColor bool }

func parseGlobalFlags(args []string) ([]string, globalFlags) {
	var flags globalFlags
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
		case "--json":
			flags.json = true
		case "--quiet":
			flags.quiet = true
		case "--verbose":
			flags.verbose = true
		case "--no-color":
			flags.noColor = true
		default:
			filtered = append(filtered, arg)
		}
	}
	return filtered, flags
}
