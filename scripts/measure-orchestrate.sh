#!/usr/bin/env bash
# Measure: does /deliver orchestrate a single task through classify → decompose → review?
# Sets up a temp repo, gives it a task, checks each stage works.
set -euo pipefail

WORK_DIR="/tmp/forge-test-$(date +%s)"
mkdir -p "$WORK_DIR"
trap "rm -rf $WORK_DIR" EXIT

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
FORGE_BIN="${SCRIPT_DIR}/../forge-test-bin"
if [ ! -x "$FORGE_BIN" ]; then
  FORGE_BIN=$(which forge 2>/dev/null || echo "forge")
fi

cd "$WORK_DIR"

# 1. Set up a minimal test repo
git init -q
cat > main.go << 'GOEOF'
package main
import "fmt"
func main() { fmt.Println("hello") }
GOEOF
go mod init testproject 2>/dev/null
git add -A && git commit -q -m "init"

# 2. Init forge harness
$FORGE_BIN init --yes --preset go --force >/dev/null 2>&1
git add -A && git commit -q -m "forge init" 2>/dev/null

# 3. Initialize beads
bd init 2>/dev/null || true

# Helper: extract last JSON object from mixed output
extract_json() {
  # jq will skip non-JSON lines when using --slurp with inputs
  echo "$1" | python3 -c "
import sys, json
text = sys.stdin.read()
# Find last { ... } block
depth = 0
start = -1
best_start = -1
best_end = -1
for i, c in enumerate(text):
    if c == '{':
        if depth == 0:
            start = i
        depth += 1
    elif c == '}':
        depth -= 1
        if depth == 0 and start >= 0:
            best_start = start
            best_end = i + 1
if best_start >= 0:
    print(text[best_start:best_end])
" 2>/dev/null || true
}

# 4. Test each stage
orchestrate_ok=0
intake_ok=0
classify_ok=0
decompose_ok=0
review_ok=0

TASK_DESC="Add a /health HTTP endpoint that returns JSON status ok with a 200 status code"

# --- INTAKE ---
intake_output=$(bash .forge/pipeline/intake.sh "$TASK_DESC" 2>/dev/null) || true
intake_title=$(echo "$intake_output" | jq -r '.title // empty' 2>/dev/null || true)
if [ -n "$intake_title" ]; then
  intake_ok=1
fi

# --- ORCHESTRATOR (intake → classify PAUSE) ---
orch_raw=$(bash .forge/pipeline/orchestrator.sh "$TASK_DESC" 2>/dev/null) || true
orch_json=$(extract_json "$orch_raw")
pause_status=$(echo "$orch_json" | jq -r '.status // empty' 2>/dev/null || true)
pause_task=$(echo "$orch_json" | jq -r '.task // empty' 2>/dev/null || true)
output_file=$(echo "$orch_json" | jq -r '.output_file // empty' 2>/dev/null || true)
resume_cmd=$(echo "$orch_json" | jq -r '.resume // empty' 2>/dev/null || true)

if [ "$pause_status" = "PAUSE" ] && [ "$pause_task" = "classify" ]; then
  classify_ok=1
  orchestrate_ok=1

  task_id=$(echo "$resume_cmd" | sed 's/.*--resume //' | awk '{print $1}' || true)

  if [ -n "$task_id" ] && [ -n "$output_file" ]; then
    mkdir -p "$(dirname "$output_file")"
    echo '{"tier":"T2","reason":"Adds HTTP endpoint"}' > "$output_file"

    # --- RESUME → post-classify → decompose PAUSE ---
    dec_raw=$(bash .forge/pipeline/orchestrator.sh --resume "$task_id" --stage post-classify 2>/dev/null) || true
    dec_json=$(extract_json "$dec_raw")
    dec_status=$(echo "$dec_json" | jq -r '.status // empty' 2>/dev/null || true)
    dec_task=$(echo "$dec_json" | jq -r '.task // empty' 2>/dev/null || true)
    dec_output_file=$(echo "$dec_json" | jq -r '.output_file // empty' 2>/dev/null || true)

    if [ "$dec_status" = "PAUSE" ] && [ "$dec_task" = "decompose" ]; then
      decompose_ok=1

      if [ -n "$dec_output_file" ]; then
        mkdir -p "$(dirname "$dec_output_file")"
        cat > "$dec_output_file" << 'DECEOF'
{"analysis":{"files_affected":["main.go"],"dependency_graph":"none","risk_notes":"simple"},"subtasks":[{"id":"sub-1","title":"Add /health endpoint","agent":"code","files":["main.go"],"dependsOn":[],"verification":"go build","instructions":"Add health endpoint"}],"waves":[{"id":"wave-1","subtasks":["sub-1"],"gate":"typecheck"}],"verification_plan":{"after_all_waves":"go build","manual_checks":[]}}
DECEOF

        # --- RESUME → review-plan PAUSE ---
        rev_raw=$(bash .forge/pipeline/orchestrator.sh --resume "$task_id" --stage review-plan 2>/dev/null) || true
        rev_json=$(extract_json "$rev_raw")
        rev_status=$(echo "$rev_json" | jq -r '.status // empty' 2>/dev/null || true)
        rev_task=$(echo "$rev_json" | jq -r '.task // empty' 2>/dev/null || true)

        if [ "$rev_status" = "PAUSE" ] && [ "$rev_task" = "review-plan" ]; then
          review_ok=1
        fi
      fi
    fi
  fi
fi

total=$((orchestrate_ok + intake_ok + classify_ok + decompose_ok + review_ok))
score=$(echo "scale=4; $total / 5" | bc)

cat <<EOJSON
{"orchestrate_ok": ${orchestrate_ok}, "intake_ok": ${intake_ok}, "classify_ok": ${classify_ok}, "decompose_ok": ${decompose_ok}, "review_ok": ${review_ok}, "pipeline_score": ${score}, "stages_passed": ${total}}
EOJSON
