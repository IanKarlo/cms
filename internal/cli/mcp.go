package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ikts/cms/internal/harness"
	"github.com/ikts/cms/internal/model"
	"github.com/ikts/cms/internal/providers"
	"github.com/ikts/cms/internal/storage"
	"github.com/ikts/cms/internal/tui"
)

type plannedMCP struct {
	target, path string
	adapter      harness.MCPAdapter
	plan         harness.MCPPlan
}

type MCPPreflightError struct{ Code, MCP, Variable, Runtime string }

type MCPVariantSelectionError struct {
	Requested string
	Variants  []string
}

func (e MCPVariantSelectionError) Error() string {
	if e.Requested != "" {
		return fmt.Sprintf("requested MCP registry variant %q was not found; choose from %s", e.Requested, strings.Join(e.Variants, ", "))
	}
	return fmt.Sprintf("MCP registry source has multiple supported variants; choose --variant from %s", strings.Join(e.Variants, ", "))
}

func (e MCPPreflightError) Error() string {
	if e.Variable != "" {
		return fmt.Sprintf("MCP %q requires environment variable %s", e.MCP, e.Variable)
	}
	return fmt.Sprintf("MCP runtime %q not found", e.Runtime)
}

func (a *App) buildMCPPlans(root string, current model.ProjectState, c model.Context, targets []harness.Adapter, scope model.Scope) ([]plannedMCP, error) {
	refs := c.MCPRefs
	if len(refs) == 0 {
		for _, id := range c.MCPs {
			refs = append(refs, model.MCPRef{ID: id})
		}
	}
	desired := make([]model.ResolvedMCP, 0, len(refs))
	for _, ref := range refs {
		m, err := a.MCPs.Get(ref.ID)
		if err != nil {
			return nil, err
		}
		if err := preflightMCP(m); err != nil {
			return nil, fail(3, err)
		}
		if a.Verbose && m.Transport == model.MCPTransportStdio && m.Command != nil {
			fmt.Fprintf(a.Err, "MCP preflight: %s found for %s\n", m.Command.Command, m.Name)
		}
		desired = append(desired, model.ResolvedMCP{Metadata: m, Ref: ref})
	}
	allTargets := append([]harness.Adapter(nil), targets...)
	seenTargets := map[string]bool{}
	for _, ad := range allTargets {
		seenTargets[ad.ID()] = true
	}
	for _, e := range current.MCPEntries {
		if e.Scope != string(scope) || seenTargets[e.Target] {
			continue
		}
		if ad, ok := a.Harnesses[e.Target]; ok {
			allTargets = append(allTargets, ad)
			seenTargets[e.Target] = true
		}
	}
	var plans []plannedMCP
	for _, ad := range allTargets {
		mcpAd, ok := ad.(harness.MCPAdapter)
		if !ok {
			if len(desired) > 0 {
				return nil, fmt.Errorf("target %q does not support MCP", ad.ID())
			}
			continue
		}
		path := mcpAd.ProjectMCPConfigPath(root)
		if scope == model.ScopeGlobal {
			path = mcpAd.GlobalMCPConfigPath(root)
		}
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		entries := make([]model.MCPStateEntry, 0)
		for _, e := range current.MCPEntries {
			if e.Target == ad.ID() && e.Scope == string(scope) {
				entries = append(entries, e)
			}
		}
		p, err := mcpAd.PlanMCPMutation(data, desired, entries, scope)
		if err != nil {
			return nil, fail(3, err)
		}
		for i := range p.Entries {
			p.Entries[i].ConfigPath = path
			if rel, e := filepath.Rel(root, path); e == nil {
				p.Entries[i].ConfigPath = filepath.ToSlash(rel)
			}
		}
		for i := range p.Actions {
			p.Actions[i].ConfigPath = filepath.ToSlash(path)
			if rel, e := filepath.Rel(root, path); e == nil {
				p.Actions[i].ConfigPath = filepath.ToSlash(rel)
			}
		}
		if a.Verbose {
			for _, action := range p.Actions {
				fmt.Fprintf(a.Err, "MCP plan [%s]: %s %s in %s\n", ad.ID(), action.Kind, action.Name, action.ConfigPath)
			}
		}
		plans = append(plans, plannedMCP{ad.ID(), path, mcpAd, p})
	}
	return plans, nil
}

