# deliver

> Intake, classify, and execute tracked work through the forge pipeline.

## Trigger

User runs `/deliver <description>` or `/deliver --flag`

## Process

1. Run: `bash .forge/pipeline/orchestrator.sh <user-input>`
2. Read the JSON output line. Branch on `status`:

### PAUSE
The pipeline needs LLM work.
- Read the file at `prompt_file` for instructions
- Read each context file listed in `context[]` from `.forge/context/`
- Execute the work described in the prompt
- Write your output to `output_file`
- Run the `resume` command to continue the pipeline

### HUMAN_INPUT
The pipeline needs a user decision.
- Present `question` and `options` to the user via AskUserQuestion
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

3. After handling the JSON output, if another orchestrator call is needed (PAUSE resume), repeat from step 1 with the resume command.
4. Continue until you receive DONE or ERROR.

## Flags

- `--quick` — Lightweight mode: skip decomposition, minimal verification
- `--hotfix` — Fast-path: skip decomposition, auto T1, minimal checks
- `--issue <N>` — Fetch GitHub issue as input
- `--resume <id>` — Resume a halted or interrupted pipeline run
