#!/usr/bin/env bash
set -euo pipefail

# soc2-checks.sh — SOC2 compliance scanning for forge projects
# Verifies audit logging, access controls, and change management practices

RESULTS_FILE=".forge/state/soc2-results.json"
PASS=0
FAIL=0
WARN=0
FINDINGS=()

mkdir -p "$(dirname "$RESULTS_FILE")"

# --- Helpers ---

log()  { echo "[soc2] $*"; }
finding() {
  local severity="$1" file="$2" line="$3" rule="$4" detail="$5"
  FINDINGS+=("{\"severity\":\"${severity}\",\"file\":\"${file}\",\"line\":${line},\"rule\":\"${rule}\",\"detail\":\"${detail}\"}")
  if [ "$severity" = "fail" ]; then
    FAIL=$((FAIL + 1))
  else
    WARN=$((WARN + 1))
  fi
}

# --- Check 1: Audit logging presence ---

log "Checking for audit logging infrastructure..."

HAS_AUDIT=false
AUDIT_PATTERNS=("auditLog\|audit_log\|AuditLog" "createAuditEntry\|logAudit\|writeAuditLog" "audit.*collection\|audit.*table")

for pattern in "${AUDIT_PATTERNS[@]}"; do
  if grep -rq "$pattern" --include='*.ts' --include='*.js' --include='*.py' . 2>/dev/null; then
    HAS_AUDIT=true
    break
  fi
done

if [ "$HAS_AUDIT" = false ]; then
  finding "fail" "project" 0 "no-audit-logging" "No audit logging infrastructure detected — SOC2 requires comprehensive audit trails"
else
  PASS=$((PASS + 1))
  log "  Audit logging infrastructure found"
fi

# --- Check 2: Auth on server endpoints ---

log "Checking server endpoints for authentication..."

UNPROTECTED=0
if [ -d "src/routes" ]; then
  while IFS= read -r file; do
    if ! grep -q 'locals\.\(user\|session\|auth\)\|getSession\|requireAuth\|authenticate\|verifyToken\|event\.locals\|isAuthenticated' "$file" 2>/dev/null; then
      finding "warn" "$file" 0 "endpoint-no-auth" "Server endpoint may lack authentication guard"
      UNPROTECTED=$((UNPROTECTED + 1))
    fi
  done < <(find . -path '*/routes/*' \( -name '+server.ts' -o -name '+server.js' \) -not -path '*/node_modules/*' 2>/dev/null | head -100)
fi

if [ "$UNPROTECTED" -eq 0 ]; then
  PASS=$((PASS + 1))
  log "  All server endpoints appear to have auth guards"
fi

# --- Check 3: No secrets in code ---

log "Checking for hardcoded secrets..."

SECRET_HITS=0
SECRET_PATTERNS=(
  'password\s*=\s*["\x27][^"\x27]\{8,\}'
  'api[_-]\?key\s*=\s*["\x27][A-Za-z0-9]\{20,\}'
  'secret\s*=\s*["\x27][^"\x27]\{8,\}'
  'PRIVATE.KEY.*BEGIN'
  'token\s*=\s*["\x27][A-Za-z0-9]\{30,\}'
)

for pattern in "${SECRET_PATTERNS[@]}"; do
  while IFS=: read -r file line _match; do
    [ -z "$file" ] && continue
    finding "fail" "$file" "$line" "hardcoded-secret" "Possible hardcoded secret — use environment variables or secret manager"
    SECRET_HITS=$((SECRET_HITS + 1))
  done < <(grep -rn "$pattern" --include='*.ts' --include='*.js' --include='*.py' --include='*.env*' . 2>/dev/null | grep -v node_modules | grep -v '\.test\.\|\.spec\.\|\.example\|\.sample' | head -50 || true)
done

if [ "$SECRET_HITS" -eq 0 ]; then
  PASS=$((PASS + 1))
  log "  No hardcoded secrets detected"
fi

# --- Check 4: Change management — branch protection evidence ---

log "Checking change management practices..."

if git rev-parse --is-inside-work-tree > /dev/null 2>&1; then
  # Check if there are direct commits to main (not merge commits)
  DIRECT_COMMITS=$(git log --first-parent --no-merges --oneline main 2>/dev/null | head -10 | wc -l | tr -d ' ')
  if [ "$DIRECT_COMMITS" -gt 3 ]; then
    finding "warn" ".git" 0 "direct-commits-to-main" "Found ${DIRECT_COMMITS} direct (non-merge) commits on main — use PRs for all changes"
  else
    PASS=$((PASS + 1))
    log "  Change management: main branch uses merge commits (PRs)"
  fi

  # Check for unsigned commits (optional but recommended)
  UNSIGNED=$(git log --format='%G?' -10 2>/dev/null | grep -c 'N' || true)
  if [ "$UNSIGNED" -gt 5 ]; then
    finding "warn" ".git" 0 "unsigned-commits" "Most recent commits are unsigned — consider requiring commit signing"
  fi
else
  finding "warn" "." 0 "no-git" "Not a git repository — cannot verify change management"
fi

# --- Check 5: Error handling — no internal details leaked ---

log "Checking for leaked internal details in error responses..."