func applyMCPPlans(plans []plannedMCP) (func(), error) {
	originals := map[string][]byte{}
	existing := map[string]bool{}
	touched := map[string]bool{}
	for _, item := range plans {
		if len(item.plan.Actions) == 0 {
			continue
		}
		touched[item.path] = true
		b, err := os.ReadFile(item.path)
		if err == nil {
			originals[item.path] = b
			existing[item.path] = true
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	rollback := func() {
		for path, b := range originals {
			_ = harness.WriteMCPConfig(path, b)
		}
		for path := range touched {
			if _, ok := existing[path]; !ok {
				_ = os.Remove(path)
			}
		}
	}
	for _, item := range plans {
		if len(item.plan.Actions) == 0 {
			continue
		}
		if err := harness.WriteMCPConfig(item.path, item.plan.Data); err != nil {
			rollback()
			return nil, err
		}
	}
	return rollback, nil
}
func hasMCPData(data []byte) bool {
	return len(strings.TrimSpace(string(data))) > 0 && strings.TrimSpace(string(data)) != "{}" && strings.TrimSpace(string(data)) != "{\n}"
}

func (a *App) mcpInstall(args []string) error {
	var source, name, runtime, cwd, bearer string
	var version, variant string
	var command string
	var envArgs, reqEnv, headers, publicHeaders []string
	var envBindings, publicEnv []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--name":
			if i+1 >= len(args) {
				return fail(2, errors.New("--name requires a value"))
			}
			name = args[i+1]
			i++
		case "--version":
			if i+1 >= len(args) {
				return fail(2, errors.New("--version requires a value"))
			}
			version = args[i+1]
			i++
		case "--variant":
			if i+1 >= len(args) {
				return fail(2, errors.New("--variant requires a value"))
			}
			variant = args[i+1]
			i++
		case "--runtime":
			if i+1 >= len(args) {
				return fail(2, errors.New("--runtime requires a value"))
			}
			runtime = args[i+1]
			i++
		case "--command":
			if i+1 >= len(args) {
				return fail(2, errors.New("--command requires a value"))
			}
			command = args[i+1]
			i++
		case "--arg":
			if i+1 >= len(args) {
				return fail(2, errors.New("--arg requires a value"))
			}
			envArgs = append(envArgs, args[i+1])
			i++
		case "--cwd":
			if i+1 >= len(args) {
				return fail(2, errors.New("--cwd requires a value"))
			}
			cwd = args[i+1]
			i++
		case "--require-env":
			if i+1 >= len(args) {
				return fail(2, errors.New("--require-env requires a value"))
			}
			reqEnv = append(reqEnv, args[i+1])
			i++
		case "--env":
			if i+1 >= len(args) {
				return fail(2, errors.New("--env requires NAME=${VAR}"))
			}
			envBindings = append(envBindings, args[i+1])
			i++
		case "--public-env":
			if i+1 >= len(args) {
				return fail(2, errors.New("--public-env requires NAME=value"))
			}
			publicEnv = append(publicEnv, args[i+1])
			envBindings = append(envBindings, args[i+1])
			i++
		case "--header":
			if i+1 >= len(args) {
				return fail(2, errors.New("--header requires NAME=value"))
			}
			headers = append(headers, args[i+1])
			i++
		case "--public-header":
			if i+1 >= len(args) {
				return fail(2, errors.New("--public-header requires NAME=value"))
			}
			publicHeaders = append(publicHeaders, args[i+1])
			i++
		case "--header-env":
			if i+1 >= len(args) {
				return fail(2, errors.New("--header-env requires NAME=ENV"))
			}
			v := strings.SplitN(args[i+1], "=", 2)
			if len(v) != 2 {
				return fail(2, errors.New("--header-env requires NAME=ENV"))
			}
			headers = append(headers, v[0]+"=${"+v[1]+"}")
			i++
		case "--bearer-env":
			if i+1 >= len(args) {
				return fail(2, errors.New("--bearer-env requires ENV"))
			}
			bearer = args[i+1]
			i++
		default:
			if strings.HasPrefix(args[i], "-") {
				return fail(2, fmt.Errorf("unknown mcp install option %q", args[i]))
			}
			if source != "" {
				return fail(2, errors.New("mcp install accepts only one source"))
			}
			source = args[i]
		}
	}
	if source == "" && command == "" {
		if !interactive(a.In) {
			return fail(2, errors.New("mcp install without a source requires an interactive terminal"))
		}
		if a.MCPProvider == nil {
			return fail(5, errors.New("Official MCP Registry provider is unavailable"))
		}
		selected, searchErr := tui.RunMCPRegistrySearch(a.MCPProvider, a.In, a.Out, a.Config.MCPRegistrySearchLimit, a.NoColor)
		if searchErr != nil {
			return fail(5, searchErr)
		}
		if selected.Name == "" {
			return nil
		}
		source = "registry:" + selected.Name
		if version == "" {
			version = selected.Version
		}
	}
	m := model.MCPMetadata{SchemaVersion: 1, Name: name, Description: "", RegisteredAt: time.Now().UTC(), Source: model.MCPSource{Type: model.MCPSourceCommand}}
	if source != "" {
		if strings.HasPrefix(source, "registry:") {
			if a.MCPProvider == nil {
				return fail(5, errors.New("Official MCP Registry provider is unavailable"))
			}
			registryName := strings.TrimPrefix(source, "registry:")
			variants, resolveErr := a.MCPProvider.Resolve(context.Background(), providers.MCPProviderRef{Name: registryName, Version: version})
			if resolveErr != nil {
				return fail(5, resolveErr)
			}
			validVariants := variantNameSlice(variants)
			if variant == "" && len(variants) > 1 && interactive(a.In) {
				fmt.Fprintf(a.Out, "Available variants: %s\n", strings.Join(validVariants, ", "))
				variant = prompt(a, "Variant: ")
			}
			if variant != "" {
				selected := variants[:0]
				for _, candidate := range variants {
					if candidate.Variant == variant {
						selected = append(selected, candidate)
					}
				}
				variants = selected
			}
			if len(variants) != 1 {
				return fail(2, MCPVariantSelectionError{Requested: variant, Variants: validVariants})
			}
			v := variants[0]
			if a.Verbose {
				fmt.Fprintf(a.Err, "MCP registry: resolved %s@%s (%s)\n", v.Source.RegistryName, v.Source.Version, v.Variant)
			}
			if name == "" {
				name = suggestedMCPName(v.Name)
			}
			m = model.MCPMetadata{SchemaVersion: 1, Name: name, Description: v.Description, RegisteredAt: time.Now().UTC(), Source: v.Source, Transport: v.Transport, Command: v.Command, Remote: v.Remote, Requirements: v.Requirements, Reproducible: v.Reproducible}
		} else if strings.HasPrefix(source, "npm:") || strings.HasPrefix(source, "pypi:") || strings.HasPrefix(source, "oci:") {
			parts := strings.SplitN(source, ":", 2)
			if len(parts) != 2 || parts[1] == "" {
				return fail(2, errors.New("invalid package source"))
			}
			if name == "" {
				name = packageName(parts[1])
			}
			m.Name = name
			m.Source = model.MCPSource{Type: model.MCPSourcePackage, PackageType: parts[0], PackageIdentifier: parts[1], PackageVersion: version, Variant: variant}
			m.Transport = model.MCPTransportStdio
			runtime = runtimeFor(parts[0], runtime)
			argsFor := []string{}
			switch parts[0] {
			case "npm":
				argsFor = []string{"-y", parts[1]}
				if version != "" {
					argsFor = []string{"-y", parts[1] + "@" + version}
				}
			case "pypi":
				argsFor = []string{parts[1]}
				if version != "" {
					argsFor = []string{parts[1] + "==" + version}
				}
			case "oci":
				argsFor = []string{"run", "--rm", parts[1]}
				if version != "" {
					sep := ":"
					if strings.HasPrefix(version, "sha256:") {
						sep = "@"
					}
					argsFor = []string{"run", "--rm", parts[1] + sep + version}
				}
			}
			m.Command = &model.MCPCommand{Command: runtime, Args: argsFor}
		} else {
			u, err := url.Parse(source)
			if err != nil || u.Scheme != "http" && u.Scheme != "https" {
				return fail(2, errors.New("source must be an http(s) URL, npm:, pypi:, oci:, or --command"))
			}
			if name == "" {
				if !interactive(a.In) {
					return fail(2, errors.New("remote URL requires --name in non-interactive mode"))
				}
				name = prompt(a, "MCP name: ")
				if name == "" {
					return fail(2, errors.New("MCP name is required"))
				}
			}
			m.Name = name
			m.Transport = model.MCPTransportHTTP
			m.Source = model.MCPSource{Type: model.MCPSourceRemote}
			m.Remote = &model.MCPRemote{URL: source, Headers: map[string]string{}, Auth: model.MCPAuth{Mode: "none"}}
			if bearer != "" {
				m.Remote.Auth = model.MCPAuth{Mode: "bearer-env", Env: bearer}
				m.Requirements = append(m.Requirements, model.MCPRequirement{Kind: "env", Name: bearer, Required: true, Secret: true})
			}
			for _, raw := range append(headers, publicHeaders...) {
				k, v, err := splitKV(raw)
				if err != nil {
					return fail(2, err)
				}
				if !contains(publicHeaders, raw) && !strings.Contains(v, "${") {
					return fail(3, fmt.Errorf("header %q must use an env template or --public-header", k))
				}
				if err := model.ValidateMCPTemplate(v); err != nil {
					return fail(3, err)
				}
				m.Remote.Headers[k] = v
				for _, env := range envRefs(v) {
					m.Requirements = appendUniqueRequirement(m.Requirements, model.MCPRequirement{Kind: "env", Name: env, Required: true, Secret: true})
				}
			}
		}
	} else {
		if name == "" {
			return fail(2, errors.New("--command requires --name"))
		}
		m.Name = name
		m.Transport = model.MCPTransportStdio
		m.Command = &model.MCPCommand{Command: command, Args: envArgs, Cwd: cwd, Env: map[string]string{}}
		for _, raw := range reqEnv {
			m.Requirements = appendUniqueRequirement(m.Requirements, model.MCPRequirement{Kind: "env", Name: raw, Required: true, Secret: true})
		}
		for _, raw := range envBindings {
			if strings.Contains(raw, "=") {
				k, v, _ := splitKV(raw)
				if k != "" {
					if !contains(publicEnv, raw) && len(envRefs(v)) == 0 {
						return fail(3, fmt.Errorf("environment value %q must use an env template or --public-env", k))
					}
					m.Command.Env[k] = v
					for _, env := range envRefs(v) {
						m.Requirements = appendUniqueRequirement(m.Requirements, model.MCPRequirement{Kind: "env", Name: env, Required: true, Secret: true})
					}
				}
			}
		}
	}
	m.Reproducible = (m.Source.Type == model.MCPSourcePackage && m.Source.PackageVersion != "" && (m.Source.PackageType != "oci" || strings.HasPrefix(m.Source.PackageVersion, "sha256:"))) ||
		(m.Source.Type == model.MCPSourceRegistry && ((m.Source.PackageType == "" && m.Source.Version != "") || (m.Source.PackageVersion != "" && (m.Source.PackageType != "oci" || strings.HasPrefix(m.Source.PackageVersion, "sha256:")))))
	registered, already, err := a.MCPs.Register(m)
	if err != nil {
		return fail(3, err)
	}
	if a.JSON {
		return jsonEncode(a.Out, registered)
	}
	if !a.Quiet {
		if already {
			fmt.Fprintf(a.Out, "MCP already registered: %s\n", registered.ID)
		} else {
			fmt.Fprintf(a.Out, "registered %s\n", registered.ID)
		}
	}
	return nil
}

