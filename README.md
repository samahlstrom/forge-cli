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
make install
```

## Quick start

```bash
forge setup       # One-time: creates your personal toolkit at ~/.forge/
forge init        # Per-project: symlinks skills into .claude/skills/
forge list        # See your agents and skills
```

`forge setup` creates `~/.forge/` with starter agents, skills, and pipeline scripts — all as a local git repo. Run `forge init` in any project to wire your skills in as Claude Code slash commands.

### Sync across machines

Your toolkit is a git repo. Add a remote to take it anywhere:

```bash
cd ~/.forge
git remote add origin <your-repo-url>
git push -u origin main
```

On another machine:

```bash
git clone <your-repo-url> ~/.forge
```

Or if you already ran `forge setup` there, use `forge sync` to pull updates.

## Commands

| Command | Description |
|---|---|
| `forge setup` | One-time — creates your toolkit at `~/.forge/` |
| `forge init` | Per-project — symlinks skills into `.claude/skills/` |
| `forge sync` | Pull the latest from your toolkit's remote |
| `forge list` | List all agents and skills in your toolkit |
| `forge agent list` | List agents |
| `forge agent show <name>` | Print an agent's full definition |
| `forge agent add <name>` | Create a new agent |
| `forge agent edit <name>` | Open an agent in your `$EDITOR` |
| `forge skill list` | List skills |
| `forge skill show <name>` | Print a skill's full definition |
| `forge skill add <name>` | Create a new skill |
| `forge skill edit <name>` | Open a skill in your `$EDITOR` |
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
5. **Execute** — dispatches subtasks to specialist agents
6. **Verify** — runs typecheck, lint, tests, and browser smoke tests
7. **Evaluate** — three evaluator agents score the implementation (must reach 0.7 composite to pass)
8. **Deliver** — creates branch, commits, pushes, opens PR

### The `/ingest` skill

Decomposes spec documents (PDFs, markdown, text) into structured project plans with epics, features, and atomic tasks.

### Adding your own tools

```bash
# Add a new agent
forge agent add my-agent --body '# my-agent\n\nDoes something useful.'

# Add a new skill
forge skill add my-skill --body '---\nname: my-skill\n---\n\n# my-skill\n\nDoes something useful.'

# Both are committed to your toolkit repo automatically
```

## Toolkit structure

```
~/.forge/                  # A git repo — your personal toolkit
├── agents/                # Agent definitions (.md)
│   ├── architect.md
│   ├── backend.md
│   ├── frontend.md
│   ├── quality.md
│   ├── security.md
│   └── ...
├── skills/                # Skill definitions (SKILL.md per skill)
│   ├── forge/SKILL.md
│   ├── ingest/SKILL.md
│   └── skill-creator/SKILL.md
└── pipeline/              # Pipeline scripts and prompts
    ├── intake.sh
    ├── classify.md
    ├── verify.sh
    └── deliver.sh
```

## Requirements

- Git
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
