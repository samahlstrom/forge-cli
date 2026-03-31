#!/usr/bin/env bash
# forge refine — measure: score a single run against criteria
# Usage: measure.sh <domain> <run-dir> [--fixture-dir=<path>]
# domain: "init" or "deliver"
# Outputs JSON: {"domain":"...", "gate_passed":bool, "scores":{...}, "total":N, "max":N}
set -euo pipefail

DOMAIN="${1:?Usage: measure.sh <init|deliver> <run-dir> [--fixture-dir=<path>]}"
RUN_DIR="${2:?}"
FIXTURE_DIR=""

for arg in "${@:3}"; do
  case "$arg" in
    --fixture-dir=*) FIXTURE_DIR="${arg#*=}" ;;
  esac
done

# ============================================================
# INIT DOMAIN
# ============================================================

measure_init() {
  local dir="${FIXTURE_DIR:-.}"
  local gate_passed=true

  # Gate 1: init.runs — did forge init exit 0?
  local runs=0
  if [[ -f "${RUN_DIR}/exit_code" ]]; then
    local ec
    ec=$(cat "${RUN_DIR}/exit_code")
    [[ "$ec" == "0" ]] && runs=1
  fi
  [[ $runs -eq 0 ]] && gate_passed=false

  # Gate 2: init.files_exist — were all expected files generated?
  local files_exist=0
  if [[ "$gate_passed" == "true" ]]; then
    files_exist=1
    local expected_files=(
      "forge.yaml"
      "CLAUDE.md"
      ".claude/settings.json"
      ".claude/skills/deliver/SKILL.md"
      ".forge/pipeline/intake.sh"
      ".forge/pipeline/classify.md"
      ".forge/pipeline/decompose.md"
      ".forge/pipeline/review-plan.md"
      ".forge/pipeline/execute.md"
      ".forge/pipeline/evaluate.md"
      ".forge/pipeline/verify.sh"
      ".forge/pipeline/deliver.sh"
      ".forge/agents/architect.md"
      ".forge/agents/edgar.md"
      ".forge/agents/code-quality.md"
      ".forge/agents/um-actually.md"
      ".forge/context/stack.md"
      ".forge/context/project.md"
      ".forge/hooks/pre-edit.sh"
      ".forge/hooks/session-start.sh"
    )
    for f in "${expected_files[@]}"; do
      if [[ ! -f "${dir}/${f}" ]]; then
        files_exist=0
        echo "MISSING: ${f}" >> "${RUN_DIR}/missing_files.log"
      fi
    done
  fi
  [[ $files_exist -eq 0 && "$gate_passed" == "true" ]] && gate_passed=false

  # Gate 3: init.valid_yaml — is forge.yaml parseable with required keys?
  local valid_yaml=0
  if [[ "$gate_passed" == "true" && -f "${dir}/forge.yaml" ]]; then
    # Check required top-level keys exist (grep-based, no python dependency)
    local yaml_ok=true
    for key in "version:" "project:" "commands:" "agents:" "verification:" "evaluation:" "pipeline:"; do
      if ! grep -q "^${key}" "${dir}/forge.yaml" 2>/dev/null; then
        echo "MISSING KEY: ${key}" >> "${RUN_DIR}/yaml_errors.log"
        yaml_ok=false
      fi
    done
    # Check nested keys
    if ! grep -q "name:" "${dir}/forge.yaml" 2>/dev/null; then yaml_ok=false; fi
    for cmd in "typecheck:" "lint:" "test:"; do
      if ! grep -q "${cmd}" "${dir}/forge.yaml" 2>/dev/null; then yaml_ok=false; fi
    done
    [[ "$yaml_ok" == "true" ]] && valid_yaml=1
  fi
  [[ $valid_yaml -eq 0 && "$gate_passed" == "true" ]] && gate_passed=false

  # Gate 4: init.valid_json — is settings.json valid?
  local valid_json=0
  if [[ "$gate_passed" == "true" && -f "${dir}/.claude/settings.json" ]]; then
    if jq -e '.permissions.allow and .hooks' "${dir}/.claude/settings.json" >/dev/null 2>&1; then
      valid_json=1
    fi
  fi
  [[ $valid_json -eq 0 && "$gate_passed" == "true" ]] && gate_passed=false

  # Quality checks (only if gates pass)
  local scripts_exec=0 no_template_vars=0 commands_detect=0 idempotent=0 fast=0

  if [[ "$gate_passed" == "true" ]]; then

    # Q1: scripts_executable
    scripts_exec=1
    while IFS= read -r sh_file; do
      if [[ ! -x "$sh_file" ]]; then
        scripts_exec=0
        break
      fi
    done < <(find "${dir}/.forge" -name "*.sh" 2>/dev/null)

    # Q2: no_template_vars — no leftover {{...}}
    no_template_vars=1
    if grep -rq '{{.*}}' "${dir}/.forge/" "${dir}/forge.yaml" "${dir}/CLAUDE.md" "${dir}/.claude/" 2>/dev/null; then
      no_template_vars=0
    fi

    # Q3: commands_detect — correct commands for project type
    # Read from run metadata what was expected, grep forge.yaml for matches
    if [[ -f "${RUN_DIR}/expected_commands.json" ]]; then
      commands_detect=1
      while IFS='=' read -r key expected; do
        # grep for the command key line and check if expected substring is present
        actual=$(grep "^  ${key}:" "${dir}/forge.yaml" 2>/dev/null | head -1 | sed 's/^[^:]*: *//' | sed 's/^"//' | sed 's/"$//')
        if [[ -z "$actual" || "$actual" != *"$expected"* ]]; then
          commands_detect=0
          break
        fi
      done < <(jq -r 'to_entries[] | "\(.key)=\(.value)"' "${RUN_DIR}/expected_commands.json" 2>/dev/null)
    fi

    # Q4: idempotent — checked by caller, read from run dir
    if [[ -f "${RUN_DIR}/idempotent" ]]; then
      idempotent=$(cat "${RUN_DIR}/idempotent")
    fi

    # Q5: fast
    if [[ -f "${RUN_DIR}/timing.json" ]]; then
      local duration budget=120
      duration=$(jq -r '.duration_seconds' "${RUN_DIR}/timing.json" 2>/dev/null || echo "9999")
      if (( $(echo "$duration < $budget" | bc -l) )); then
        fast=1
      fi
    fi
  fi

  local total=0
  [[ "$gate_passed" == "true" ]] && total=$((scripts_exec + no_template_vars + commands_detect + idempotent + fast))

  jq -n \
    --arg domain "init" \
    --argjson gate_passed "$([ "$gate_passed" == "true" ] && echo true || echo false)" \
    --argjson runs "$runs" \
    --argjson files_exist "$files_exist" \
    --argjson valid_yaml "$valid_yaml" \
    --argjson valid_json "$valid_json" \
    --argjson scripts_exec "$scripts_exec" \
    --argjson no_template_vars "$no_template_vars" \
    --argjson commands_detect "$commands_detect" \
    --argjson idempotent "$idempotent" \
    --argjson fast "$fast" \
    --argjson total "$total" \
    '{
      domain: $domain,
      gate_passed: $gate_passed,
      scores: {
        "init.runs": $runs,
        "init.files_exist": $files_exist,
        "init.valid_yaml": $valid_yaml,
        "init.valid_json": $valid_json,
        "init.scripts_executable": $scripts_exec,
        "init.no_template_vars": $no_template_vars,
        "init.commands_detect": $commands_detect,
        "init.idempotent": $idempotent,
        "init.fast": $fast
      },
      total: $total,
      max: 5
    }'
}