func (a *App) mcpList(args []string) error {
	plain := has(args, "--plain")
	jsonOut := a.JSON || has(args, "--json")
	if plain && jsonOut {
		return fail(2, errors.New("mcp list --plain cannot be combined with --json"))
	}
	list, err := a.MCPs.List()
	if err != nil {
		return err
	}
	if jsonOut {
		type item struct {
			ID           string   `json:"id"`
			Name         string   `json:"name"`
			DisplayName  string   `json:"display_name"`
			Transport    string   `json:"transport"`
			Source       string   `json:"source"`
			Reproducible bool     `json:"reproducible"`
			Requirements []string `json:"requirements"`
		}
		labels := model.MCPDisplayLabels(list)
		out := make([]item, 0, len(list))
		for _, m := range list {
			var req []string
			for _, r := range m.Requirements {
				if r.Kind == "env" {
					req = append(req, r.Name)
				}
			}
			out = append(out, item{m.ID, m.Name, labels[m.ID], string(m.Transport), string(m.Source.Type), m.Reproducible, req})
		}
		return jsonEncode(a.Out, out)
	}
	if plain {
		labels := model.MCPDisplayLabels(list)
		writer := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "DISPLAY_NAME\tID\tTRANSPORT\tSOURCE")
		for _, m := range list {
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", labels[m.ID], m.ID, m.Transport, mcpPlainSource(m))
		}
		if err := writer.Flush(); err != nil {
			return err
		}
		return nil
	}
	if interactive(a.In) {
		return tui.RunMCPList(list, a.In, a.Out, a.NoColor)
	}
	labels := model.MCPDisplayLabels(list)
	nameWidth := len("NAME")
	for _, m := range list {
		if len(labels[m.ID]) > nameWidth {
			nameWidth = len(labels[m.ID])
		}
	}
	fmt.Fprintf(a.Out, "%-*s  %-26s  %-16s  %s\n", nameWidth, "NAME", "ID", "TRANSPORT", "SOURCE")
	for _, m := range list {
		fmt.Fprintf(a.Out, "%-*s  %-26s  %-16s  %s\n", nameWidth, labels[m.ID], m.ID, m.Transport, m.Source.Type)
	}
	return nil
}

