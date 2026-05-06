# forge

> Intake, classify, and execute tracked work through the forge pipeline.

## Trigger

User runs `/forge <description>` or `/forge --flag`

## You Are the Pipeline

You are the orchestrator. You run every stage directly using your tools — Bash for mechanical work, Agent for subagent dispatch, Read/Write for state. No external state machine. No PAUSE/resume. You run continuously from start to finish.

### Path Setup (MUST run first)

Before doing anything, resolve all paths by running:
```
Bash("forge paths")
```

This returns JSON with all resolved directories. Store these values and use them throughout:
- `forge_home` — root of forge (e.g., `~/.forge/`)
- `library_dir` — the toolkit content (e.g., `~/.forge/library/`)
- `agents_dir` — agent definitions (e.g., `~/.forge/library/agents/`)
- `skills_dir` — skill definitions (e.g., `~/.forge/library/skills/`)
- `pipeline_dir` — pipeline scripts (e.g., `~/.forge/library/pipeline/`)
- `repo_dir` — the forge repo clone (e.g., `~/.forge/repo/`)

**CRITICAL**: Nothing is written to the project directory. All agents/pipeline/skills live in `library_dir`. The toolkit is read-only from the project's perspective — zero footprint.

**Agents are discovered dynamically**: list `<agents_dir>/*.md` to see what's available. New agents added via `forge agent add` are immediately usable.

### Architecture Scan (MUST run before any work)

Before intake, scan the project root for architecture documentation and load it into context:

```
Bash("ls ARCHITECTURE.md architecture.md docs/ARCHITECTURE.md docs/architecture.md .forge/context/architecture.md 2>/dev/null")
```

For every file that exists, `Read` it in full. Capture the absolute paths into `architecture_docs[]`.

**If any architecture doc is found:**
- Treat it as authoritative for module boundaries, layering rules, naming conventions, and tech choices. Do not violate documented constraints without explicit user approval.
- Pass `architecture_docs[]` to every dispatched agent (Architect, Security, executors, evaluators) as required reading alongside `<context_dir>/stack.md` and `<context_dir>/project.md`.
- The Architect MUST cite which sections of the architecture doc shaped the decomposition. The Plan Reviewer MUST flag any subtask that conflicts with the documented architecture as `revise`.

**If none is found:** record `architecture_docs = []` and continue. Do not block on missing docs.

**DO NOT:**
- Skip stages (intake → classify → decompose → review-plan → execute → verify → evaluate → deliver)
- Write code without going through decompose → review-plan → execute
- Decide a task is "too simple" for decomposition (only `--quick` and `--hotfix` flags skip it)
- Ask the user for input mid-pipeline — the pipeline is autonomous

## Contract

1. **OPEN**: "Starting /forge pipeline for: [description]."
2. **WORK**: Execute Steps 1-13 below.
3. **REPORT**: Present summary — what was built, agents dispatched, scores, decisions, issues.
4. **CLOSE**: "Pipeline complete."

---

## Step 0: Resume Check

If the user passed `--resume <id>`:

1. Set `task_id` = `<id>`
2. Set `task_dir` = `<runs_dir>/<task_id>`
3. Check which artifacts exist and skip to the appropriate step:
   - `run-report.json` exists → Already done. Read and show the report. Stop.
   - `evaluation-N.json` with `verdict: "pass"` → Skip to Step 11
   - `execution.json` exists → Skip to Step 8
   - `plan-review.json` with `verdict: "approve"` → Skip to Step 7
   - `decomposition.json` exists → Skip to Step 6
   - `classification.json` exists → Skip to Step 4
   - `intake.json` exists → Skip to Step 3
   - Nothing found → Error: no run found for `<id>`
4. Read `classification.json` to recover `tier`, read bd labels to recover `mode`

---

## Step 1: Intake

Run intake to parse the user's input:

```
Bash("bash <pipeline_dir>/intake.sh <user-input-with-flags>")
```

Parse the JSON output: `title`, `description`, `mode`, `quality_score`.

## Step 2: Create Task

Create a bead for tracking:

