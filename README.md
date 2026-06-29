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

2. **`forge init`** (run per-project) does four things:
   - Symlinks your skills into `.claude/skills/` so they work as slash commands
   - Adds a forge reference to `CLAUDE.md` so Claude knows the toolkit exists
   - Writes `.claude/skills/.gitignore` so symlinks aren't committed (they're machine-specific)
   - Installs the toolkit's **default hooks** from the [hooks manifest](#hooks) — e.g. a git `pre-push` validation gate that travels with the repo

   Use **`forge init --global`** to install skills into `~/.claude/skills/` instead — this makes them available in **every** Claude Code session across all interfaces (CLI, Desktop app, VS Code, JetBrains) without per-project setup.

3. **`forge sync`** pulls and pushes your toolkit to/from GitHub, then re-wires any new skills **and refreshes the default hooks** into the current project.

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
| `forge init` | Per-project — wires skills into `.claude/skills/` and `CLAUDE.md`, installs default hooks |
| `forge init --enable-hook <name>` | Also install an opt-in (default:false) hook, e.g. `validate-gate` (repeatable) |
| `forge init --global` | User-wide — wires skills into `~/.claude/skills/` for all sessions |
| `forge sync` | Pull + push toolkit, re-wire new skills + refresh default hooks into current project |
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
| `.githooks/pre-push` | Portable git pre-push validation gate (resolves the personal gate at runtime) | Yes |
| `.claude/settings.json` (hooks) | Only with `--enable-hook` — deep-merged opt-in hooks | Yes |

Project-specific skills (real files, not symlinks) are never overwritten and are always committed normally.

## Hooks

Skills are not the only thing forge installs. The same `forge init` / `forge sync`
also installs **hooks** declared in a manifest, so your workflow's guardrails
travel with you to any repo on any machine — not just your slash commands.

### The manifest

`~/.forge/hooks/manifest.json` is the single source of truth. The installer walks
it generically (switching on `kind`, never on a hook's name), so adding a hook is a
manifest edit, not a code change:

```json
{
  "hooks": [
    {"name": "pre-push-validate", "kind": "git-hook", "gitHook": "pre-push",
     "script": "pre-push-validate.sh", "scope": "repo", "default": true},
    {"name": "validate-gate", "kind": "claude-settings-hook", "event": "PreToolUse",
     "matcher": "Bash", "script": "validate-gate.sh", "scope": "repo", "default": false,
     "note": "leaky command string-match; opt-in only; never global"}
  ],
  "scripts": []
}
```

- `default: true` hooks install automatically on `forge init`.
- `default: false` hooks are opt-in: install with `forge init --enable-hook <name>`.
- The scripts themselves live in `~/.forge/hooks/` (installed by `forge setup`) and
  are referenced — never copied per repo — so one place stays the source of truth.

### The pre-push validation gate (primary)

`pre-push-validate.sh` is installed as a git **`pre-push`** hook. It blocks
`git push` of a branch whose **validation receipt** is missing or names a different
branch — your guarantee that UI work was actually validated before it leaves your
machine.

- **Receipt handshake.** The `/validate` skill writes `<repo>/.claude/.validate-receipt`
  at its Definition of Done. Line 1 is pipe-delimited; field 1 is the branch:
  `<branch> | validated | <Linear id> | shots=<N> | <comment URL> | <UTC time>`.
  The gate reads the **actual refs** git is pushing (on stdin), so — unlike a
  command string-match — it never false-blocks a message or script that merely
  contains the words "git push". Tag pushes and branch deletions pass untouched.
- **Portable, no hardcoded paths.** `forge init` writes a committed `.githooks/pre-push`
  wrapper and sets `core.hooksPath` to `.githooks` (relative, so it resolves per
  worktree and travels with the code). The wrapper resolves the personal gate at
  **runtime** (`$FORGE_HOME` or `$HOME/.forge`), so the same committed file works on
  every machine. A teammate without forge installed gets a harmless no-op.
- **Reaches every worktree.** `core.hooksPath` lives in the shared `.git` config, so
  one install covers all worktrees of the repo.
- **Never clobbers.** If you already use a `pre-push` hook, it's preserved as
  `pre-push.local` and chained ahead of the gate. An already-set `core.hooksPath` is
  respected (installed into, not overridden). Re-runs are idempotent (a
  `# forge-managed:` sentinel marks our wrapper).

### The PreToolUse validate-gate (opt-in, off by default)

`validate-gate.sh` is a Claude `PreToolUse(Bash)` hook that hard-blocks `gh pr create`
until the same receipt exists. It is **opt-in and off by default** because its command
string-match is inherently leaky — it can false-block any Bash command containing
`gh pr create` at a boundary. So it is **never** installed globally or into a
commander session; only into a per-repo committed `.claude/settings.json` when you ask
for it with `--enable-hook validate-gate`. Prefer the pre-push gate; reach for this
only when you want the earlier, PR-time block too.

### settings.json deep-merge (scope & merge guarantees)

Claude-settings hooks are **deep-merged** into the committed `.claude/settings.json`
(not `settings.local.json`, so the hook reaches worktrees and teammates):

- Reads the existing file (or `{}`), preserves **every** other key — `permissions`,
  `model`, unrelated matchers — and pretty-prints the result back.
- Finds or creates the entry for the hook's `matcher` and appends the command **only
  if absent**, so re-running never duplicates an entry (idempotent).
- The forge binary performs this write, which also bypasses Claude's auto-mode
  classifier that blocks agent edits to settings.

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
│   ├── skill-creator/SKILL.md
│   └── validate/SKILL.md
├── hooks/                 # Hooks installed by forge init (see "Hooks")
│   ├── manifest.json      # Declares which hooks install, and how
│   ├── pre-push-validate.sh   # git pre-push gate (default)
│   └── validate-gate.sh   # PreToolUse(Bash) gate (opt-in, off by default)
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