func mcpPlainSource(m model.MCPMetadata) string {
	origin := m.DisplayOrigin()
	if origin == "" {
		return string(m.Source.Type)
	}
	return string(m.Source.Type) + ":" + origin
}

func (a *App) mcpRemove(args []string) error {
	if len(args) == 0 {
		return fail(2, errors.New("usage: cms mcp remove <id>..."))
	}
	contexts, _ := a.Contexts.List()
	root, _ := os.Getwd()
	manifest, _ := storage.LoadManifest(root)
	state, _ := storage.LoadState(root)
	globalRoot, _ := os.UserHomeDir()
	globalState, _ := storage.LoadState(globalRoot)
	selected := make([]model.MCPMetadata, 0, len(args))
	seen := map[string]bool{}
	for _, id := range args {
		if seen[id] {
			continue
		}
		seen[id] = true
		for _, c := range contexts {
			for _, r := range c.MCPRefs {
				if r.ID == id {
					return fail(3, fmt.Errorf("MCP %q is referenced by context %q", id, c.Name))
				}
			}
			for _, legacy := range c.MCPs {
				if legacy == id {
					return fail(3, fmt.Errorf("MCP %q is referenced by context %q", id, c.Name))
				}
			}
		}
		for _, p := range manifest.MCPs {
			if p.Metadata.ID == id {
				return fail(3, fmt.Errorf("MCP %q is referenced by cms.toml", id))
			}
		}
		for _, e := range state.MCPEntries {
			if e.MCPID == id {
				return fail(3, fmt.Errorf("MCP %q is active in the current state", id))
			}
		}
		for _, e := range globalState.MCPEntries {
			if e.MCPID == id {
				return fail(3, fmt.Errorf("MCP %q is active in the global state", id))
			}
		}
		m, err := a.MCPs.Get(id)
		if err != nil {
			return fail(6, err)
		}
		selected = append(selected, m)
	}
	for i, m := range selected {
		if err := a.MCPs.Remove(m.ID); err != nil {
			for _, restored := range selected[:i] {
				_, _, _ = a.MCPs.Register(restored)
			}
			return fail(1, fmt.Errorf("could not remove MCP %q: %w", m.ID, err))
		}
	}
	if a.JSON {
		return jsonEncode(a.Out, selected)
	}
	if !a.Quiet {
		for _, m := range selected {
			fmt.Fprintf(a.Out, "removed %s\n", m.ID)
		}
	}
	return nil
}