```
Bash("echo '<description>' | bd create '<title>' --body-file - --add-label 'mode:<mode>' --json")
```

Extract `task_id` from the JSON response (the `.id` field).

```
Bash("mkdir -p <runs_dir>/<task_id>/reports")
```

Write the intake JSON to `<runs_dir>/<task_id>/intake.json`.

## Step 3: Classify Risk

Read `<pipeline_dir>/classify.md` for the classification rules.
Run `Bash("bd show <task_id>")` to see the full task context.

Apply the rules yourself — this is a language task, no agent needed:
- **T3**: touches auth, encryption, PII, payments, security controls
- **T2**: touches APIs, database, business logic, state management
- **T1**: styling, docs, tests, config only
- **T3 wins**: if ANY part is T3, the whole task is T3
- **When ambiguous, go up**: T1↔T2 → pick T2, T2↔T3 → pick T3

Then determine `behavior_change` per the rules in `<pipeline_dir>/classify.md` § Behavior Change Flag. When ambiguous, set `behavior_change=true`.

Write your classification to `<runs_dir>/<task_id>/classification.json`:
```json
{"tier": "T1|T2|T3", "behavior_change": true|false, "reason": "One sentence explaining tier and behavior_change"}
```

Label the bead:
```
Bash("bd update <task_id> --add-label 'tier:<tier>'")
Bash("bd update <task_id> --add-label 'behavior:<true|false>'")
```

## Step 4: Mode Check

`mode` (`quick`/`hotfix`/full) controls decomposition and tri-agent evaluation. It does **not** control the test-first requirement.

- If `mode` is `quick` or `hotfix` AND `behavior_change` is `false`: **skip to Step 8** (no Wave 0, no decomposition, no evaluation).
- If `mode` is `quick` or `hotfix` AND `behavior_change` is `true`: run **Step 4.5 (Wave 0)** then skip directly to Step 8 (no decomposition, no evaluation, but the redline test is still authored and verified).
- All other modes proceed through Wave 0 then full decomposition and review.

The redline test is never skipped for a behavior change. This is non-negotiable doctrine (see `<library_dir>/doctrine/tdd.md`).

## Step 4.5: Wave 0 — Quality Writes the Redline Test

**Run this step whenever `behavior_change=true`, regardless of mode.**

Dispatch the Quality agent **before** any builder agent runs:

```
Agent (model: sonnet):
"You are the Quality agent operating as Wave 0. Read and follow <agents_dir>/quality.md.
Read doctrine: <library_dir>/doctrine/tdd.md.
Task: bd show <task_id>
Project context: <context_dir>/stack.md, <context_dir>/project.md (read if present)

Your job in this wave:
1. Identify the System Under Test (SUT) for the requirement.
2. Write ONE failing behavior test at the SUT's public surface.
3. Run the project's test command and capture the failure output.
4. Confirm the failure is for the intended reason (missing behavior, not typo).
5. Write the failure output to <runs_dir>/<task_id>/redline-wave0.txt — this is the redline artifact.
6. Write your JSON report to <runs_dir>/<task_id>/reports/wave0-quality.json.

Do NOT write implementation. That is the builder's job in subsequent waves.
Follow the Agent Contract: OPEN → WORK → REPORT → CLOSE."
```

After the agent returns:

1. Read `<runs_dir>/<task_id>/reports/wave0-quality.json`.
2. Verify `status` is `complete`, `test_count` >= 1, and `redline_artifact` points to a file that exists and is non-empty.
3. Verify `redline_failure_reason` describes a missing behavior, not just a missing function/symbol. If it only says "function does not exist" or similar, the test is structural — reject and re-dispatch with revision instructions.
4. If verification fails: re-dispatch up to twice with the failure as additional context. After 3 total attempts, close the task with `bd close <task_id> --reason "Wave 0 redline test could not be authored"` and stop.

Once the redline is accepted, store its path in pipeline state for use in Step 8 verification.

If `mode` is `quick` or `hotfix`: skip directly to **Step 7** (execute waves) using a one-wave manifest where every builder subtask is gated on the redline test passing. Do not run decomposition or plan review.

