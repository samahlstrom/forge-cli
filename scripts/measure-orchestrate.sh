#!/usr/bin/env bash
# Measure: does the full /deliver pipeline orchestrate a task end-to-end?
# Spins up a temp repo, feeds a task, simulates agent outputs at each PAUSE,
# and verifies every stage transition works.
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

# 1. Set up a minimal Go repo
git init -q
cat > main.go << 'GOEOF'
package main
import (
	"encoding/json"
	"fmt"
	"net/http"
)
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
func main() {
	http.HandleFunc("/health", healthHandler)
	fmt.Println("listening on :8080")
	http.ListenAndServe(":8080", nil)
}
GOEOF
go mod init testproject 2>/dev/null
git add -A && git commit -q -m "init"

# 2. Init forge harness
$FORGE_BIN init --yes --preset go --force >/dev/null 2>&1
# Patch verify commands to work without golangci-lint
sed -i '' 's/golangci-lint run/go vet .\/.../' .forge/pipeline/verify.sh 2>/dev/null || \
  sed -i 's/golangci-lint run/go vet .\/.../' .forge/pipeline/verify.sh 2>/dev/null || true
git add -A && git commit -q -m "forge init" 2>/dev/null

# 3. Initialize beads
bd init 2>/dev/null || true

# Helper: extract last JSON object from mixed output
extract_json() {
  python3 -c "
import sys
text = sys.stdin.read()
depth = 0; start = -1; best_s = -1; best_e = -1
for i, c in enumerate(text):
    if c == '{':
        if depth == 0: start = i
        depth += 1
    elif c == '}':
        depth -= 1
        if depth == 0 and start >= 0:
            best_s = start; best_e = i + 1
if best_s >= 0: print(text[best_s:best_e])
" <<< "$1" 2>/dev/null || true
}

# --- Results ---
intake_ok=0
classify_ok=0
decompose_ok=0
review_ok=0
review_check_ok=0
execute_ok=0
verify_ok=0
evaluate_ok=0
eval_check_ok=0

TASK_DESC="Add a /health HTTP endpoint that returns JSON status ok with a 200 status code"
ORCH=".forge/pipeline/orchestrator.sh"

# ── STAGE 1: INTAKE ──
intake_output=$(bash .forge/pipeline/intake.sh "$TASK_DESC" 2>/dev/null) || true
if echo "$intake_output" | jq -e '.title' >/dev/null 2>&1; then
  intake_ok=1
fi

# ── STAGE 2: ORCHESTRATOR → CLASSIFY PAUSE ──
raw=$(bash "$ORCH" "$TASK_DESC" 2>/dev/null) || true
json=$(extract_json "$raw")
status=$(echo "$json" | jq -r '.status // empty' 2>/dev/null || true)
task=$(echo "$json" | jq -r '.task // empty' 2>/dev/null || true)