func (a *App) mcpImport(args []string) error {
	if len(args) < 1 {
		return fail(2, errors.New("usage: cms mcp import <path> --target <target>"))
	}
	path := args[0]
	target := flagValue(args, "--target")
	all := has(args, "--all")
	selectedNames := allFlagValues(args, "--name")
	if target == "" {
		target = inferMCPTarget(path)
	}
	ad, ok := a.Harnesses[target]
	if !ok {
		return fail(2, fmt.Errorf("unknown harness target %q", target))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fail(1, err)
	}
	items, err := importMCPJSON(data, target)
	if target == "codex" {
		items, err = importMCPText(data, target)
	}
	if err != nil {
		return fail(3, err)
	}
	if len(items) > 1 && !all && len(selectedNames) == 0 && !interactive(a.In) {
		return fail(2, errors.New("import has multiple MCPs; use --all or repeat --name"))
	}
	if len(items) > 1 && !all && len(selectedNames) == 0 && interactive(a.In) {
		fmt.Fprintln(a.Out, "MCPs found in config:")
		for i, item := range items {
			fmt.Fprintf(a.Out, "  %d) %s\n", i+1, item.Name)
		}
		choice := prompt(a, "MCPs to import (comma-separated numbers): ")
		var selected []model.MCPMetadata
		for _, raw := range strings.Split(choice, ",") {
			n, parseErr := strconv.Atoi(strings.TrimSpace(raw))
			if parseErr != nil || n < 1 || n > len(items) {
				return fail(2, errors.New("invalid MCP selection"))
			}
			selected = append(selected, items[n-1])
		}
		if len(selected) == 0 {
			return fail(2, errors.New("no MCP selected"))
		}
		items = selected
	}
	if len(selectedNames) > 0 {
		wanted := map[string]bool{}
		for _, name := range selectedNames {
			wanted[name] = true
		}
		var selected []model.MCPMetadata
		for _, m := range items {
			if wanted[m.Name] {
				selected = append(selected, m)
			}
		}
		if len(selected) != len(selectedNames) {
			return fail(6, errors.New("one or more requested MCP names were not found in the imported config"))
		}
		items = selected
	}
	for i, m := range items {
		m.Source = model.MCPSource{Type: model.MCPSourceImport, ImportedTarget: target}
		m.RegisteredAt = time.Now().UTC()
		registered, _, err := a.MCPs.Register(m)
		if err != nil {
			return fail(3, err)
		}
		items[i] = registered
	}
	if a.JSON {
		return jsonEncode(a.Out, items)
	}
	if !a.Quiet {
		fmt.Fprintf(a.Out, "imported %d MCP(s)\n", len(items))
	}
	_ = ad
	return nil
}

func preflightMCP(m model.MCPMetadata) error {
	for _, r := range m.Requirements {
		if r.Kind == "env" && r.Required && os.Getenv(r.Name) == "" {
			return MCPPreflightError{Code: "mcp_missing_env", MCP: m.Name, Variable: r.Name}
		}
		if r.Kind == "executable" && r.Required {
			if _, err := exec.LookPath(r.Name); err != nil {
				return MCPPreflightError{Code: "mcp_runtime_missing", MCP: m.Name, Runtime: r.Name}
			}
		}
	}
	checkTemplate := func(value string) error {
		for _, name := range envRefs(value) {
			if os.Getenv(name) == "" {
				return MCPPreflightError{Code: "mcp_missing_env", MCP: m.Name, Variable: name}
			}
		}
		return nil
	}
	if m.Command != nil {
		for _, value := range m.Command.Env {
			if err := checkTemplate(value); err != nil {
				return err
			}
		}
	}
	if m.Remote != nil {
		for _, value := range m.Remote.Headers {
			if err := checkTemplate(value); err != nil {
				return err
			}
		}
		if m.Remote.Auth.Mode == "bearer-env" {
			if os.Getenv(m.Remote.Auth.Env) == "" {
				return MCPPreflightError{Code: "mcp_missing_env", MCP: m.Name, Variable: m.Remote.Auth.Env}
			}
		}
	}
	if m.Transport == model.MCPTransportStdio && m.Command != nil {
		if _, err := exec.LookPath(m.Command.Command); err != nil {
			return MCPPreflightError{Code: "mcp_runtime_missing", MCP: m.Name, Runtime: m.Command.Command}
		}
	}
	return nil
}
func runtimeFor(kind, r string) string {
	if r != "" {
		return r
	}
	switch kind {
	case "npm":
		return "npx"
	case "pypi":
		return "uvx"
	default:
		return "docker"
	}
}
func packageName(v string) string {
	v = strings.TrimPrefix(v, "@")
	v = strings.ReplaceAll(v, "/", "-")
	return v
}

