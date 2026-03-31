# Forge CLI: Complete Workflow Specification

> Compiled from the forge-cli implementation and compared against the origin specification in `healthtree/one` (`deliver/full-pipeline` branch).

---

## Table of Contents

1. [System Overview](#system-overview)
2. [Pipeline Stages](#pipeline-stages)
   - [Stage 1: Intake](#stage-1-intake)
   - [Stage 2: Quality Gate](#stage-2-quality-gate)
   - [Stage 3: Classify](#stage-3-classify)
   - [Stage 4: Decomposition Check](#stage-4-decomposition-check)
   - [Stage 5: Decompose](#stage-5-decompose)
   - [Stage 6: Review Plan](#stage-6-review-plan)
   - [Stage 7: Execute](#stage-7-execute)
   - [Stage 8: Verify](#stage-8-verify)
   - [Stage 9: Evaluate](#stage-9-evaluate)
   - [Stage 10: Deliver](#stage-10-deliver)
3. [Spec Ingestion (`/ingest`)](#spec-ingestion)
4. [Auto-Pilot Execution (`forge run`)](#auto-pilot-execution)
5. [Agent Definitions](#agent-definitions)
6. [Configuration](#configuration)
7. [Risk Tier Classification](#risk-tier-classification)
8. [Workflow Shortcuts](#workflow-shortcuts)
9. [Comparison: Forge CLI vs Origin Spec](#comparison-forge-cli-vs-origin-spec)

---

## System Overview

**Forge** is an AI agent pipeline scaffolding tool for Claude Code. It transforms any repository into an orchestrated, multi-agent workspace that takes a work description and drives it through intake, risk classification, architectural decomposition, parallel code generation, mechanical verification, adversarial evaluation, and automated PR delivery.

**Core design:**
- Built in Go (single binary, cross-platform)
- Integrates via Claude Code skills (`/deliver`, `/ingest`, `/skill-creator`)
- Uses **beads (`bd`)** — a Dolt-backed, AI-native issue tracker
- Pipeline implemented as shell scripts + LLM prompt files forming a JSON state machine
- All stage inputs/outputs persisted to `.forge/pipeline/runs/<task-id>/` for auditability and resumability

**Entry point:** `/deliver "description"` triggers the orchestrator, which runs a 10-stage state machine. The orchestrator emits JSON control messages (`PAUSE`, `HUMAN_INPUT`, `DONE`, `ERROR`) and the skill executor dispatches LLM work to subagents at each `PAUSE` point.

---

## Pipeline Stages

### Stage 1: Intake

**Script:** `intake.sh`
**Purpose:** Parse input and score description quality.

**Process:**
1. Extract work description, title, and flags (`--quick`, `--hotfix`, `--issue N`)
2. If `--issue N`, fetch GitHub issue title + body via `gh issue view`
3. Score quality (0.0 - 1.0) across six dimensions:

| Dimension | Weight | What it checks |
|-----------|--------|----------------|
| What | 25% | Action verbs + object nouns present |
| Why | 15% | Rationale keywords (because, to fix, to prevent) |
| Where | 15% | File paths, component names, area references |
| Scope | 15% | Bounded vs unbounded description |
| Layers | 20% | Mentions of architectural layers (UI, API, DB, auth, state) |
| Criteria | 10% | Acceptance criteria or done conditions |

4. Create a beads task with the description and mode label

**Output:** `{ title, description, source, mode, quality_score }`

---

### Stage 2: Quality Gate

**Script:** Inline in `orchestrator.sh`
**Purpose:** Prevent low-quality descriptions from entering the pipeline.

**Logic:**
- If `quality_score < 0.4` (configurable), present human with options:
  1. Continue anyway
  2. Provide more detail
  3. Cancel task
- If score is acceptable, proceed automatically
- `--quick` and `--hotfix` modes bypass this gate

---

### Stage 3: Classify

**Script:** `classify.sh`
**Purpose:** Assign risk tier that determines verification intensity.

**Tiers:**

| Tier | Risk | Examples | Verification Level |
|------|------|----------|-------------------|
| T3 | Critical | Auth, encryption, PII, payment, security | Full + security scan |
| T2 | Moderate | Business logic, APIs, database, state | Full + optional coverage |
| T1 | Low | UI, docs, styling, tests, config | Standard (typecheck + lint + test) |

**Matching mechanism:**
- Built-in keyword patterns per tier
- Custom keywords/paths from `forge.yaml` (weighted higher)
- T3 always wins if any T3 match detected

**Output:** `{ tier, reason, hits: { t3, t2, t1 } }`

Labels the beads task with `tier:<T1|T2|T3>`.

---

### Stage 4: Decomposition Check

**Script:** Inline in `orchestrator.sh`
**Purpose:** Determine whether to run full architectural decomposition.

**Logic:**
- **Skip decomposition:** Only if `--quick` or `--hotfix` (user's explicit choice)
- **Normal mode:** ALL tasks go through full architect decomposition, regardless of tier

This prevents the LLM from making autonomous decisions about when to skip planning.

---

### Stage 5: Decompose

**Prompt:** `decompose.md` (dispatched to Architect subagent)
**Purpose:** Break work into parallel-safe subtasks organized into execution waves.

**Constraints:**
- Max 8 subtasks (prefer 2-4)
- Max 4 waves (prefer 1-2)
- No circular dependencies
- No file conflicts within a wave
- Each file assigned to exactly one subtask

**Agent types per subtask:** `code`, `test`, `docs`, `config`

**Wave ordering rules:**
- Type definitions and interfaces go to wave-1
- DB migrations go to wave-1 (before dependent code)
- Tests go in same wave as code they test
- Wide waves (3 subtasks in wave-1) beat deep chains (3 sequential waves)

**Output schema:**
```json
{
  "analysis": {
    "files_affected": ["..."],
    "dependency_graph": "description",
    "risk_notes": "..."
  },
  "subtasks": [
    {
      "id": "sub-1",
      "title": "...",
      "agent": "code",
      "files": ["src/lib/foo.ts"],
      "dependsOn": [],
      "verification": "npm run check passes",
      "instructions": "Detailed instructions"
    }
  ],
  "waves": [
    { "id": "wave-1", "subtasks": ["sub-1", "sub-2"], "gate": "typecheck" }
  ],
  "verification_plan": { ... }
}
```

**Saved to:** `.forge/pipeline/runs/<task-id>/decomposition.json`

---

### Stage 6: Review Plan

**Prompt:** `review-plan.md` (dispatched to Plan Reviewer subagent)
**Purpose:** Catch bad decompositions before execution wastes time.

**Review checklist (each scored 0.0 - 1.0):**

1. **Completeness** — Does the plan cover everything? Are tests included?
2. **File Conflict Safety** — Do any two subtasks in the same wave modify the same file?
3. **Dependency Correctness** — Are dependencies ordered correctly? Missing deps?
4. **Verification Adequacy** — Do verification criteria actually test the thing?
5. **Scope Creep** — Unnecessary refactoring bundled in?
6. **Instruction Clarity** — Could an unfamiliar agent execute each subtask?

**Verdicts:**
- **Approve** (composite >= 0.8, no dimension < 0.6): Proceed to execution
- **Revise** (composite >= 0.5): Send revision instructions back to architect
- **Reject** (composite < 0.5): Escalate to human

**Saved to:** `.forge/pipeline/runs/<task-id>/plan-review.json`

---

### Stage 7: Execute

**Prompt:** `execute.md` (dispatched to Execution Dispatcher subagent)
**Purpose:** Launch specialist agents wave-by-wave, verify between waves, handle failures.

**Protocol:**
1. For each wave in order:
   - Launch one subagent per subtask (parallel within wave)
   - Each subagent receives: instructions, file list, project context, agent role `.md`
   - Post-wave verification gate: typecheck (always), tests (if gate requires)
2. On failure:
   - Identify breaking subtask
   - Retry with error context (max 2 retries)
   - If still failing, revert files: `git checkout -- <files>`
   - Continue to next wave (mark subtask as deferred)
3. On completion: run full verification suite (typecheck + lint + test + browser smoke if applicable)

**Revision mode** (coming from failed evaluation):
- Read latest evaluation file
- Focus only on files/subtasks in revision brief
- Do NOT re-implement things that passed
- Address critical findings first

**Output:** `{ status, waves_executed, subtasks_completed, subtasks_deferred, files_modified, verification: { typecheck, lint, test } }`

**Saved to:** `.forge/pipeline/runs/<task-id>/execution.json`

---

### Stage 8: Verify

**Script:** `verify.sh`
**Purpose:** Run mechanical verification checks sequentially.

**Checks (in order, stops on first failure):**

| # | Check | Required | Condition |
|---|-------|----------|-----------|
| 1 | Typecheck | Always | - |
| 2 | Lint | Always | - |
| 3 | Test | Always | - |
| 4 | Coverage | Optional | T2+ only, configurable threshold |
| 5 | Security scan | Optional | T3 only (semgrep) |
| 6 | Anti-pattern | Optional | Blocks TODO, FIXME, console.log, debugger |
| 7 | Browser smoke | Optional | If frontend files changed |

**Browser smoke tests:**
- Playwright visual regression
- Mobile viewport: 375x812px
- Desktop viewport: 1440x900px
- Checks: HTTP status, overflow, screenshots
- Results fed to evaluators as evidence

**Output:** `{ passed, failed_check, stderr, checks[], tier }`

---

### Stage 9: Evaluate

**Prompt:** `evaluate.md` (dispatched to Evaluation Dispatcher subagent)
**Purpose:** Generator-evaluator separation — three independent agents score the implementation.

**Three evaluators run in parallel:**

#### Edgar the Edger (weight: 0.35)
*"What will break in production?"*

| Dimension | Weight | Focus |
|-----------|--------|-------|
| Robustness | 0.35 | Empty input, null, boundary values, race conditions |
| Error Handling | 0.25 | Error propagation, actionable messages, idempotence |
| Security Surface | 0.20 | Injection, TOCTOU, resource exhaustion, secret exposure |
| Brittleness | 0.20 | String format deps, hardcoded values, schema fragility |

#### Code Quality Evaluator (weight: 0.35)
*"Is this code well-built for this codebase?"*

| Dimension | Weight | Focus |
|-----------|--------|-------|
| Architecture Fit | 0.30 | Project patterns, abstraction consistency, layer placement |
| Maintainability | 0.25 | Function length, naming, premature abstraction, complexity |
| Performance | 0.20 | N+1 queries, hot path work, memory, missing indexes |
| Correctness Beyond Tests | 0.25 | Coverage gaps, logic errors, concurrency, off-by-one |

#### Um-Actually (weight: 0.30)
*"Does this follow documented best practices?"*

| Dimension | Weight | Focus |
|-----------|--------|-------|
| API Correctness | 0.35 | Correct API usage, return types, error codes |
| Framework Conventions | 0.35 | Idiomatic patterns for the stack |
| Documentation | 0.20 | Alignment with docs and standards |
| Upgrade Safety | 0.10 | Deprecation warnings, version compatibility |

**Composite scoring:**
```
composite = (edgar * 0.35) + (code_quality * 0.35) + (um_actually * 0.30)
```

**Verdict logic:**
- **PASS** (composite >= 0.7, no evaluator verdict "fail"): Proceed to delivery
- **REVISE** (0.5 <= composite < 0.7 OR any "conditional" verdict): Loop back to execution with revision brief (max 3 iterations)
- **FAIL** (composite < 0.5 OR "fail" AND no iterations left): Fail the task

**Score trending (iteration 2+):**
- Improved >= 0.1: revision is working
- Improved < 0.05: generator is stuck
- Decreased: revision made things worse, try different approach

**Saved to:** `.forge/pipeline/runs/<task-id>/evaluation-<iteration>.json`

---

### Stage 10: Deliver

**Script:** `deliver.sh`
**Purpose:** Create branch, commit, push, and open PR.

**Two-phase process:**

**Phase 1 — Branch + Commit + Push:**
- Generate branch: `feature/<task-id>-<slug>`
- Stage modified files
- Commit message (conventional commits): `{type}({slug}): {title}\n\nTask: {task-id}\nRisk: {tier}`
- Push: `git push -u origin <branch>`
- PAUSE: LLM writes PR body (reads execution summary, diff, browser test results)

**Phase 2 — Create PR:**
- Risk badge in PR body
- Auto-labels:
  - T3: `critical`, `security-review`
  - T2: `needs-review`
  - T1: (none)
- Create via `gh pr create`
- Close beads task: `bd close <task-id> --reason "Delivered: PR {url}"`

**Output:** `{ pr_url, branch, task_id, tier }`

---

## Spec Ingestion

**Command:** `forge ingest <files...>` or `/ingest <spec-id>`
**Purpose:** Decompose large specification documents into executable task plans.

### Multi-pass analysis:

**Pass 1 — Structure Extraction:**
Extract sections, headings, hierarchy, requirements (SHALL/MUST/SHOULD), user stories, constraints, data entities, glossary.

**Pass 2 — Domain Mapping:**
Identify logical domains/modules, map cross-references, build dependency graph, identify shared concerns (logging, auth, multi-tenancy).

**Pass 3 — Epic/Feature/Task Breakdown:**
- Epics: one per domain or major capability
- Features: logical groupings (max 5 per epic)
- Tasks: atomic units (max 8 per feature, completable in single `/deliver`)
- Each task gets: id, title, description with acceptance criteria, risk tier, dependencies, predicted files, agent assignment

**Pass 4 — Skill Identification:**
Scan for repeated patterns (4+ occurrences), identify domain workflows, recognize boilerplate. Generate `.claude/skills/<name>/SKILL.md` for each.

**Synthesis:** Produces `spec.yaml` with domains, epics, features, tasks, execution phases, critical path, and generated skills.

**Review gate:** User approves, refines, or cancels. Refinement re-runs affected passes.

**Seeding:** `forge seed <spec-id>` creates beads issues for all phases/epics/tasks with correct dependency links.

---

## Auto-Pilot Execution

**Command:** `forge run <spec-id> --yes [--phase N] [--concurrency 3] [--budget 5]`

- Launches parallel workers in isolated git worktrees
- Circuit breaker pauses on repeated rate limits
- Idle timeout kills hung tasks
- Budget control per-task spend limits
- Resumable: interrupted runs pick up via `bd ready`

---

## Agent Definitions

### Specialist Agents (Code Writers)

| Agent | Role |
|-------|------|
| Architect | Analyzes work, produces decomposition plan (no code) |
| Backend | Server-side code, APIs, database |
| Frontend | UI components, client logic |
| Quality | Tests, fixtures, QA automation |
| Security | Security review, security controls |

### Evaluator Agents (Quality Gates)

| Agent | Focus | Weight |
|-------|-------|--------|
| Edgar | Edge cases, robustness, security surface | 0.35 |
| Code Quality | Architecture, maintainability, performance | 0.35 |
| Um-Actually | Framework conventions, API correctness, docs | 0.30 |

---

## Configuration

### `forge.yaml` (user-customizable)

```yaml
version: 1
project:
  name: "My App"
  preset: "sveltekit-ts|react-next-ts|python-fastapi|go"

commands:
  typecheck: "npm run check"
  lint: "npm run lint"
  test: "npx vitest run"
  format: "npx prettier --write ."
  dev: "npm run dev"

agents:
  - architect
  - backend
  - frontend
  - quality
  - security

verification:
  typecheck: true
  lint: true
  test: true
  coverage:
    enabled: false
    threshold: 70
  security:
    enabled: false
    command: "semgrep --config=auto"
  anti_patterns:
    enabled: true
    blockers: ["TODO", "FIXME", "console.log", "debugger"]
  browser:
    enabled: true

evaluation:
  enabled: true
  pass_threshold: 0.7
  max_iterations: { T1: 3, T2: 3, T3: 3 }
  weights: { edgar: 0.35, code_quality: 0.35, um_actually: 0.30 }
  skip_modes: ["quick", "hotfix"]

pipeline:
  max_retries: 3
  max_subtasks: 15
  max_waves: 5
  auto_pr: true

tracking:
  backend: bd
  enforce_tracking: true

risk:
  t3_paths: []
  t3_keywords: [authentication, encryption, PII, payment]

addons: []
```

### Generated file structure

```
.forge/
├── pipeline/          # Orchestrator + stage scripts + prompts
├── agents/            # Agent instruction files (.md)
├── context/           # stack.md (conventions), project.md (custom)
├── hooks/             # session-start, pre-edit, post-edit
├── specs/             # Ingested spec documents
├── addons/            # Installed addon files
└── state/screenshots/ # Browser test evidence

.claude/
├── settings.json      # Permissions & hooks
└── skills/            # /deliver, /ingest, /skill-creator

forge.yaml             # Main configuration
CLAUDE.md              # Agent instructions (forge section merged)
```

---

## Risk Tier Classification

### T3 (Critical) Keywords
Authentication, authorization, login, password, credential, secret, token, JWT, OAuth, encryption, hash, certificate, SSL/TLS, PII, GDPR, HIPAA, PHI, SSN, payment, Stripe, billing, credit card, security, vulnerability, XSS, CSRF, injection, permission, RBAC, role, access control

### T2 (Moderate) Keywords
API, endpoint, route, handler, controller, database, migration, schema, SQL, service, repository, model, entity, state, store, reducer, business logic, workflow, validation, integration, webhook, event, queue, cache, session

### T1 (Low) Keywords
Style, CSS, color, font, spacing, UI, component, layout, responsive, doc, README, comment, changelog, test, spec, config, ESLint, Prettier, typo, rename, format, lint

---

## Workflow Shortcuts

| Mode | Flag | Behavior |
|------|------|----------|
| Normal | (default) | Full pipeline: all 10 stages |
| Quick | `--quick` | Skip decomposition + plan review, minimal verification, still evaluates |
| Hotfix | `--hotfix` | Skip decomposition, auto T1, minimal checks, fastest path |
| Resume | `--resume <id>` | Resume halted task at last stage |
| From Issue | `--issue N` | Fetch GitHub issue as input |

---

## Comparison: Forge CLI vs Origin Spec

The origin specification lives in `healthtree/one` on the `deliver/full-pipeline` branch, documented primarily in `docs/deliver-pipeline-rfc.md`. Forge CLI is the **productized, generalized extraction** of that spec. Below is a detailed comparison.

### Architecture

| Aspect | Origin Spec (healthtree/one) | Forge CLI |
|--------|------------------------------|-----------|
| **Tier model** | 6 explicit tiers (Inputs, Intake, Architecture, Dispatch, Verification, Delivery) | 10-stage state machine (more granular, same flow) |
| **Domain** | Healthcare (HealthTree), HIPAA-focused | General-purpose, any stack |
| **Compliance** | NIST 800-53 controls, OSCAL output, FedRAMP/SOC2 architecture | Removed — available as optional addons (`forge add compliance-hipaa`) |
| **Implementation** | Prompt files + shell scripts living inside the repo | Go CLI that generates prompt files + shell scripts into any repo |
| **Distribution** | Copy-paste into repo | `brew install forge-cli` / `go install` / binary download |

### Stage-by-Stage Mapping

| Origin Tier | Origin Stage | Forge Stage | Changes |
|-------------|-------------|-------------|---------|
| Tier 1 | 7 input types (text, issue, Figma, plan, hotfix, quick, resume) | Intake (Stage 1) | **Dropped Figma input** — too domain-specific. Kept text, issue, hotfix, quick, resume |
| Tier 2 | Quality gate (0.0-1.0), semantic enrichment (LLM), human approval block | Quality Gate (Stage 2) | **Removed semantic enrichment pass** — origin had a mandatory LLM layer answering 5 architectural questions before classification. Forge simplified to keyword-based scoring only. **Quality threshold lowered** from 0.8 to 0.4 |
| Tier 2 | Risk classifier with `calculate-pre-risk.sh` | Classify (Stage 3) | **Simplified** — origin used a script + LLM fallback. Forge uses pure keyword matching with custom overrides from `forge.yaml` |
| Tier 2 | Bead creation + claim | Intake (Stage 1) | Merged into intake — bead created during initial parsing |
| Tier 3 | Architect + Security agents in parallel | Decompose (Stage 5) | **Dropped parallel Security agent** — origin dispatched Architect and Security simultaneously. Forge dispatches Architect only; security concerns handled by Edgar evaluator later |
| Tier 3 | 8-dimension plan quality gate | Review Plan (Stage 6) | **Reduced to 6 dimensions** — origin had 8 review dimensions. Forge consolidated to 6 (completeness, file safety, dependencies, verification, scope, clarity) |
| Tier 3 | Wave planner (separate step) | Decompose (Stage 5) | **Merged** — wave planning is part of the architect's output, not a separate step |
| Tier 4 | 15 specialized agents | Execute (Stage 7) | **Reduced to 5 agents** (architect, backend, frontend, quality, security). Origin had 15 including Mobile Engineer, Data Engineer, Product Manager, UX Designer, Tech Writer, DevOps Engineer, Bead Inspector, QA Guard, Handoff Coordinator |
| Tier 4 | Context packs (tier-scoped knowledge bundles) | `stack.md` + `project.md` | **Simplified** — origin had tier-scoped context packs (tier1-fe, tier1-be, tier2-be, tier3-compliance). Forge uses two static files |
| Tier 4 | Git worktree isolation per subtask | In-place execution | **Changed** — origin isolated each subtask in a worktree. Forge executes in-place within waves (worktrees used only for `forge run` parallelism across tasks) |
| Tier 5 | Verification with 11 anti-patterns, 3-fail halt, debug report | Verify (Stage 8) | **Simplified** — origin had 11 specific anti-pattern blockers and generated debug reports on failure. Forge has 4 configurable anti-pattern blockers (TODO, FIXME, console.log, debugger) |
| Tier 5 | SemGrep with HIPAA/FedRAMP/OWASP rulesets | Verify (Stage 8) | **Made optional** — origin always ran HIPAA SemGrep rules. Forge makes security scan optional and T3-only |
| Tier 5 | Evidence collection (npm audit, coverage, SemGrep findings) | Not implemented | **Dropped** — origin collected evidence for OSCAL compliance reports. Forge has no compliance report output |
| Tier 6 | Security gate with NIST control mapping | Deliver (Stage 10) | **Dropped** — origin mapped each subtask to NIST 800-53 controls and generated OSCAL evidence. Forge delivers plain PRs with risk badges |
| Tier 6 | Human review requirements by tier | Deliver (Stage 10) | **Simplified** — origin required specific human review for T3 (security team sign-off). Forge adds labels (`security-review`) but doesn't enforce review |
| — | Lifecycle hooks (PreToolUse blocks edits without /deliver) | `.forge/hooks/pre-edit.sh` | **Kept** — both enforce task tracking before code changes |

### Evaluation System

| Aspect | Origin Spec | Forge CLI |
|--------|-------------|-----------|
| **Evaluator count** | Referenced but not fully specified in RFC | 3 named evaluators (Edgar, Code Quality, Um-Actually) |
| **Scoring** | Not detailed in RFC | Fully specified: 4 dimensions each, weighted composite |
| **Iteration limit** | Not specified | 3 iterations for all tiers |
| **Score trending** | Not specified | Tracked: improvement >= 0.1 OK, < 0.05 stuck, decrease = worse |

The tri-agent evaluation system is arguably Forge's most significant **addition** over the origin spec, which described verification tiers but didn't detail an adversarial evaluation loop.

### Features Present in Origin but Absent from Forge

| Feature | Why Dropped |
|---------|-------------|
| Figma input type | Too domain-specific |
| Semantic enrichment (LLM pre-processing of input) | Adds latency and cost for marginal quality improvement on general tasks |
| 15 specialized agents | Most repos don't need Mobile Engineer, Data Engineer, UX Designer, etc. |
| NIST 800-53 control mapping | Compliance-specific — moved to addon |
| OSCAL evidence output | Compliance-specific — moved to addon |
| SemGrep HIPAA rulesets | Healthcare-specific — moved to addon |
| Context packs (tier-scoped) | Over-engineered for general use |
| Per-subtask git worktree isolation | Performance cost; wave-level isolation sufficient |
| Debug report generation on verify failure | Pipeline pauses for human input instead |
| Throughput targets (30 PRs/day) | Removed as a spec requirement; depends on repo/model |
| Healthcare-specific templates | Domain-specific — moved to addon |

### Features Present in Forge but Absent from Origin

| Feature | Why Added |
|---------|-----------|
| Tri-agent adversarial evaluation (Edgar, Code Quality, Um-Actually) | Generator-evaluator separation; origin relied more on verification checks |
| Plan review stage (6-dimension review before execution) | Catches bad decompositions before wasting execution time |
| Browser smoke tests (Playwright visual regression) | Catches frontend regressions mechanically |
| `forge.yaml` user configuration | Origin was hardcoded to HealthTree's stack |
| Stack presets (sveltekit-ts, react-next-ts, python-fastapi, go) | Multi-stack support |
| `forge init` interactive onboarding | Origin was pre-configured |
| `forge add` addon system | Extensibility for compliance, etc. |
| `forge ingest` multi-pass spec analysis | Large-doc decomposition into executable tasks |
| `forge run` auto-pilot with budget controls | Autonomous batch execution |
| Quality scoring on intake (6-dimension, 0.0-1.0) | Origin had quality scoring but simpler |
| Configurable evaluation weights and thresholds | Origin was fixed |

### Design Philosophy Shift

The origin spec was a **compliance-grade, healthcare-specific pipeline** designed for a single product (HealthTree) with HIPAA/FedRAMP requirements baked into every tier. It prioritized:
- Regulatory compliance (NIST, OSCAL, evidence collection)
- Maximum agent specialization (15 agents)
- Full isolation (per-subtask worktrees)
- Throughput optimization (30 PRs/day target)

Forge CLI is a **generalized developer tool** that extracts the orchestration pattern and makes it work for any stack. It prioritizes:
- Zero-config onboarding (`forge init` detects your stack)
- Configurable verification intensity (risk-proportional)
- Adversarial quality evaluation (tri-agent system)
- Extensibility via addons (compliance bolted on, not baked in)
- Single-binary distribution

The core insight preserved from origin to Forge: **the pipeline never decides to skip stages — only explicit user flags can do that**. Both systems enforce the same principle that AI agents should not autonomously reduce their own quality checks.

---

*Document generated 2026-03-30. Source: forge-cli@main vs healthtree/one@deliver/full-pipeline.*
