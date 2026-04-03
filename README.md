# forge-cli

Portable AI agent toolkit for [Claude Code](https://docs.anthropic.com/en/docs/claude-code). A personal library of agents, skills, and pipeline scripts you take from project to project — zero footprint in your repos.

Built in Go. Cross-platform. Single binary.

## Install

### Homebrew (macOS / Linux)

```bash
brew install samahlstrom/tap/forge
```

> **Note:** Always use the full tap path (`samahlstrom/tap/forge`). There is an unrelated tool also called `forge` in Homebrew core — `brew install forge` will install the wrong thing.

### Go install

```bash
go install github.com/samahlstrom/forge-cli@latest
```

Requires Go 1.22+.

### Download binary

Pre-built binaries for macOS, Linux, and Windows (amd64/arm64) are available on the [GitHub Releases](https://github.com/samahlstrom/forge-cli/releases) page.

### Build from source

```bash
git clone https://github.com/samahlstrom/forge-cli.git
cd forge-cli
make build
# Binary is at bin/forge — move it somewhere on your PATH
```

## Quick start

```bash
forge setup       # One-time: creates toolkit, private GitHub repo, pushes
forge init        # Per-project: wires skills into .claude/skills/ + CLAUDE.md
forge list        # See your agents and skills
```

### What happens

1. **`forge setup`** creates `~/.forge/` with starter agents, skills, and pipeline scripts. It detects your GitHub user via `gh`, creates a **private** `<you>/forge-toolkit` repo, and pushes — so your toolkit is backed up and portable from the start.

2. **`forge init`** (run per-project) does three things:
   - Symlinks your skills into `.claude/skills/` so they work as slash commands
   - Adds a forge reference to `CLAUDE.md` so Claude knows the toolkit exists
   - Writes `.claude/skills/.gitignore` so symlinks aren't committed (they're machine-specific)

   Use **`forge init --global`** to install skills into `~/.claude/skills/` instead — this makes them available in **every** Claude Code session across all interfaces (CLI, Desktop app, VS Code, JetBrains) without per-project setup.

3. **`forge sync`** pulls and pushes your toolkit to/from GitHub, then re-wires any new skills into the current project.

### Cross-machine sync

Your toolkit syncs automatically via GitHub. No manual git needed.

```bash
# First machine — forge setup already created the repo and pushed
forge skill add my-skill    # Auto-commits and pushes to your forge-toolkit repo

# Second machine — forge setup detects your existing repo and clones it
forge setup                 # Finds <you>/forge-toolkit on GitHub, clones to ~/.forge/
forge init                  # Wire skills into the current project
```

Requires [GitHub CLI](https://cli.github.com/) (`gh`) authenticated via `gh auth login`.

## Commands

| Command | Description |
|---|---|
| `forge setup` | One-time — creates toolkit at `~/.forge/`, creates private GitHub repo |
| `forge init` | Per-project — wires skills into `.claude/skills/` and `CLAUDE.md` |
| `forge init --global` | User-wide — wires skills into `~/.claude/skills/` for all sessions |
| `forge sync` | Pull + push toolkit, re-wire new skills into current project |
| `forge get <repo> [name]` | Pull a skill or agent from any repo into your toolkit |
| `forge list` | List all agents and skills |
| `forge skill list` | List skills |
| `forge skill show <name>` | Print a skill's content |
| `forge skill add <name>` | Create a new skill (auto-commits, pushes, wires) |
| `forge skill edit <name>` | Open a skill in your `$EDITOR` |
| `forge skill remove <name>` | Remove a skill (auto-commits, pushes, unwires) |
| `forge agent list` | List agents |
| `forge agent show <name>` | Print an agent's definition |
| `forge agent add <name>` | Create a new agent (auto-commits, pushes) |
| `forge agent edit <name>` | Open an agent in your `$EDITOR` |
| `forge agent remove <name>` | Remove an agent (auto-commits, pushes) |
| `forge paths` | Print resolved toolkit paths as JSON |

## How it works

Forge manages a personal library of markdown-based tools that Claude Code uses at runtime:

- **Agents** (`~/.forge/agents/`) — specialist agent definitions (architect, backend, frontend, quality, security, evaluators, etc.) that the pipeline dispatches as subagents
- **Skills** (`~/.forge/skills/`) — Claude Code slash commands (`/forge`, `/ingest`, `/skill-creator`) that orchestrate multi-step workflows
- **Pipeline** (`~/.forge/pipeline/`) — shell scripts and prompt templates used by the `/forge` skill to run intake, classification, verification, and delivery

### The `/forge` skill

The main workflow. When you run `/forge "Add JWT authentication"` in Claude Code, it orchestrates an autonomous pipeline:

1. **Intake** — parses and scores the work description
2. **Classify** — assigns a risk tier (T1/T2/T3)
3. **Decompose** — architect agent breaks work into parallel-safe subtasks
4. **Review plan** — independent reviewer validates the decomposition
5. **Execute** — dispatches subtasks to specialist agents in isolated worktrees
6. **Verify** — runs typecheck, lint, tests, and browser smoke tests
7. **Evaluate** — three evaluator agents score the implementation (must reach 0.7 composite to pass)
8. **Deliver** — creates branch, commits, pushes, opens PR

### The `/ingest` skill

Decomposes spec documents (PDFs, markdown, text) into structured project plans with epics, features, and atomic tasks.

### Pulling from the ecosystem

```bash
# Browse what's available in any repo
forge get anthropics/skills

# Pull a skill into your toolkit
forge get anthropics/skills pdf

# Pull from any GitHub repo
forge get someone/their-toolkit code-review

# Pull an agent
forge get someone/their-toolkit debugger --agent
```

Works with any repo that has `skills/` or `agents/` directories — the same format used by [Anthropic's skills repo](https://github.com/anthropics/skills).

### Adding and removing tools

```bash
# Skills — auto-commit, push to GitHub, wire into current project
forge skill add my-skill --body '---\nname: my-skill\n---\n\n# my-skill'
forge skill remove my-skill

# Agents — auto-commit, push to GitHub
forge agent add my-agent --body '# my-agent\n\nDoes something useful.'
forge agent remove my-agent
```

### What forge init does to your project

`forge init` adds three things (all safe to commit):

| File | Purpose | Committed? |
|---|---|---|
| `.claude/skills/<name>/SKILL.md` | Symlinks to `~/.forge/` | No (gitignored) |
| `.claude/skills/.gitignore` | Ignores symlinks, allows project-specific skills | Yes |
| `CLAUDE.md` (forge section) | Tells Claude the toolkit exists and how to use it | Yes |

Project-specific skills (real files, not symlinks) are never overwritten and are always committed normally.

## Toolkit structure

```
~/.forge/                  # A git repo — your personal toolkit
├── agents/                # Agent definitions (.md)
│   ├── architect.md
│   ├── backend.md
│   ├── code-quality.md
│   ├── edgar.md
│   ├── frontend.md
│   ├── quality.md
│   ├── security.md
│   ├── um-actually.md
│   └── visual-qa.md
├── skills/                # Skill definitions (SKILL.md per skill)
│   ├── evaluate/SKILL.md
│   ├── forge/SKILL.md
│   ├── ingest/SKILL.md
│   └── skill-creator/SKILL.md
└── pipeline/              # Pipeline scripts and prompts
    ├── intake.sh
    ├── classify.md
    ├── helpers.sh
    ├── review-plan.md
    ├── verify.sh
    ├── browser-smoke.sh
    └── deliver.sh
```

## Requirements

- Git
- [GitHub CLI](https://cli.github.com/) (`gh`) — for automatic repo creation and sync
- [Claude Code](https://docs.anthropic.com/en/docs/claude-code)

## Development

```bash
make setup    # Configure git hooks
make build    # Build binary
make test     # Run tests
make vet      # Run linter
make ship     # Push, release via GitHub Actions, brew upgrade
```

Releases are automated via [GoReleaser](https://goreleaser.com/) — the post-commit hook auto-tags, and `make ship` pushes the tag to trigger a GitHub release with cross-platform binaries and Homebrew tap update.

## License

MIT