func suggestedMCPName(v string) string {
	if i := strings.LastIndexAny(v, "/:"); i >= 0 && i+1 < len(v) {
		v = v[i+1:]
	}
	v = strings.ToLower(v)
	var b strings.Builder
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-_")
	if name == "" {
		return "mcp"
	}
	return name
}
func variantNameSlice(v []providers.MCPVariant) []string {
	names := make([]string, 0, len(v))
	for _, x := range v {
		names = append(names, x.Variant)
	}
	return names
}
func splitKV(v string) (string, string, error) {
	p := strings.SplitN(v, "=", 2)
	if len(p) != 2 || p[0] == "" {
		return "", "", fmt.Errorf("expected NAME=value")
	}
	return p[0], p[1], nil
}
func envRefs(v string) []string {
	var out []string
	for {
		start := strings.Index(v, "${")
		if start < 0 {
			break
		}
		end := strings.Index(v[start+2:], "}")
		if end < 0 {
			break
		}
		name := v[start+2 : start+2+end]
		if !contains(out, name) {
			out = append(out, name)
		}
		v = v[start+3+end:]
	}
	return out
}
func appendUniqueRequirement(in []model.MCPRequirement, r model.MCPRequirement) []model.MCPRequirement {
	for _, x := range in {
		if x.Kind == r.Kind && x.Name == r.Name {
			return in
		}
	}
	return append(in, r)
}
func contains(ss []string, v string) bool {
	for _, x := range ss {
		if x == v {
			return true
		}
	}
	return false
}
func inferMCPTarget(path string) string {
	p := filepath.ToSlash(path)
	switch {
	case strings.HasSuffix(p, ".mcp.json"), strings.HasSuffix(p, ".claude.json"):
		return "claude"
	case strings.Contains(p, "mcp_config.json"):
		return "antigravity"
	case strings.Contains(p, ".cursor/"):
		return "cursor"
	case strings.HasSuffix(p, "opencode.json"):
		return "opencode"
	default:
		return "codex"
	}
}

