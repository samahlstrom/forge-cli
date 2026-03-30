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
- Read the file at `prompt_file` — these are your instructions for this stage
- Read each context file listed in `context[]`
- Execute the work described in the prompt file **exactly as specified**
- Write your output to `output_file` in the format the prompt file specifies
- Run the `resume` command to hand control back to the orchestrator

**For decompose pauses**: You are acting as the architect agent. Read `.forge/agents/architect.md` for instructions. Break the task into subtasks with waves, dependencies, and file assignments. Output JSON to the output file.

**For review-plan pauses**: You are acting as the plan reviewer. Read `.forge/pipeline/review-plan.md` for instructions. Evaluate the decomposition and output your verdict.

**For execute pauses**: You are the execution dispatcher. Launch subagents for each subtask per wave, verify between waves. Follow `.forge/pipeline/execute.md` exactly.

**For evaluate pauses**: Launch the three evaluator agents (Edgar, Code Quality, Um-Actually) in parallel as subagents. Each reads its own agent file from `.forge/agents/`. Aggregate their results into the evaluation output format. If the verdict is "revise", the resume command will loop back to execution with the revision brief.

### HUMAN_INPUT
The pipeline needs a user decision.
- Present `question` and `options` to the user
- Get their answer (option index)
- Run the `resume` command with their answer

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