## Step 5: Decompose + Security

Dispatch TWO agents in parallel (single message, two Agent tool calls):

**Before dispatching**, build the agent roster dynamically:
```
Bash("ls <agents_dir>/*.md | sed 's/.*\///' | sed 's/\.md$//'")
```
Then for each agent, read the YAML frontmatter to get `id`, `specializes`, `good_at`. Build the roster list.

**Agent 1 — Architect (model: sonnet):**
```
"You are the Architect agent. Read and follow <agents_dir>/architect.md.
Task: bd show <task_id>
Read context: <context_dir>/stack.md, <context_dir>/project.md, <each path in architecture_docs[]>
Write your execution manifest JSON to <runs_dir>/<task_id>/decomposition.json

Available agents for subtask assignment (each maps to <agents_dir>/<name>.md):
<dynamically built roster with name — specializes for each agent>

Assign each subtask to one of these agents based on what the subtask does.
Follow the Agent Contract: OPEN → WORK → REPORT → CLOSE."
```

**Agent 2 — Security (model: sonnet):**
```
"You are the Security agent. Read and follow <agents_dir>/security.md.
Task: bd show <task_id>
Read context: <context_dir>/stack.md, <context_dir>/project.md, <each path in architecture_docs[]>
Analyze security implications. Output risk annotations JSON to
<runs_dir>/<task_id>/security-annotations.json
Follow the Agent Contract: OPEN → WORK → REPORT → CLOSE."
```

After both return:
1. Read `decomposition.json` and `security-annotations.json`
2. Merge security annotations into the decomposition subtasks (add `security` field to each subtask that has annotations)
3. Write the merged result back to `decomposition.json`

## Step 6: Review Plan

Dispatch the plan reviewer:

```
Agent (model: sonnet): "You are the Plan Reviewer. Read and follow <pipeline_dir>/review-plan.md.
Review the decomposition at <runs_dir>/<task_id>/decomposition.json.
Read context: <context_dir>/stack.md, <context_dir>/project.md, <each path in architecture_docs[]>.
Write your review JSON to <runs_dir>/<task_id>/plan-review.json.
Follow the Agent Contract: OPEN → WORK → REPORT → CLOSE."
```

Read `plan-review.json`. Check `verdict`:
- **approve** → proceed to Step 7
- **revise** or **reject** → archive current review as `plan-review-N.json`, increment counter
  - If counter >= 3: close the task bead (`bd close <task_id> --reason "Plan failed review 3 times"`), report failure to user, stop
  - Otherwise: go back to Step 5 with the `revision_instructions` from the review as additional context for the architect

## Step 7: Execute Waves

Read `decomposition.json`. Extract `waves[]` and `subtasks[]`.

**For each wave in order:**

1. Read the wave's subtask IDs
2. For EACH subtask in the wave, spawn an agent with `isolation: "worktree"`, `model: "sonnet"` — all subtasks in the same wave in a SINGLE message (parallel):

```
Agent (per subtask, isolation: "worktree", model: "sonnet"):
"You are a <assigned_agent> agent working on bead <bead_id>.
Read and follow <agents_dir>/<assigned_agent>.md.

Task: <parent task title>
Subtask: <subtask title> (<subtask id>)
Bead: <bead_id>

Instructions:
<subtask instructions from manifest>

Files to modify: <subtask files>
Only modify these files.

Verification: <subtask verification>

Project context: <context_dir>/stack.md, <context_dir>/project.md, <each path in architecture_docs[]>
If architecture docs are listed, treat them as authoritative — do not violate documented module boundaries, layering rules, or tech choices.

<If revision iteration: include revision_brief from latest evaluation>

When complete:
1. Write your JSON report to <runs_dir>/<task_id>/reports/<subtask_id>.json
2. Report MUST include: bead_id, task_given, approach_planned, approach_taken,
   files_modified, files_created, decisions, issues_encountered, status
3. Close your bead: bd close <bead_id> --reason='<summary>'"
```

