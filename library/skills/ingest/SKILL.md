---
name: ingest
description: Parse a spec document into a structured project plan with epics, features, and atomic tasks. Use when a user has a PRD, technical spec, or requirements document and wants to decompose it into executable work before writing code. Triggers on /ingest, or when user mentions ingesting a spec, parsing requirements, or planning from a document.
---

# ingest

> Decompose a spec document into a structured project plan, generate custom skills, and execute phase-by-phase through the /forge pipeline.

## Trigger

User runs `/ingest <spec-id>` where spec-id corresponds to a directory in `.forge/specs/`.

## Process

### Step 1: Load the spec

1. Read `.forge/specs/<spec-id>/meta.json` to get source file path, format, page count, and chunk size
2. If `.forge/specs/<spec-id>/analysis.json` exists, read it — this contains project metadata extracted during `forge init --spec`. **Treat this as a starting hint only — you MUST re-derive all metadata from the full document in Pass 1.**
3. Read the **ENTIRE** source document at `.forge/specs/<spec-id>/source.*` — every single page, no skipping
   - For PDFs: read ALL pages in chunks of 20 using the `pages` parameter (e.g., pages 1-20, then 21-40, then 41-60, etc.) — do NOT stop until you have read every page
   - For text/markdown: read the full file
   - **You MUST read the complete document before proceeding to any analysis pass. Partial reads produce garbage plans.**

### Step 2: Multi-pass analysis

Run four analysis passes. Each pass builds on the previous ones. Write outputs to `.forge/specs/<spec-id>/`.

#### Pass 1: Structure Extraction

Read the spec (chunk by chunk for large documents) and extract:

- **Sections**: headings, hierarchy, page ranges
- **Requirements**: SHALL/MUST/SHOULD statements with IDs
- **User stories**: As a... I want... So that...
- **Constraints**: performance, compliance, compatibility, security
- **Data entities**: models, schemas, relationships
- **Glossary**: domain-specific terms

Additionally, re-derive the project metadata by answering these validation questions based on the FULL document:
- **Does this project have a user-facing UI?** (web pages, dashboards, portals, forms, mobile views) — if yes, `project_type` MUST be `web-app` or `fullstack`, never `api` or `library`
- **What do end users actually interact with?** (browser, CLI, API, mobile app, desktop app)
- **What is the full technology stack?** (frontend framework, backend framework, database, etc.)

If the answers contradict `analysis.json`, update the analysis. Write the corrected metadata to `analysis.json`.

Write to: `.forge/specs/<spec-id>/pass-1-structure.json`

For chunked documents: run pass 1 per chunk, then consolidate into a single pass-1-structure.json before proceeding. **You MUST process ALL chunks — do not skip any.**

#### Pass 2: Domain Mapping

Read pass-1 output. Identify:

- **Domains/modules**: logical groupings (e.g., auth, billing, scheduling)
- **Cross-references**: which sections feed which domains
- **Dependency graph**: which domains depend on others
- **Shared concerns**: cross-cutting things like logging, auth, multi-tenancy

Write to: `.forge/specs/<spec-id>/pass-2-domains.json`

#### Pass 3: Epic/Feature/Task Breakdown

Read pass-1 + pass-2 outputs. Decompose into:

- **Epics**: one per domain or major capability
- **Features**: logical groupings within an epic (use as many as the spec requires — do NOT artificially cap)
- **Tasks**: atomic units of work, each completable in a single `/forge` call (use as many as needed — do NOT artificially limit)

Each task gets:
- `id`: unique identifier (e.g., task-1-1-1)
- `title`: what to do
- `description`: detailed description with acceptance criteria
- `risk_tier`: T1 (low), T2 (moderate), T3 (critical) — using the same classification as the pipeline
- `dependencies`: list of task IDs that must complete first
- `files_likely`: predicted files to create or modify
- `agent`: which agent handles this (architect, backend, frontend, quality, security)

Write to: `.forge/specs/<spec-id>/pass-3-breakdown.json`

#### Pass 4: Skill Identification

Read pass-3 output. Look for:

- **Repeated patterns**: if "create CRUD for entity X" appears 4+ times, that's a skill
- **Domain workflows**: complex multi-step processes that appear more than once
- **Boilerplate patterns**: things like "add API endpoint with validation + tests + docs"

For each identified skill:
- `name`: kebab-case identifier
- `description`: what it does and when to trigger
- `pattern`: the repeated work it captures
- `estimated_savings`: how many tasks it simplifies

Write to: `.forge/specs/<spec-id>/pass-4-skills.json`

### Step 3: Synthesize