func importMCPJSON(data []byte, target string) ([]model.MCPMetadata, error) {
	var root map[string]any
	if err := json.Unmarshal(stripJSONC(data), &root); err != nil {
		return nil, err
	}
	key := "mcpServers"
	if target == "opencode" {
		key = "mcp"
	}
	raw, ok := root[key].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s config has no MCP servers", target)
	}
	var out []model.MCPMetadata
	for name, v := range raw {
		obj, ok := v.(map[string]any)
		if !ok {
			continue
		}
		m := model.MCPMetadata{SchemaVersion: 1, Name: name, Transport: model.MCPTransportStdio}
		if typ, _ := obj["type"].(string); typ == "http" || typ == "remote" {
			m.Transport = model.MCPTransportHTTP
		}
		if cmd, ok := obj["command"].(string); ok {
			m.Command = &model.MCPCommand{Command: cmd}
			if arr, ok := obj["args"].([]any); ok {
				for _, x := range arr {
					if s, ok := x.(string); ok {
						m.Command.Args = append(m.Command.Args, s)
					}
				}
			}
		}
		if arr, ok := obj["command"].([]any); ok {
			m.Command = &model.MCPCommand{}
			for _, x := range arr {
				if s, ok := x.(string); ok {
					if m.Command.Command == "" {
						m.Command.Command = s
					} else {
						m.Command.Args = append(m.Command.Args, s)
					}
				}
			}
		}
		envKey := "env"
		if target == "opencode" {
			envKey = "environment"
		}
		if env, ok := obj[envKey].(map[string]any); ok {
			if m.Command == nil {
				m.Command = &model.MCPCommand{}
			}
			m.Command.Env = map[string]string{}
			for k, v := range env {
				sv, _ := v.(string)
				sv = canonicalImportedTemplate(sv, target)
				if len(envRefs(sv)) == 0 {
					return nil, fmt.Errorf("import of %q cannot safely persist environment value %q; convert it to an env template", name, k)
				}
				m.Command.Env[k] = sv
				for _, e := range envRefs(sv) {
					m.Requirements = appendUniqueRequirement(m.Requirements, model.MCPRequirement{Kind: "env", Name: e, Required: true, Secret: true})
				}
			}
		}
		if u, ok := obj["url"].(string); ok {
			m.Remote = &model.MCPRemote{URL: u}
			m.Transport = model.MCPTransportHTTP
		}
		if u, ok := obj["serverUrl"].(string); ok {
			m.Remote = &model.MCPRemote{URL: u}
			m.Transport = model.MCPTransportHTTP
		}
		if h, ok := obj["headers"].(map[string]any); ok {
			if m.Remote == nil {
				m.Remote = &model.MCPRemote{}
			}
			m.Remote.Headers = map[string]string{}
			for k, v := range h {
				sv, _ := v.(string)
				sv = canonicalImportedTemplate(sv, target)
				if len(envRefs(sv)) == 0 {
					return nil, fmt.Errorf("import of %q cannot safely persist header %q; convert it to an env template", name, k)
				}
				m.Remote.Headers[k] = sv
				for _, e := range envRefs(sv) {
					m.Requirements = appendUniqueRequirement(m.Requirements, model.MCPRequirement{Kind: "env", Name: e, Required: true, Secret: true})
				}
			}
		}
		if err := validateImported(m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func canonicalImportedTemplate(value, target string) string {
	switch target {
	case "cursor":
		return rewriteImportedEnv(value, "${env:", "}")
	case "opencode":
		return rewriteImportedEnv(value, "{env:", "}")
	default:
		return value
	}
}

func rewriteImportedEnv(value, prefix, suffix string) string {
	var out strings.Builder
	for {
		start := strings.Index(value, prefix)
		if start < 0 {
			out.WriteString(value)
			return out.String()
		}
		out.WriteString(value[:start])
		rest := value[start+len(prefix):]
		end := strings.Index(rest, suffix)
		if end < 0 {
			out.WriteString(value[start:])
			return out.String()
		}
		name := rest[:end]
		if model.ValidateMCPTemplate("${"+name+"}") != nil {
			out.WriteString(value[start : start+len(prefix)+end+len(suffix)])
		} else {
			out.WriteString("${" + name + "}")
		}
		value = rest[end+len(suffix):]
	}
}
func validateImported(m model.MCPMetadata) error {
	if m.Transport == model.MCPTransportStdio && (m.Command == nil || m.Command.Command == "") {
		return fmt.Errorf("imported MCP %q has no command", m.Name)
	}
	if err := model.ValidateMCPName(m.Name); err != nil {
		return err
	}
	return nil
}
func importMCPText(data []byte, target string) ([]model.MCPMetadata, error) {
	_ = target
	statements, err := logicalTOMLStatements(string(data))
	if err != nil {
		return nil, err
	}
	var out []model.MCPMetadata
	var current *model.MCPMetadata
	active := false
	for _, line := range statements {
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			active = false
			if !strings.HasPrefix(line, "[mcp_servers.") {
				continue
			}
			rawName := strings.TrimSuffix(strings.TrimPrefix(line, "[mcp_servers."), "]")
			if strings.Contains(rawName, ".") && !strings.HasPrefix(rawName, "\"") {
				continue
			}
			if current != nil {
				out = append(out, *current)
			}
			name := rawName
			if strings.HasPrefix(rawName, "\"") {
				name, err = parseImportedTOMLString(rawName)
				if err != nil {
					return nil, fmt.Errorf("invalid MCP table name: %w", err)
				}
			}
			current = &model.MCPMetadata{SchemaVersion: 1, Name: name, Transport: model.MCPTransportStdio, Command: &model.MCPCommand{}}
			active = true
			continue
		}
		if current == nil || !active {
			continue
		}
		p := strings.SplitN(line, "=", 2)
		if len(p) != 2 {
			continue
		}
		k := strings.TrimSpace(p[0])
		v := strings.TrimSpace(p[1])
		if k == "command" {
			current.Command.Command, err = parseImportedTOMLString(v)
			if err != nil {
				return nil, fmt.Errorf("import of %q command: %w", current.Name, err)
			}
		} else if k == "url" {
			v, err = parseImportedTOMLString(v)
			if err != nil {
				return nil, fmt.Errorf("import of %q URL: %w", current.Name, err)
			}
			current.Transport = model.MCPTransportHTTP
			current.Command = nil
			current.Remote = &model.MCPRemote{URL: v, Headers: map[string]string{}}
		} else if k == "bearer_token_env_var" {
			v, err = parseImportedTOMLString(v)
			if err != nil {
				return nil, fmt.Errorf("import of %q bearer token variable: %w", current.Name, err)
			}
			if current.Remote == nil {
				current.Remote = &model.MCPRemote{Headers: map[string]string{}}
			}
			current.Remote.Auth = model.MCPAuth{Mode: "bearer-env", Env: v}
			current.Requirements = append(current.Requirements, model.MCPRequirement{Kind: "env", Name: v, Required: true, Secret: true})
		} else if k == "http_headers" && current.Remote != nil {
			values, parseErr := parseTOMLStringMap(v)
			if parseErr != nil {
				return nil, fmt.Errorf("import of %q: %w", current.Name, parseErr)
			}
			if current.Remote.Headers == nil {
				current.Remote.Headers = map[string]string{}
			}
			for header, value := range values {
				if len(envRefs(value)) == 0 {
					return nil, fmt.Errorf("import of %q cannot safely persist header %q; convert it to an env template", current.Name, header)
				}
				current.Remote.Headers[header] = value
				for _, env := range envRefs(value) {
					current.Requirements = appendUniqueRequirement(current.Requirements, model.MCPRequirement{Kind: "env", Name: env, Required: true, Secret: true})
				}
			}
		} else if k == "env" && current.Command != nil {
			values, parseErr := parseTOMLStringMap(v)
			if parseErr != nil {
				return nil, fmt.Errorf("import of %q: %w", current.Name, parseErr)
			}
			if current.Command.Env == nil {
				current.Command.Env = map[string]string{}
			}
			for key, value := range values {
				if len(envRefs(value)) == 0 {
					return nil, fmt.Errorf("import of %q cannot safely persist environment value %q; convert it to an env template", current.Name, key)
				}
				current.Command.Env[key] = value
				for _, env := range envRefs(value) {
					current.Requirements = appendUniqueRequirement(current.Requirements, model.MCPRequirement{Kind: "env", Name: env, Required: true, Secret: true})
				}
			}
		} else if k == "args" && current.Command != nil {
			current.Command.Args, err = parseTOMLStringArray(v)
			if err != nil {
				return nil, fmt.Errorf("import of %q args: %w", current.Name, err)
			}
		}
	}
	if current != nil {
		out = append(out, *current)
	}
	for _, imported := range out {
		if err := validateImported(imported); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func logicalTOMLStatements(text string) ([]string, error) {
	var out []string
	var current strings.Builder
	depth := 0
	for _, physical := range strings.Split(text, "\n") {
		line, delta, err := scanTOMLLine(physical)
		if err != nil {
			return nil, err
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
		depth += delta
		if depth < 0 {
			return nil, errors.New("unbalanced TOML delimiters")
		}
		if depth == 0 {
			if statement := strings.TrimSpace(current.String()); statement != "" {
				out = append(out, statement)
			}
			current.Reset()
		}
	}
	if depth != 0 || current.Len() != 0 {
		return nil, errors.New("unterminated TOML array or inline table")
	}
	return out, nil
}

func scanTOMLLine(line string) (string, int, error) {
	inString, escaped, depth := false, false, 0
	for i := 0; i < len(line); i++ {
		switch {
		case inString && escaped:
			escaped = false
		case inString && line[i] == '\\':
			escaped = true
		case line[i] == '"':
			inString = !inString
		case !inString && line[i] == '#':
			return strings.TrimRight(line[:i], " \t"), depth, nil
		case !inString && (line[i] == '[' || line[i] == '{'):
			depth++
		case !inString && (line[i] == ']' || line[i] == '}'):
			depth--
		}
	}
	if inString {
		return "", 0, errors.New("unterminated TOML string")
	}
	return line, depth, nil
}

func parseTOMLStringMap(value string) (map[string]string, error) {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[0] != '{' || value[len(value)-1] != '}' {
		return nil, errors.New("expected an inline TOML string map")
	}
	value = strings.TrimSpace(value[1 : len(value)-1])
	out := map[string]string{}
	if value == "" {
		return out, nil
	}
	items, err := splitTOMLItems(value)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if strings.TrimSpace(item) == "" {
			continue
		}
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			return nil, errors.New("invalid inline TOML string map")
		}
		key, err := parseImportedTOMLString(parts[0])
		if err != nil {
			return nil, err
		}
		val, err := parseImportedTOMLString(parts[1])
		if err != nil {
			return nil, err
		}
		out[key] = val
	}
	return out, nil
}

func parseTOMLStringArray(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[0] != '[' || value[len(value)-1] != ']' {
		return nil, errors.New("expected a TOML string array")
	}
	items, err := splitTOMLItems(strings.TrimSpace(value[1 : len(value)-1]))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) == "" {
			continue
		}
		parsed, err := parseImportedTOMLString(item)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}

func splitTOMLItems(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var out []string
	start, nested := 0, 0
	inString, escaped := false, false
	for i := 0; i < len(value); i++ {
		switch {
		case inString && escaped:
			escaped = false
		case inString && value[i] == '\\':
			escaped = true
		case value[i] == '"':
			inString = !inString
		case !inString && (value[i] == '[' || value[i] == '{'):
			nested++
		case !inString && (value[i] == ']' || value[i] == '}'):
			nested--
		case !inString && nested == 0 && value[i] == ',':
			out = append(out, strings.TrimSpace(value[start:i]))
			start = i + 1
		}
	}
	if inString || nested != 0 {
		return nil, errors.New("invalid TOML value")
	}
	out = append(out, strings.TrimSpace(value[start:]))
	return out, nil
}

func parseImportedTOMLString(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", errors.New("inline TOML map values must be quoted strings")
	}
	value, err := strconv.Unquote(value)
	if err != nil {
		return "", err
	}
	return value, nil
}
func stripJSONC(data []byte) []byte {
	var out strings.Builder
	inString, escaped := false, false
	for i := 0; i < len(data); {
		c := data[i]
		if inString {
			out.WriteByte(c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			i++
			continue
		}
		if c == '"' {
			inString = true
			out.WriteByte(c)
			i++
			continue
		}
		if c == '/' && i+1 < len(data) && data[i+1] == '/' {
			i += 2
			for i < len(data) && data[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(data) && data[i+1] == '*' {
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				if data[i] == '\n' {
					out.WriteByte('\n')
				}
				i++
			}
			if i+1 < len(data) {
				i += 2
			}
			continue
		}
		out.WriteByte(c)
		i++
	}
	return stripTrailingJSONCommas([]byte(out.String()))
}
func stripTrailingJSONCommas(data []byte) []byte {
	var out strings.Builder
	inString, escaped := false, false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inString {
			out.WriteByte(c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out.WriteByte(c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(data) && (data[j] == ' ' || data[j] == '\n' || data[j] == '\r' || data[j] == '\t') {
				j++
			}
			if j < len(data) && (data[j] == '}' || data[j] == ']') {
				continue
			}
		}
		out.WriteByte(c)
	}
	return []byte(out.String())
}