if [ "$status" = "PAUSE" ] && [ "$task" = "classify" ]; then
  classify_ok=1
  output_file=$(echo "$json" | jq -r '.output_file' 2>/dev/null)
  task_id=$(echo "$json" | jq -r '.resume' 2>/dev/null | sed 's/.*--resume //' | awk '{print $1}')

  # Simulate classify agent
  mkdir -p "$(dirname "$output_file")"
  echo '{"tier":"T2","reason":"HTTP endpoint — behavioral change"}' > "$output_file"

  # ── STAGE 3: POST-CLASSIFY → DECOMPOSE PAUSE ──
  raw=$(bash "$ORCH" --resume "$task_id" --stage post-classify 2>/dev/null) || true
  json=$(extract_json "$raw")
  status=$(echo "$json" | jq -r '.status // empty' 2>/dev/null || true)
  task=$(echo "$json" | jq -r '.task // empty' 2>/dev/null || true)

  if [ "$status" = "PAUSE" ] && [ "$task" = "decompose" ]; then
    decompose_ok=1
    dec_output_file=$(echo "$json" | jq -r '.output_file' 2>/dev/null)

    # Simulate architect agent
    mkdir -p "$(dirname "$dec_output_file")"
    cat > "$dec_output_file" << 'DECEOF'
{"analysis":{"files_affected":["main.go"],"dependency_graph":"none","risk_notes":"simple endpoint"},"subtasks":[{"id":"sub-1","title":"Add /health endpoint","agent":"code","files":["main.go"],"dependsOn":[],"verification":"go vet ./...","instructions":"Add net/http handler returning JSON {status:ok}"}],"waves":[{"id":"wave-1","subtasks":["sub-1"],"gate":"typecheck"}],"verification_plan":{"after_all_waves":"go vet ./...","manual_checks":[]}}
DECEOF

    # ── STAGE 4: REVIEW-PLAN PAUSE ──
    raw=$(bash "$ORCH" --resume "$task_id" --stage review-plan 2>/dev/null) || true
    json=$(extract_json "$raw")
    status=$(echo "$json" | jq -r '.status // empty' 2>/dev/null || true)
    task=$(echo "$json" | jq -r '.task // empty' 2>/dev/null || true)

    if [ "$status" = "PAUSE" ] && [ "$task" = "review-plan" ]; then
      review_ok=1
      rev_output_file=$(echo "$json" | jq -r '.output_file' 2>/dev/null)

      # Simulate review agent — approve the plan
      mkdir -p "$(dirname "$rev_output_file")"
      cat > "$rev_output_file" << 'REVEOF'
{"task_id":"test","review":{"completeness":{"score":0.9,"issues":[]},"file_safety":{"score":1.0,"issues":[]},"dependencies":{"score":1.0,"issues":[]},"verification":{"score":0.8,"issues":[]},"scope":{"score":1.0,"issues":[]},"clarity":{"score":0.9,"issues":[]}},"composite_score":0.93,"verdict":"approve","revision_instructions":null,"summary":"Plan is well-scoped for a simple endpoint addition."}
REVEOF

      # ── STAGE 5: REVIEW-PLAN-CHECK → EXECUTE PAUSE ──
      raw=$(bash "$ORCH" --resume "$task_id" --stage review-plan-check 2>/dev/null) || true
      json=$(extract_json "$raw")
      status=$(echo "$json" | jq -r '.status // empty' 2>/dev/null || true)
      task=$(echo "$json" | jq -r '.task // empty' 2>/dev/null || true)

      if [ "$status" = "PAUSE" ] && [ "$task" = "execute" ]; then
        review_check_ok=1
        execute_ok=1
        exec_output_file=$(echo "$json" | jq -r '.output_file' 2>/dev/null)

        # Simulate execution agent — write the execution report
        mkdir -p "$(dirname "$exec_output_file")"
        cat > "$exec_output_file" << 'EXECEOF'
{"status":"complete","waves_executed":1,"subtasks_completed":["sub-1"],"subtasks_deferred":[],"files_modified":["main.go"],"verification":{"typecheck":"passed","lint":"passed","test":"passed"}}
EXECEOF

        # ── STAGE 6: VERIFY ──
        # verify.sh runs actual go vet / go test — our repo compiles, so this should pass
        raw=$(bash "$ORCH" --resume "$task_id" --stage verify 2>/dev/null) || true
        json=$(extract_json "$raw")
        status=$(echo "$json" | jq -r '.status // empty' 2>/dev/null || true)
        task=$(echo "$json" | jq -r '.task // empty' 2>/dev/null || true)

        if [ "$status" = "PAUSE" ] && [ "$task" = "evaluate" ]; then
          verify_ok=1
          evaluate_ok=1
          eval_output_file=$(echo "$json" | jq -r '.output_file' 2>/dev/null)

          # Simulate evaluator agents — pass verdict
          mkdir -p "$(dirname "$eval_output_file")"
          cat > "$eval_output_file" << 'EVALEOF'
{"verdict":"pass","composite_score":0.85,"evaluators":{"edgar":{"score":0.9,"findings":[]},"code_quality":{"score":0.8,"findings":[]},"um_actually":{"score":0.85,"findings":[]}},"summary":"Code meets requirements. Clean implementation."}
EVALEOF

          # ── STAGE 7: EVALUATE-CHECK ──
          # On pass, orchestrator calls stage_deliver which needs git push + gh.
          # We can't test delivery without a remote. Instead, run evaluate-check
          # directly to confirm it returns "pass".
          eval_check_raw=$(bash -c '
            source "'$ORCH'" 2>/dev/null
          ' 2>/dev/null) || true

          # Simpler: just call the orchestrator and check the output.
          # It will try stage_deliver and fail (no remote), but if evaluate-check
          # passed, we'll see the deliver error (not an evaluate error).
          raw=$(bash "$ORCH" --resume "$task_id" --stage evaluate-check 2>/dev/null) || true
          json=$(extract_json "$raw")
          err_stage=$(echo "$json" | jq -r '.stage // empty' 2>/dev/null || true)

          # If error is from "deliver" stage, that means evaluate-check PASSED
          # and the pipeline progressed to delivery (which fails without a remote)
          if [ "$err_stage" = "deliver" ]; then
            eval_check_ok=1
          fi
          # Also pass if raw contains "pass" (from the echo in evaluate_check)
          if echo "$raw" | grep -q "^pass" 2>/dev/null; then
            eval_check_ok=1
          fi
        fi
      fi
    fi
  fi
fi

# Score: 9 stages
total=$((intake_ok + classify_ok + decompose_ok + review_ok + review_check_ok + execute_ok + verify_ok + evaluate_ok + eval_check_ok))
score=$(echo "scale=4; $total / 9" | bc)

cat <<EOJSON
{"intake_ok": ${intake_ok}, "classify_ok": ${classify_ok}, "decompose_ok": ${decompose_ok}, "review_ok": ${review_ok}, "review_check_ok": ${review_check_ok}, "execute_ok": ${execute_ok}, "verify_ok": ${verify_ok}, "evaluate_ok": ${evaluate_ok}, "eval_check_ok": ${eval_check_ok}, "pipeline_score": ${score}, "stages_passed": ${total}}
EOJSON