Combine all pass outputs into a single `spec.yaml`:

```yaml
version: 1
spec_id: "<id>"
status: "draft"
summary: "<1-2 sentence project summary>"

domains:
  - id: "dom-<name>"
    name: "<Domain Name>"
    dependencies: ["dom-<other>"]

epics:
  - id: "epic-<N>"
    domain: "dom-<name>"
    title: "<Epic title>"
    features:
      - id: "feat-<N>-<M>"
        title: "<Feature title>"
        tasks:
          - id: "task-<N>-<M>-<K>"
            title: "<Task title>"
            description: "<Detailed description>"
            risk_tier: "T1|T2|T3"
            dependencies: []
            files_likely: []
            agent: "backend|frontend|quality|security"

execution_plan:
  phases:
    - id: "phase-<N>"
      name: "<Phase name>"
      epics: ["epic-<N>"]
      rationale: "<Why this phase comes here>"
      parallelizable: true|false

  total_tasks: <count>
  critical_path: ["task-...", "task-..."]

generated_skills: ["skill-name-1", "skill-name-2"]

constraints:
  - "<Hard constraint from the spec>"
```

Write to: `.forge/specs/<spec-id>/spec.yaml`

### Step 4: Review Gate

Present the plan to the user. Show:

```
Spec Analysis Complete
═══════════════════════

  Domains:       6
  Epics:         8
  Features:      31
  Tasks:         52
  Phases:        4
  Custom skills: 4
  Risk profile:  23 T3, 18 T2, 11 T1

  Phase 1 — Foundation
    <epics and task count>

  Phase 2 — Core Features
    <epics and task count>

  ...

  Constraints:
    • <constraint 1>
    • <constraint 2>

  Custom skills to generate:
    • <skill-1>: <what it does>
    • <skill-2>: <what it does>
```

Ask: **Approve, refine, or cancel?**

### Step 5: Refinement (if needed)

If the user wants changes:
1. Take their feedback as natural language
2. Re-run the affected passes with the feedback as additional context
3. Update spec.yaml
4. Return to the review gate

Common refinement requests:
- "Split epic X into two epics"
- "Move module Y to an earlier phase"
- "Add a constraint about Z"
- "This task is too big, break it down further"
- "Remove the reporting module, that's out of scope"

Loop until the user approves.

### Step 6: Generate Skills

After approval, for each skill identified in pass-4:

1. Create `.claude/skills/<skill-name>/SKILL.md` with proper frontmatter
2. Write the skill body with:
   - Step-by-step instructions specific to this project's stack
   - References to project context files
   - Output format expectations
   - Examples from the spec

Use the `/skill-creator` skill's patterns for writing good skill files.

### Step 7: Seed Beads

After skills are generated, create beads for all tasks in the plan.

**IMPORTANT: Do NOT create beads manually with `bd create`. Only use `forge seed`.**

1. Run: `forge seed <spec-id>`
2. This creates bd issues for every phase, epic, and task in spec.yaml with correct types and dependency links
3. Do NOT use `bd create` directly — `forge seed` handles the full dependency graph
4. Report the counts to the user

### Step 8: Execution Handoff

After beads are seeded, automatically start auto-pilot execution by running:

```bash
forge run <spec-id> --yes
```

Run this command directly — do NOT tell the user to exit Claude Code or run it manually. The `forge run` command works from within Claude Code.

Options you can append:
- `--phase 1` — run only one phase at a time
- `--concurrency 3` — run 3 tasks in parallel worktrees
- `--budget 5` — set per-task budget cap in USD
- `--no-review` — skip review gates between phases

For manual execution instead:
- Run `bd ready -t task -l "spec:<spec-id>"` to see available tasks
- Pick a task and run `/forge` with its description
- Close the bead with `bd close <id>` when done

### Resuming

If the session is interrupted, the user can run `/ingest <spec-id>` again. Check `spec.yaml` status:
- `pending-analysis`: start from pass 1
- `draft`: show the review gate
- `approved`: resume skill generation, seeding, or execution
- `in-progress`: run `forge run <spec-id> --yes` to resume — it picks up where it left off via `bd ready`

## Output Protocol

All intermediate outputs go to `.forge/specs/<spec-id>/`:
- `meta.json` — source file metadata
- `analysis.json` — project metadata from init
- `pass-1-structure.json` — extracted structure
- `pass-2-domains.json` — domain mapping
- `pass-3-breakdown.json` — epic/feature/task breakdown
- `pass-4-skills.json` — identified skills
- `spec.yaml` — the final synthesized plan
- `refinement-log.json` — history of user refinements