ERROR_LEAK_PATTERNS=(
  'res\.\(status\|json\).*stack\|stack.*res\.\(status\|json\)'
  'response.*error\.message\|error\.message.*response'
  'catch.*res\..*500.*err\.\(message\|stack\)'
)

LEAK_HITS=0
for pattern in "${ERROR_LEAK_PATTERNS[@]}"; do
  while IFS=: read -r file line _match; do
    [ -z "$file" ] && continue
    finding "warn" "$file" "$line" "error-info-leak" "Error response may expose internal details — sanitize before sending to client"
    LEAK_HITS=$((LEAK_HITS + 1))
  done < <(grep -rn "$pattern" --include='*.ts' --include='*.js' . 2>/dev/null | grep -v node_modules | grep -v '\.test\.\|\.spec\.' | head -30 || true)
done

if [ "$LEAK_HITS" -eq 0 ]; then
  PASS=$((PASS + 1))
  log "  No obvious error info leaks detected"
fi

# --- Check 6: HTTPS enforcement ---

log "Checking for non-HTTPS API calls..."

HTTP_HITS=0
while IFS=: read -r file line _match; do
  [ -z "$file" ] && continue
  if echo "$_match" | grep -q 'localhost\|127\.0\.0\.1\|0\.0\.0\.0'; then
    continue
  fi
  finding "fail" "$file" "$line" "no-tls" "HTTP (non-TLS) API call — use HTTPS"
  HTTP_HITS=$((HTTP_HITS + 1))
done < <(grep -rn "fetch(['\"]http:" --include='*.ts' --include='*.js' --include='*.svelte' . 2>/dev/null | grep -v node_modules | grep -v '\.test\.\|\.spec\.' | head -50 || true)

if [ "$HTTP_HITS" -eq 0 ]; then
  PASS=$((PASS + 1))
  log "  All API calls use HTTPS"
fi

# --- Check 7: Dependency vulnerabilities ---

log "Checking for known dependency vulnerabilities..."

if [ -f "package-lock.json" ] || [ -f "yarn.lock" ]; then
  AUDIT_OUTPUT=$(npm audit --json 2>/dev/null || true)
  VULN_COUNT=$(echo "$AUDIT_OUTPUT" | jq '.metadata.vulnerabilities.high + .metadata.vulnerabilities.critical' 2>/dev/null || echo "0")
  if [ "$VULN_COUNT" -gt 0 ] 2>/dev/null; then
    finding "fail" "package.json" 0 "vulnerable-deps" "${VULN_COUNT} high/critical dependency vulnerabilities found — run npm audit fix"
  else
    PASS=$((PASS + 1))
    log "  No high/critical dependency vulnerabilities"
  fi
elif [ -f "requirements.txt" ]; then
  if command -v pip-audit > /dev/null 2>&1; then
    pip-audit -r requirements.txt --format json 2>/dev/null > /tmp/forge-pip-audit.json || true
    PY_VULNS=$(jq 'length' /tmp/forge-pip-audit.json 2>/dev/null || echo "0")
    if [ "$PY_VULNS" -gt 0 ]; then
      finding "fail" "requirements.txt" 0 "vulnerable-deps" "${PY_VULNS} Python dependency vulnerabilities found"
    else
      PASS=$((PASS + 1))
    fi
  else
    finding "warn" "requirements.txt" 0 "no-dep-audit" "pip-audit not installed — cannot check Python dependencies"
  fi
fi

# --- Check 8: Semgrep SOC2-relevant rules ---

if command -v semgrep > /dev/null 2>&1; then
  log "Running semgrep security rules..."
  semgrep --config "p/security-audit" --json --quiet . 2>/dev/null > /tmp/forge-semgrep-soc2.json || true

  SEMGREP_COUNT=$(jq '.results | length' /tmp/forge-semgrep-soc2.json 2>/dev/null || echo "0")
  if [ "$SEMGREP_COUNT" -gt 0 ]; then
    log "Semgrep found ${SEMGREP_COUNT} security findings"
    while IFS= read -r sg_finding; do
      FINDINGS+=("$sg_finding")
      FAIL=$((FAIL + 1))
    done < <(jq -c '.results[] | {severity:"fail",file:.path,line:.start.line,rule:.check_id,detail:.extra.message}' /tmp/forge-semgrep-soc2.json 2>/dev/null || true)
  else
    PASS=$((PASS + 1))
    log "  Semgrep: no security findings"
  fi
else
  log "Semgrep not installed — skipping advanced security rules (install with: pip install semgrep)"
fi

# --- Write results ---

FINDINGS_JSON=$(printf '%s,' "${FINDINGS[@]}" 2>/dev/null | sed 's/,$//')
[ -z "$FINDINGS_JSON" ] && FINDINGS_JSON=""

TOTAL=$((PASS + FAIL + WARN))

cat > "$RESULTS_FILE" <<EOF
{
  "addon": "compliance-soc2",
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "summary": {
    "total_checks": ${TOTAL},
    "pass": ${PASS},
    "fail": ${FAIL},
    "warn": ${WARN}
  },
  "findings": [${FINDINGS_JSON}]
}
EOF

log "Results written to ${RESULTS_FILE}"
log "Summary: ${PASS} passed, ${FAIL} failures, ${WARN} warnings"

[ "$FAIL" -eq 0 ] && exit 0 || exit 1
