# forge-cli

AI agent pipeline scaffolding for [Claude Code](https://docs.anthropic.com/en/docs/claude-code). Turns any repository into an orchestrated, multi-agent workspace with decomposition, risk classification, parallel execution, tri-agent adversarial evaluation, and automated delivery.

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

This builds and installs `forge` to your `$GOPATH/bin`.

## Quick start

### New project from scratch

```bash
mkdir my-app && cd my-app && git init
forge init
```

The onboarding asks what language, framework, and project type you want, then generates everything.

### Existing project

```bash
cd my-existing-project
forge init
```

Forge auto-detects your stack (language, framework, test runner, linter) and generates a harness that matches.

### Projects with existing Claude Code configuration

Forge is designed to fit into repos that already have Claude Code set up. It won't clobber your work:

| File | Behavior on init |
|---|---|
| `CLAUDE.md` | **Merge** — appends forge section inside `<!-- forge:start -->` / `<!-- forge:end -->` delimiters. Your existing instructions stay untouched. Re-init updates only the forge section. |
| `.claude/settings.json` | **Merge** — adds forge permissions and hooks to your existing config. Your custom entries are preserved. |
| `forge.yaml` | **Skip if exists** — your customized commands, risk keywords, and evaluation weights are never overwritten. |
| `.forge/context/project.md` | **Skip if exists** — your project context is yours. |
| `.forge/agents/*` | **Overwrite** — forge-owned agent definitions, updated on every init. |
| `.forge/pipeline/*` | **Overwrite** — forge-owned pipeline scripts, updated on every init. |
| `.forge/hooks/*` | **Overwrite** — forge-owned hooks, updated on every init. |
| `.claude/skills/*/SKILL.md` | **Overwrite** — forge-owned skill prompts. Your own custom skills in other directories are never touched. |

Use `--force` to overwrite everything, including `forge.yaml` and `project.md`.

### From a spec document

```bash
mkdir my-app && cd my-app && git init
forge init --spec ~/Downloads/project-spec.pdf
```

Forge analyzes the spec with Claude, extracts project metadata (language, modules, architecture, constraints), and pre-fills everything. No manual onboarding needed.

### From multiple spec documents

```bash
mkdir my-app && cd my-app && git init
forge ingest \
  ~/Downloads/architecture.md \
  ~/Downloads/engineering-backlog.md \
  ~/Downloads/database-schema.md
forge init
```

Forge combines multiple documents into one spec, analyzes it, and sets up the harness.

## What it does

`forge init` scans your project, asks a few questions about what you're building, and generates a complete `.forge/` harness with:

- **Pipeline scripts** — a resumable state machine that routes work through intake, classification, decomposition, plan review, execution, verification, evaluation, and delivery
- **Specialist agents** — architect, backend, frontend, quality, security, and visual-qa agents (only the ones your project needs)
- **Tri-agent evaluation** — three independent evaluator agents (Edgar, Code Quality, Um-Actually) score every implementation before delivery, with few-shot calibrated scoring
- **Browser testing** — Playwright visual smoke tests at mobile + desktop viewports, running automatically when frontend files change
- **Risk classification** — T1/T2/T3 tiers that determine how much decomposition and verification a task gets
- **Task tracking** — work tracked via [`bd`](https://github.com/steveyegge/beads) (Dolt-backed issue tracker for AI agents)
- **Hooks** — pre-edit and post-edit hooks that enforce tracked work
- **Skills** — `/deliver`, `/ingest`, and `/skill-creator` commands for Claude Code

## Usage

After init, open Claude Code in your project:

### `/deliver` — execute tracked work

```
/deliver "Add JWT authentication with role-based access"
```

The pipeline:
1. **Intake** — parses and scores the work description for completeness
2. **Classify** — assigns risk tier (T1 low, T2 moderate, T3 critical)
3. **Decompose** — architect agent breaks complex work into parallel-safe subtasks
4. **Review plan** — independent reviewer validates the decomposition before execution
5. **Execute** — dispatches subtasks to specialist agents wave-by-wave
6. **Verify** — runs typecheck, lint, tests, anti-pattern checks, and Playwright browser smoke tests
7. **Evaluate** — three evaluator agents score the implementation independently (must reach 0.7 composite to pass, max 3 revision iterations)
8. **Deliver** — creates branch, commits, pushes, opens PR

### `/ingest` — decompose a spec into a project plan

```bash
# Add specs to your project
forge ingest ~/Downloads/platform-spec.pdf

# Or multiple documents at once
forge ingest architecture.md backlog.md schema.md

# Then in Claude Code:
/ingest spec-a1b2
```

Multi-pass analysis:
1. **Extract** — sections, requirements, constraints, data entities
2. **Map domains** — group into modules with dependency graph
3. **Decompose** — epics, features, atomic tasks
4. **Identify skills** — find repeated patterns worth automating

You review and refine the plan before any code is written. Then execute phase-by-phase through `/deliver`.

### `forge run` — auto-pilot execution

```bash
# Seed tasks from an approved spec
forge seed spec-a1b2

# Execute all tasks with parallel workers
forge run spec-a1b2 --concurrency 3

# Dry run to see the plan
forge run spec-a1b2 --dry-run

# Budget-limited execution
forge run spec-a1b2 --budget 50
```

Auto-pilot features:
- **Parallel workers** in isolated git worktrees
- **Circuit breaker** — pauses on repeated rate limits
- **Idle timeout** — kills hung tasks after configurable seconds
- **Phase gates** — optional human review between phases
- **Budget control** — per-task spend limits
- **Resumable** — interrupted runs pick up where they left off

### `/skill-creator` — generate custom skills

```
/skill-creator
```

Create new Claude Code skills for domain-specific workflows. The ingestion system can also auto-generate skills from patterns it finds in your spec.

## Commands

| Command | Description |
|---|---|
| `forge init` | Initialize harness in current project |
| `forge init --spec <file>` | Initialize from a spec document (PDF, markdown, text) |
| `forge ingest <files...>` | Add one or more spec documents for analysis |
| `forge seed <spec-id>` | Create beads tasks from an approved spec decomposition |
| `forge run <spec-id>` | Auto-pilot task execution with parallel workers |
| `forge run-status <spec-id>` | Check status of a running auto-pilot execution |
| `forge add <addon>` | Install an addon (compliance-hipaa, compliance-soc2) |
| `forge remove <addon>` | Remove an addon |
| `forge status` | Show harness status, agents, addons |
| `forge doctor` | Diagnose harness health |
| `forge upgrade` | Upgrade harness files to latest version |

## Tri-agent evaluation

Every implementation is reviewed by three independent evaluator agents before delivery. This follows Anthropic's [generator-evaluator separation](https://www.anthropic.com/engineering/harness-design-long-running-apps) principle — the agents that write code never evaluate their own output.

| Evaluator | Weight | Focus |
|---|---|---|
| **Edgar** | 35% | Adversarial edge cases — robustness, error handling, security surface, brittleness |
| **Code Quality** | 35% | Architecture fit, maintainability, performance, correctness beyond tests |
| **Um-Actually** | 30% | API correctness, framework conventions, documentation alignment, upgrade safety |

Each evaluator scores four dimensions from 0.0 to 1.0. The composite score must reach **0.7** to pass. If it doesn't, a revision brief is generated and the pipeline loops back to execution — up to **3 iterations** before failing.

All evaluator prompts include **few-shot calibration examples** with full score breakdowns, ensuring consistent and well-anchored judgment across runs.

## Browser testing

Playwright visual smoke tests run automatically during verification when frontend files are modified. No addon required — browser testing is built into the core pipeline.

- **Mobile viewport**: 375x812px
- **Desktop viewport**: 1440x900px
- Tests affected routes based on git diff
- Checks: HTTP status, horizontal overflow (mobile), screenshot capture
- Results written to `.forge/state/screenshots/results.json`
- Screenshots fed to evaluators as runtime evidence

Evaluators receive browser test results alongside the code diff, so they can assess visual regression, layout issues, and responsive behavior — not just static code quality.

## What gets generated

```
my-project/
├── forge.yaml                          # Main configuration
├── CLAUDE.md                           # Agent instructions
├── .claude/
│   ├── settings.json                   # Permissions and hooks
│   └── skills/
│       ├── deliver/SKILL.md            # /deliver command
│       ├── ingest/SKILL.md             # /ingest command
│       └── skill-creator/SKILL.md      # /skill-creator command
└── .forge/
    ├── pipeline/                       # State machine scripts
    │   ├── orchestrator.sh
    │   ├── intake.sh
    │   ├── classify.md
    │   ├── decompose.md
    │   ├── review-plan.md
    │   ├── execute.md
    │   ├── verify.sh
    │   ├── evaluate.md
    │   ├── browser-smoke.sh
    │   └── deliver.sh
    ├── agents/                         # Specialist agent definitions
    │   ├── architect.md
    │   ├── backend.md                  # (if project needs backend)
    │   ├── frontend.md                 # (if project needs frontend)
    │   ├── quality.md
    │   ├── security.md
    │   ├── visual-qa.md
    │   ├── edgar.md                    # Evaluator: edge cases
    │   ├── code-quality.md             # Evaluator: architecture
    │   └── um-actually.md              # Evaluator: best practices
    ├── context/                        # Project knowledge
    │   ├── stack.md                    # Tech stack conventions
    │   └── project.md                  # Your project context
    ├── hooks/                          # Claude Code lifecycle hooks
    │   ├── pre-edit.sh
    │   ├── post-edit.sh
    │   └── session-start.sh
    ├── specs/                          # Ingested spec documents
    ├── addons/                         # Installed addon files
    └── state/                          # Screenshots, transient state
.beads/                                 # bd task tracking (Dolt database)
```

## Supported stacks

Forge auto-detects and has presets for:

- **TypeScript/JavaScript** — Next.js, SvelteKit
- **Python** — FastAPI, Django, Flask
- **Go** — Gin, Chi, Fiber

Works with any project regardless of stack — presets just provide stack-specific conventions and verification commands.

## Addons

```bash
forge add compliance-hipaa     # HIPAA security checks
forge add compliance-soc2      # SOC2 compliance verification
```

## Requirements

- Git
- [`bd`](https://github.com/steveyegge/beads) (task tracking — `brew install beads`)
- `jq` (JSON processing in pipeline scripts)
- `gh` CLI (PR creation)
- [Claude Code](https://docs.anthropic.com/en/docs/claude-code) (to run the generated harness)
- Node.js 18+ and Playwright (auto-installed for browser testing)

## Development

```bash
# Build
make build

# Install locally
make install

# Run tests
make test

# Run linter
make vet
```

Releases are automated via [GoReleaser](https://goreleaser.com/) — push a version tag (`git tag v0.3.0 && git push --tags`) to trigger a GitHub release with cross-platform binaries and Homebrew tap update.

## License

MIT
