# forge-cli

AI agent pipeline scaffolding for [Claude Code](https://docs.anthropic.com/en/docs/claude-code). Turns any repository into an orchestrated, multi-agent workspace with decomposition, risk classification, parallel execution, and automated delivery.

## Install

```bash
npx forge-cli init
```

That's it. One command in any project directory.

## What it does

`forge init` scans your project, asks a few questions about what you're building, and generates a complete `.forge/` harness with:

- **Pipeline scripts** — a state machine that routes work through intake, classification, decomposition, execution, verification, and delivery
- **Specialist agents** — architect, backend, frontend, quality, and security agents (only the ones your project needs)
- **Risk classification** — T1/T2/T3 tiers that determine how much decomposition and verification a task gets
- **Bead tracking** — every unit of work is tracked with file locking, checkpoints, and audit trails
- **Hooks** — pre-edit and post-edit hooks that enforce tracked work and log modifications
- **Skills** — `/deliver`, `/ingest`, and `/skill-creator` commands for Claude Code

## Quick start

### New project from scratch

```bash
mkdir my-app && cd my-app && git init
npx forge-cli init
```

The onboarding asks what language, framework, and project type you want, then generates everything.

### Existing project

```bash
cd my-existing-project
npx forge-cli init
```

Forge auto-detects your stack (language, framework, test runner, linter) and generates a harness that matches.

### From a spec document

```bash
mkdir my-app && cd my-app && git init
npx forge-cli init --spec ~/Downloads/project-spec.pdf
```

Forge analyzes the spec with Claude, extracts project metadata (language, modules, architecture, constraints), and pre-fills everything. No manual onboarding needed.

## Usage

After init, open Claude Code in your project:

### `/deliver` — execute tracked work

```
/deliver "Add JWT authentication with role-based access"
```

The pipeline:
1. **Intake** — parses and scores the work description
2. **Classify** — assigns risk tier (T1 low, T2 moderate, T3 critical)
3. **Decompose** — breaks complex work into parallel-safe subtasks
4. **Execute** — dispatches subtasks to specialist agents
5. **Verify** — runs typecheck, lint, tests, anti-pattern checks
6. **Deliver** — creates branch, commits, pushes, opens PR

### `/ingest` — decompose a spec into a project plan

```bash
# Add a spec to your project
npx forge-cli ingest ~/Downloads/platform-spec.pdf

# Then in Claude Code:
/ingest spec-a1b2
```

Multi-pass analysis:
1. **Extract** — sections, requirements, constraints, data entities
2. **Map domains** — group into modules with dependency graph
3. **Decompose** — epics, features, atomic tasks
4. **Identify skills** — find repeated patterns worth automating

You review and refine the plan before any code is written. Then execute phase-by-phase through `/deliver`.

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
| `forge ingest <file>` | Add a spec to an existing project for analysis |
| `forge add <addon>` | Install an addon (browser-testing, compliance-hipaa, compliance-soc2) |
| `forge remove <addon>` | Remove an addon |
| `forge status` | Show harness status, agents, addons |
| `forge doctor` | Diagnose harness health |
| `forge upgrade` | Upgrade harness files to latest version |

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
    │   ├── classify.sh
    │   ├── decompose.md
    │   ├── execute.md
    │   ├── verify.sh
    │   └── deliver.sh
    ├── agents/                         # Specialist agent definitions
    │   ├── architect.md
    │   ├── backend.md                  # (if project needs it)
    │   ├── frontend.md                 # (if project needs it)
    │   ├── quality.md
    │   └── security.md
    ├── context/                        # Project knowledge
    │   ├── stack.md                    # Tech stack conventions
    │   └── project.md                  # Your project context
    ├── hooks/                          # Claude Code lifecycle hooks
    ├── specs/                          # Ingested spec documents
    └── addons/                         # Installed addon files
.beads/                                 # bd task tracking (Dolt database)
```

## Supported stacks

Forge auto-detects and has presets for:

- **TypeScript/JavaScript** — Next.js, SvelteKit
- **Python** — FastAPI, Django, Flask
- **Go** — Gin, Chi, Fiber

Works with any project regardless of stack — presets just provide stack-specific conventions.

## Addons

```bash
forge add browser-testing      # Playwright visual QA
forge add compliance-hipaa     # HIPAA security checks
forge add compliance-soc2      # SOC2 compliance verification
```

## Requirements

- Node.js 18+
- Git
- [`bd`](https://github.com/steveyegge/beads) (task tracking — `brew install beads`)
- `jq` (for JSON processing in pipeline scripts)
- `gh` CLI (for PR creation)
- Claude Code (to run the generated harness)

## License

MIT