3. After ALL subtasks in the wave return, merge and clean up each worktree:
   - Each agent result includes a `worktree_path` and `branch`. For each:
     ```
     Bash("git merge <branch> --no-edit")
     Bash("git worktree remove <worktree_path>")
     Bash("git branch -d <branch>")
     ```
   - If merge conflicts occur, resolve them before proceeding.

4. Run the wave gate — read the `commands` section from `forge.yaml` for the actual commands:
   ```
   Bash("<typecheck command from forge.yaml>")
   ```
   If the wave's `gate` includes "test":
   ```
   Bash("<test command from forge.yaml>")
   ```

5. If the gate fails:
   - Read error output to identify which subtask's files caused the failure
   - Retry that subtask ONCE with the error output as additional context
   - If retry fails: `Bash("git checkout -- <files>")` to revert, close the subtask bead (`bd close <bead_id> --reason "Deferred: gate failure after retry"`), mark subtask as deferred, continue

6. After ALL waves complete, read all reports from `<runs_dir>/<task_id>/reports/`:
   - Compile `execution.json`: status, waves_executed, subtasks_completed, subtasks_deferred, files_modified, verification results
   - Compile `agent-log.json`: every agent's report, successes, failures, improvements

Write both files to the task's run directory.

## Step 8: Verify

Run mechanical verification — pass `behavior_change` so the verifier knows whether to enforce the redline check:
```
Bash("bash <pipeline_dir>/verify.sh <task_id> <tier> <behavior_change>")
```

The verifier runs (in order, stopping on first required failure):

1. **typecheck** (required)
2. **lint** (required)
3. **test** (required) — the project's full test suite, including the Wave-0 redline test now in its passing state
4. **banned-patterns** (required for `behavior_change=true`) — calls `<pipeline_dir>/banned-patterns.sh` to grep all `**/*.{test,spec}.*` files in the diff for forbidden assertions and scaffolding shapes (see `<library_dir>/doctrine/tdd.md` § Banned Test Patterns)
5. **redline** (required for `behavior_change=true`) — calls `<pipeline_dir>/redline-check.sh` to verify the Wave-0 redline test transitioned red→green between the parent commit and HEAD
6. coverage / security / browser smoke (configured per project)

Read the JSON output. If `passed` is false:
- Close the task bead: `Bash("bd close <task_id> --reason 'Verification failed: <failed_check>'")`
- Also close any open subtask beads: for each subtask bead that wasn't already closed, run `bd close <bead_id> --reason "Parent task verification failed"`
- Report the failed check and stderr to the user
- Stop the pipeline (user needs to fix and `/forge --resume <task_id>`)

A failure of `banned-patterns` or `redline` is a doctrine violation. The user message must include a link to `<library_dir>/doctrine/tdd.md` and the specific banned pattern or missing redline.

If `mode` is `quick` or `hotfix`: **skip to Step 11**.

## Step 9: Evaluate

Generate the diff for evaluators:
```
Bash("git diff HEAD~1 > <runs_dir>/<task_id>/eval-<iteration>-diff.patch")
```
(If `HEAD~1` fails, use `git diff` instead)

Dispatch THREE evaluators in parallel (single message, three Agent tool calls):

**Agent 1 — Edgar (model: sonnet):**
```
"You are Edgar. Read and follow <agents_dir>/edgar.md.
Evaluate the diff at <runs_dir>/<task_id>/eval-<iteration>-diff.patch.
Read execution summary at <runs_dir>/<task_id>/execution.json.
Context: <context_dir>/stack.md, <context_dir>/project.md, <each path in architecture_docs[]>.
If architecture docs are present, score the diff against them — flag any violation of documented architecture as a critical finding.
Write your JSON report to <runs_dir>/<task_id>/reports/eval-<iteration>-edgar.json.
Follow the Agent Contract: OPEN → WORK → REPORT → CLOSE."
```

**Agent 2 — Code Quality (model: sonnet):** (same pattern, reads `<agents_dir>/code-quality.md`, writes `eval-<iteration>-code-quality.json`)

