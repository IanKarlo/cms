<p align="center">
  <img src="assets/mascot.png" alt="CMS maestro gopher mascot" width="320">
</p>

# CMS — Context Management System

CMS is a command-line tool for organizing Agent Skills and MCP servers into
reusable contexts. A context can then be applied to a project or made
available globally to your coding harnesses.

## What CMS does

CMS keeps a central library of skills and MCP definitions, lets you group them
into named contexts, and exposes only the selected resources to each harness.
This makes it easy to switch between focused setups such as `backend`,
`frontend`, or `infrastructure` without copying resources between projects.

CMS supports these harness targets:

- Codex
- Claude Code
- Antigravity
- Cursor
- OpenCode

## How it works

1. Install skills or register MCPs in the CMS library.
2. Create a context containing the resources you want to use together.
3. Apply the context to the current project or to your global environment.
4. CMS creates the links and harness entries required by the selected targets.

CMS manages only the resources it owns. Existing files, links, and harness
entries created outside CMS are preserved.

## Quick start

Install a skill, create a context, and apply it to the current project:

```bash
cms skill install https://github.com/example/skills/tree/main/skills/code-review
cms skill list
cms context new --name backend --skill code-review@23a91cc8
cms project init backend --target codex
```

The context is now available to the Codex harness in the current project.

## Commands

Running a module without an action shows its available actions and a short
description:

```bash
cms skill
cms mcp
cms context
cms project
```

### General help

```bash
cms help
```

Shows all modules, actions, syntax, and descriptions.

Global flags can be used with commands that support them:

- `--json` returns machine-readable output.
- `--quiet` suppresses normal progress output.
- `--verbose` shows additional operational details.
- `--no-color` disables terminal colors.

### Skills

Skills are reusable instruction packages containing a `SKILL.md` file.

```bash
# Install one skill from GitHub.
cms skill install https://github.com/org/repo/tree/main/skills/testing

# Install every valid skill found at the repository's skills directory.
cms skill install https://github.com/org/repo

# Search for a skill interactively.
cms skill install

# List installed skills.
cms skill list
cms skill list --plain
cms skill list --json

# Remove selected skills.
cms skill remove testing@23a91cc8

# Remove all installed skills.
cms skill remove --all
```

When a skill ID is required, use the ID shown by `cms skill list`. The `--yes`
flag skips confirmation for removal in scripts.

### MCPs

MCPs are registered in the CMS library and are added to a harness only when
they belong to the active context. Registering an MCP does not install a
package, start a process, connect to a remote endpoint, or call `tools/list`.

MCPs can come from the Official MCP Registry, a remote URL, a package, or a
local command:

```bash
# Search the Official MCP Registry interactively.
cms mcp install

# Register a specific registry entry.
cms mcp install registry:io.github.example/context7 --version 1.2.3

# Register a remote MCP endpoint.
cms mcp install https://api.example.com/mcp \
  --name company-api \
  --bearer-env COMPANY_API_TOKEN

# Register an npm package without installing it locally.
cms mcp install npm:@vendor/database-mcp \
  --version 4.1.0 \
  --name database

# Register a local command with separate arguments.
cms mcp install --name dev-server \
  --command go \
  --arg run \
  --arg ./cmd/mcp
```

Useful MCP actions:

```bash
# Browse registered MCPs interactively.
cms mcp list

# Use non-interactive output.
cms mcp list --plain
cms mcp list --json

# Remove registered MCPs that are not in use.
cms mcp remove <mcp-id>

# Import MCP definitions from a harness file.
cms mcp import .mcp.json --target claude --all
cms mcp import .cursor/mcp.json --all
```

The plain list includes display name, canonical ID, transport, and source so
entries with the same raw name can still be distinguished.

### Contexts

Contexts group skills and MCPs into a reusable setup.

```bash
# Create a context from installed resources.
cms context new \
  --name backend \
  --description "Backend development" \
  --skill code-review@23a91cc8 \
  --mcp database@41e90ac3

# Edit an existing context.
cms context edit backend \
  --description "Backend and data services" \
  --skill code-review@23a91cc8 \
  --mcp database@41e90ac3

# Browse contexts interactively.
cms context list

# Use non-interactive output.
cms context list --plain
cms context list --json
```

The context editor has separate pages for its name and description, skills,
and MCPs. MCP entries can also include an alias and tool allow/deny filters
when supported by the target harness.

### Project actions

Project actions operate on the current repository.

```bash
# Apply a context to the current project.
cms project init backend --target codex --target cursor

# Save the selected context as the project's reproducible manifest.
cms project freeze backend

# Restore resources described by the project manifest.
cms project sync

# Preview the cleanup operation.
cms project clear --dry-run

# Remove CMS-managed project links, MCP entries, and project state.
cms project clear --yes
```

`cms project freeze` creates `cms.toml`, which records the context and pinned
resource sources for the project. `cms project sync` uses that manifest to
restore missing resources on another machine. The manifest can be committed
with the project so the same context can be reproduced by the team.

`cms project clear` asks for confirmation because it removes the project's
CMS-managed state. It removes only links and harness entries tracked by CMS;
files and directories created independently are preserved.

### Global actions

Global actions apply a reserved `global` context to the user's harnesses,
outside any individual project.

```bash
# Create or update the global context.
cms global init \
  --skill code-review@23a91cc8 \
  --mcp database@41e90ac3 \
  --target codex

# Preview global changes.
cms global init --dry-run

# Remove the global context and its CMS-managed links.
cms global remove --yes
```

The global context is independent from project contexts. It is not added
automatically to every project manifest.

### Default targets

The `config` module controls which harness targets are used when a command does
not specify `--target`:

```bash
# Show the current target selection.
cms config show
cms config list
cms config get default-targets

# Use only selected targets by default.
cms config set default-targets codex,claude

# Consider all supported targets again.
cms config unset default-targets
```

An explicit `--target` always takes precedence for that command.

### Shell completion

Generate a completion script for your shell:

```bash
cms shell completion bash
cms shell completion zsh
cms shell completion fish
cms shell completion powershell
```

To enable Bash completion, append the generated script to `~/.bashrc`:

```bash
cms shell completion bash >> ~/.bashrc
source ~/.bashrc
```

The completion suggests modules, actions, and context names. For example,
`./cms sk<Tab>` completes to `./cms skill`.

## Interactive navigation

List and search commands automatically use an interactive terminal interface
when available.

- Use the arrow keys or `j`/`k` to move through a list.
- Press `/` to start or edit a filter.
- Press `Enter` to browse or select.
- Press `Esc` to leave the screen.

The list screens use a compact moving window so large registries remain easy
to navigate in a terminal. Use `--plain` or `--json` for scripts and other
non-interactive workflows.

## Typical workflows

### Set up a project

```bash
cms skill install https://github.com/org/skills/tree/main/skills/backend-review
cms mcp install npm:@vendor/database-mcp --version 4.1.0 --name database
cms context new --name backend \
  --skill backend-review@23a91cc8 \
  --mcp database@41e90ac3
cms project init backend --target codex
cms project freeze backend
```

### Reproduce a project elsewhere

From a project containing `cms.toml`:

```bash
cms project sync
cms project init
```

CMS restores missing pinned resources, recreates the context when necessary,
and applies the project's links and MCP entries to the selected harnesses.

### Keep a global setup

```bash
cms global init --skill general-review@23a91cc8 --target codex
```

Project contexts can still be used independently alongside the global setup.