# ============================================================
# DELIVER DOMAIN
# ============================================================

measure_deliver() {
  local gate_passed=true

  # Gate 1: deliver.completes
  local completes=0
  if [[ -f "${RUN_DIR}/run-report.json" ]]; then
    if jq -e '.pr_url' "${RUN_DIR}/run-report.json" >/dev/null 2>&1; then
      completes=1
    fi
  fi
  [[ $completes -eq 0 ]] && gate_passed=false

  # Gate 2: deliver.builds
  local builds=0
  if [[ "$gate_passed" == "true" && -f "${RUN_DIR}/typecheck_exit_code" ]]; then
    [[ "$(cat "${RUN_DIR}/typecheck_exit_code")" == "0" ]] && builds=1
  fi
  [[ $builds -eq 0 && "$gate_passed" == "true" ]] && gate_passed=false

  # Gate 3: deliver.tests_pass
  local tests_pass=0
  if [[ "$gate_passed" == "true" && -f "${RUN_DIR}/test_exit_code" ]]; then
    [[ "$(cat "${RUN_DIR}/test_exit_code")" == "0" ]] && tests_pass=1
  fi
  [[ $tests_pass -eq 0 && "$gate_passed" == "true" ]] && gate_passed=false

  # Quality checks
  local correct=0 scoped=0 fast=0 clean=0

  if [[ "$gate_passed" == "true" ]]; then

    # Q1: fast
    if [[ -f "${RUN_DIR}/timing.json" ]]; then
      local duration budget=600
      duration=$(jq -r '.duration_seconds' "${RUN_DIR}/timing.json" 2>/dev/null || echo "9999")
      if (( $(echo "$duration < $budget" | bc -l) )); then
        fast=1
      fi
    fi

    # Q2: clean — no debug artifacts
    if [[ -f "${RUN_DIR}/diff.patch" ]]; then
      if ! grep -qE 'console\.log|debugger|TODO|FIXME|HACK' "${RUN_DIR}/diff.patch" 2>/dev/null; then
        clean=1
      fi
    fi

    # Q3+Q4: correct + scoped — LLM judge scores (written by caller)
    [[ -f "${RUN_DIR}/llm_correct" ]] && correct=$(cat "${RUN_DIR}/llm_correct")
    [[ -f "${RUN_DIR}/llm_scoped" ]] && scoped=$(cat "${RUN_DIR}/llm_scoped")
  fi

  local total=0
  [[ "$gate_passed" == "true" ]] && total=$((correct + scoped + fast + clean))

  jq -n \
    --arg domain "deliver" \
    --argjson gate_passed "$([ "$gate_passed" == "true" ] && echo true || echo false)" \
    --argjson completes "$completes" \
    --argjson builds "$builds" \
    --argjson tests_pass "$tests_pass" \
    --argjson correct "$correct" \
    --argjson scoped "$scoped" \
    --argjson fast "$fast" \
    --argjson clean "$clean" \
    --argjson total "$total" \
    '{
      domain: $domain,
      gate_passed: $gate_passed,
      scores: {
        "deliver.completes": $completes,
        "deliver.builds": $builds,
        "deliver.tests_pass": $tests_pass,
        "deliver.correct": $correct,
        "deliver.scoped": $scoped,
        "deliver.fast": $fast,
        "deliver.clean": $clean
      },
      total: $total,
      max: 4
    }'
}

# ============================================================
# DISPATCH
# ============================================================

case "$DOMAIN" in
  init)    measure_init ;;
  deliver) measure_deliver ;;
  *)
    echo "{\"error\":\"Unknown domain: ${DOMAIN}. Use 'init' or 'deliver'.\"}" >&2
    exit 1
    ;;
esac