**Agent 3 — Um-Actually (model: sonnet):** (same pattern, reads `<agents_dir>/um-actually.md`, writes `eval-<iteration>-um-actually.json`)

## Step 10: Aggregate + Verdict

After all three evaluators return, read their report files.

Extract `weighted_total` from each (default `0.5` if missing or invalid JSON).

Compute composite score:
```
composite = (edgar * 0.35) + (code_quality * 0.35) + (um_actually * 0.30)
```

Determine verdict:
- **PASS** (composite >= 0.7 AND no evaluator has verdict "fail"):
  Write `evaluation-<iteration>.json` with verdict "pass". Proceed to Step 11.

- **REVISE** (composite 0.5-0.7 OR any evaluator verdict "conditional"):
  If iteration < 3:
    - Collect all critical and high severity findings from all three reports
    - Build a revision brief with: scores per evaluator, composite, specific findings with file:line references
    - Write `evaluation-<iteration>.json` with verdict "revise" and the revision brief
    - Increment iteration. Go back to Step 7 (re-execute with revision brief as context)
  Else: treat as FAIL

- **FAIL** (composite < 0.5 OR evaluator verdict "fail" AND iteration >= 3):
  Write `evaluation-<iteration>.json` with verdict "fail"
  Close the task bead: `Bash("bd close <task_id> --reason 'Evaluation failed: composite=<score>'")`
  Also close any open subtask beads: for each subtask bead that wasn't already closed, run `bd close <bead_id> --reason "Parent task evaluation failed"`
  Report failure to user with all findings. Stop.

## Step 11: Deliver

Create branch, commit, and push:
```
Bash("bash <pipeline_dir>/deliver.sh <task_id>")
```

Read the JSON output: `branch`, `commit_sha`, `has_remote`.

## Step 12: Create PR

If `has_remote` is false: skip PR creation, just report the branch and commit.

Write a PR body based on everything you know — the task description, execution summary, evaluation scores, agent decisions. Write it to `<runs_dir>/<task_id>/pr-body.md`.

Create the PR:
```
Bash("gh pr create --title '<title (max 70 chars)>' --body-file <runs_dir>/<task_id>/pr-body.md --head <branch>")
```

Add labels by tier:
- T3: `Bash("gh pr edit <pr_url> --add-label 'critical,security-review'")`
- T2: `Bash("gh pr edit <pr_url> --add-label 'needs-review'")`

## Step 13: Report + Close

Build the run report and write to `<runs_dir>/<task_id>/run-report.json`:
```json
{
  "task_id": "<task_id>",
  "title": "<title>",
  "pr_url": "<pr_url>",
  "branch": "<branch>",
  "tier": "<tier>",
  "agents_dispatched": N,
  "agents_succeeded": N,
  "agents_failed": N,
  "evaluation_iterations": N,
  "final_composite_score": 0.0
}
```

Close the bead:
```
Bash("bd close <task_id> --reason 'Delivered: PR <pr_url>'")
```

Present the summary to the user:
- What was built (title, PR URL, branch)
- How many agents dispatched, succeeded, failed
- Key decisions agents made (from agent-log.json)
- Evaluation scores and iterations needed
- Issues encountered and how they were resolved

---

## Agent Report Format

Every agent spawned during Step 7 MUST write a structured report:

```json
{
  "bead_id": "<assigned bead>",
  "subtask_id": "<subtask id>",
  "agent": "<agent id>",
  "task_given": "What they were asked to do",
  "approach_planned": "How they planned to do it before starting",
  "approach_taken": "What they actually did",
  "files_modified": [],
  "files_created": [],
  "decisions": ["Key choices and why"],
  "issues_encountered": ["Problems and resolutions"],
  "status": "complete|blocked|failed"
}
```

`task_given` vs `approach_taken` is the metric for measuring agent effectiveness.

## Flags

- `--quick` — Skip decomposition + evaluation, minimal verification
- `--hotfix` — Skip decomposition + evaluation, auto T1
- `--issue <N>` — Fetch GitHub issue as input
- `--resume <id>` — Resume from last completed step
