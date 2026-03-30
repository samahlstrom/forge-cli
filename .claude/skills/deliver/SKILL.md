# deliver

> Intake, classify, and execute tracked work through the forge pipeline.

## Trigger

User runs `/deliver <description>` or `/deliver --flag`

## CRITICAL: You Are a Pipeline Executor, Not a Decision Maker

The orchestrator script (`orchestrator.sh`) is the authority. It decides what happens next — not you. Your ONLY job is to:
1. Run the orchestrator script
2. Read its JSON output
3. Do exactly what it says
4. Resume with the command it gives you

**DO NOT:**
- Skip any stage the orchestrator tells you to do
- Write code without going through the decompose → review-plan → execute stages
- Decide a task is "too simple" for decomposition
- Modify the pipeline flow in any way
- Make judgment calls about what stages are needed

The pipeline ALWAYS runs: intake → classify → decompose → review-plan → execute → verify → evaluate → deliver. The only exceptions are `--quick` and `--hotfix` flags, which the USER chooses — never you.

## Process

1. Run: `bash .forge/pipeline/orchestrator.sh <user-input>`
2. Read the LAST LINE of JSON output. Branch on `status`:

### PAUSE
The orchestrator needs LLM work (decompose, review-plan, execute, or evaluate).

**CRITICAL: ALL pause work MUST be dispatched to a subagent.** Never do pause work in the main context — it pollutes the conversation with noise. Your job is to:
1. Launch a subagent (via the Agent tool) with the prompt file, context files, and output file path
2. Wait for the subagent to complete
3. Run the `resume` command to hand control back to the orchestrator

**For decompose pauses**: Launch a subagent to act as the architect agent. The subagent reads `prompt_file`, `.forge/agents/architect.md`, and any `context[]` files. It writes the decomposition JSON to `output_file`.

**For review-plan pauses**: Launch a subagent to act as the plan reviewer. The subagent reads `prompt_file` (`.forge/pipeline/review-plan.md`) and the decomposition file. It writes its verdict JSON to `output_file`.

**For execute pauses**: Launch a subagent as the execution dispatcher. It reads `.forge/pipeline/execute.md`, the decomposition, and launches its own sub-subagents for each subtask per wave. It writes execution results to `output_file`.

**For evaluate pauses**: Launch a subagent as the evaluation dispatcher. It reads `.forge/pipeline/evaluate.md` and launches three parallel sub-subagents (Edgar, Code Quality, Um-Actually), each reading its own agent file from `.forge/agents/`. It aggregates scores and writes the evaluation to `output_file`.

### HUMAN_INPUT
The pipeline needs a user decision.
- Present `question` and `options` to the user **verbatim** — no commentary, no recommendations, no editorializing
- Wait for their answer (option index)
- Run the `resume` command with their answer
- Do NOT add opinions like "I'd recommend..." or "this seems fine" — you are not a decision maker

### DONE
Pipeline complete.
- Report: PR URL from `pr_url`, branch from `branch`, summary from `summary`

### ERROR
Pipeline failed.
- Report: which `stage` failed, the `error` message
- Point user to the debug file at `debug_file`
- Suggest running the `action` command to retry

3. After handling the JSON output, run the `resume` command the orchestrator gave you. This starts the next stage.
4. Repeat the loop: run orchestrator → read JSON → do work → resume. Continue until you receive DONE or ERROR.

## Flags

- `--quick` — Lightweight mode: skip decomposition, minimal verification
- `--hotfix` — Fast-path: skip decomposition, auto T1, minimal checks
- `--issue <N>` — Fetch GitHub issue as input
- `--resume <id>` — Resume a halted or interrupted pipeline run
