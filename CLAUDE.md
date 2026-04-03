# CLAUDE.md

## Project

- **Name**: forge-cli
- **Language**: Go
- **Architecture**: monolith CLI tool

## Build & Test

- **Build**: `go build ./...`
- **Typecheck**: `go vet ./...`
- **Lint**: `golangci-lint run`
- **Test**: `go test ./...`

## Anti-Patterns (Blockers)

Do NOT introduce these:
- `TODO` / `FIXME` comments
- Hardcoded secrets or API keys

## Task Tracking (bd)

- Work is tracked via `bd` (steveyegge/beads) — a Dolt-backed issue tracker
- `bd ready` — see tasks with no open blockers
- `bd update <id> --claim` — atomically claim a task
- `bd close <id> --reason "..."` — mark work complete
- `bd update <id> --add-label "key:value"` — add labels (NO `-l` shorthand for update)
- `bd ready -l "key:value"` — filter by labels (query commands DO support `-l`)

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

## Releasing

After committing, use `make ship` to push, release, and update the local binary:

```bash
git add <files>
git commit -m "message"   # post-commit hook auto-tags vX.Y.Z
make ship                 # push → GitHub Action → goreleaser → brew upgrade
```

- Tags are created automatically by `.githooks/post-commit` (patch+1, rolls over at 99)
- `make ship` pushes the commit + tag, waits for the GitHub Action to finish, then runs `brew upgrade`
- New clones need `make setup` to enable the git hooks

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd dolt push
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->

<!-- BEGIN FORGE INTEGRATION -->
## Forge Toolkit

This project uses [forge](https://github.com/samahlstrom/forge-cli) — a portable AI agent toolkit.
Your personal toolkit lives at `~/.forge/` and is synced via `forge sync`.

### CLI Commands

```bash
forge list              # See all skills and agents
forge skill add <name>  # Create a new skill
forge skill remove <name> # Remove a skill
forge agent add <name>  # Create a new agent
forge agent remove <name> # Remove an agent
forge sync              # Pull/push toolkit changes
forge get <repo> <name> # Pull a skill from any repo
```

### Available Skills

- `/docx`
- `/evaluate`
- `/forge`
- `/ingest`
- `/skill-creator`

<!-- END FORGE INTEGRATION -->
