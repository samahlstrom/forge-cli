#!/usr/bin/env bash
set -euo pipefail

# hipaa-checks.sh — HIPAA compliance scanning for forge projects
# Checks for PHI leaks, auth gaps, and encryption issues

RESULTS_FILE=".forge/state/hipaa-results.json"
PASS=0
FAIL=0
WARN=0
FINDINGS=()

mkdir -p "$(dirname "$RESULTS_FILE")"

# --- Helpers ---

log()  { echo "[hipaa] $*"; }
finding() {
  local severity="$1" file="$2" line="$3" rule="$4" detail="$5"
  FINDINGS+=("{\"severity\":\"${severity}\",\"file\":\"${file}\",\"line\":${line},\"rule\":\"${rule}\",\"detail\":\"${detail}\"}")
  if [ "$severity" = "fail" ]; then
    FAIL=$((FAIL + 1))
  else
    WARN=$((WARN + 1))
  fi
}

# --- Check 1: PHI in log statements ---

log "Checking for PHI in log statements..."

# Patterns that suggest logging PHI fields
PHI_LOG_PATTERNS=(
  'console\.\(log\|error\|warn\|info\|debug\).*\(patient\|ssn\|dob\|dateOfBirth\|diagnosis\|mrn\|medicalRecord\|socialSecurity\)'
  'logger\.\(info\|warn\|error\|debug\).*\(patient\|ssn\|dob\|dateOfBirth\|diagnosis\|mrn\|medicalRecord\|socialSecurity\)'
)

for pattern in "${PHI_LOG_PATTERNS[@]}"; do
  while IFS=: read -r file line _match; do
    [ -z "$file" ] && continue
    finding "fail" "$file" "$line" "phi-in-logs" "Potential PHI logged — review this log statement"
  done < <(grep -rn "$pattern" --include='*.ts' --include='*.js' --include='*.svelte' --include='*.py' . 2>/dev/null | head -50 || true)
done

# --- Check 2: PHI in URLs / route params ---

log "Checking for PHI in URL patterns..."

URL_PHI_PATTERNS=(
  'patient[Ii]d.*params\|params.*patient[Ii]d'
  'ssn.*query\|query.*ssn'
  'dob.*searchParams\|searchParams.*dob'
  '\$page\.params.*\(patient\|ssn\|dob\|mrn\)'
)

for pattern in "${URL_PHI_PATTERNS[@]}"; do
  while IFS=: read -r file line _match; do
    [ -z "$file" ] && continue
    finding "fail" "$file" "$line" "phi-in-url" "Potential PHI in URL parameter"
  done < <(grep -rn "$pattern" --include='*.ts' --include='*.js' --include='*.svelte' . 2>/dev/null | head -50 || true)
done

# --- Check 3: PHI in browser storage ---

log "Checking for PHI in browser storage usage..."

STORAGE_PATTERNS=(
  'localStorage\.set.*\(patient\|ssn\|dob\|diagnosis\|mrn\)'
  'sessionStorage\.set.*\(patient\|ssn\|dob\|diagnosis\|mrn\)'
  'indexedDB.*\(patient\|ssn\|dob\|diagnosis\|mrn\)'
)

for pattern in "${STORAGE_PATTERNS[@]}"; do
  while IFS=: read -r file line _match; do
    [ -z "$file" ] && continue
    finding "fail" "$file" "$line" "phi-in-storage" "Potential PHI in browser storage"
  done < <(grep -rn "$pattern" --include='*.ts' --include='*.js' --include='*.svelte' . 2>/dev/null | head -50 || true)
done

# --- Check 4: Unprotected endpoints ---

log "Checking for endpoints without auth guards..."

# Look for API endpoints / server routes missing auth checks
if [ -d "src/routes" ]; then
  while IFS= read -r file; do
    if ! grep -q 'locals\.\(user\|session\|auth\)\|getSession\|requireAuth\|authenticate\|verifyToken\|event\.locals' "$file" 2>/dev/null; then
      finding "warn" "$file" 0 "no-auth-guard" "Server endpoint may lack authentication — verify manually"
    fi
  done < <(find . -path '*/routes/*' \( -name '+server.ts' -o -name '+server.js' \) -not -path '*/node_modules/*' 2>/dev/null | head -100)
fi

# --- Check 5: Wildcard Firestore rules ---

log "Checking Firestore security rules..."

RULES_FILES=$(find . -name 'firestore.rules' -o -name '*.rules' -not -path '*/node_modules/*' 2>/dev/null || true)
for rules_file in $RULES_FILES; do
  while IFS=: read -r _ line _match; do
    [ -z "$line" ] && continue
    finding "fail" "$rules_file" "$line" "wildcard-rules" "Overly permissive Firestore rule — never allow unrestricted access to PHI"
  done < <(grep -n 'allow.*:.*if true\|allow.*:.*if request\.auth != null' "$rules_file" 2>/dev/null || true)
done

# --- Check 6: HTTP (non-TLS) API calls ---

log "Checking for non-HTTPS API calls..."

while IFS=: read -r file line _match; do
  [ -z "$file" ] && continue
  # Skip localhost and test files
  if echo "$_match" | grep -q 'localhost\|127\.0\.0\.1\|0\.0\.0\.0'; then
    continue
  fi
  finding "fail" "$file" "$line" "no-tls" "HTTP (non-TLS) API call detected — use HTTPS"
done < <(grep -rn "fetch(['\"]http:" --include='*.ts' --include='*.js' --include='*.svelte' . 2>/dev/null | grep -v node_modules | grep -v '\.test\.' | head -50 || true)

# --- Check 7: Secrets in code ---

log "Checking for hardcoded secrets..."

SECRET_PATTERNS=(
  'password\s*=\s*["\x27][^"\x27]\{8,\}'
  'api[_-]\?key\s*=\s*["\x27][A-Za-z0-9]\{20,\}'
  'secret\s*=\s*["\x27][^"\x27]\{8,\}'
  'PRIVATE.KEY'
)

for pattern in "${SECRET_PATTERNS[@]}"; do
  while IFS=: read -r file line _match; do
    [ -z "$file" ] && continue
    finding "warn" "$file" "$line" "hardcoded-secret" "Possible hardcoded secret — use environment variables or secret manager"
  done < <(grep -rn "$pattern" --include='*.ts' --include='*.js' --include='*.svelte' --include='*.py' --include='*.env*' . 2>/dev/null | grep -v node_modules | grep -v '\.test\.' | grep -v '\.example' | head -50 || true)
done

# --- Check 8: Semgrep HIPAA rules (if available) ---

if command -v semgrep > /dev/null 2>&1; then
  log "Running semgrep HIPAA rules..."
  semgrep --config "p/hipaa" --json --quiet . 2>/dev/null > /tmp/forge-semgrep-hipaa.json || true

  SEMGREP_COUNT=$(jq '.results | length' /tmp/forge-semgrep-hipaa.json 2>/dev/null || echo "0")
  if [ "$SEMGREP_COUNT" -gt 0 ]; then
    log "Semgrep found ${SEMGREP_COUNT} HIPAA findings"
    FAIL=$((FAIL + SEMGREP_COUNT))
    # Merge semgrep findings
    while IFS= read -r sg_finding; do
      FINDINGS+=("$sg_finding")
    done < <(jq -c '.results[] | {severity:"fail",file:.path,line:.start.line,rule:.check_id,detail:.extra.message}' /tmp/forge-semgrep-hipaa.json 2>/dev/null || true)
  fi
else
  log "Semgrep not installed — skipping advanced HIPAA rules (install with: pip install semgrep)"
fi

# --- Passed checks ---

# Count passes for checks that found no issues
TOTAL_CHECKS=8
PASS=$((TOTAL_CHECKS - (FAIL > 0 ? 1 : 0) - (WARN > 0 ? 1 : 0)))
[ "$PASS" -lt 0 ] && PASS=0

# --- Write results ---

FINDINGS_JSON=$(printf '%s,' "${FINDINGS[@]}" 2>/dev/null | sed 's/,$//')
[ -z "$FINDINGS_JSON" ] && FINDINGS_JSON=""

cat > "$RESULTS_FILE" <<EOF
{
  "addon": "compliance-hipaa",
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "summary": {
    "total_checks": ${TOTAL_CHECKS},
    "pass": $((TOTAL_CHECKS - (FAIL > 0 ? 1 : 0) - (WARN > 0 ? 1 : 0))),
    "fail": ${FAIL},
    "warn": ${WARN}
  },
  "findings": [${FINDINGS_JSON}]
}
EOF

log "Results written to ${RESULTS_FILE}"
log "Summary: ${FAIL} failures, ${WARN} warnings"

[ "$FAIL" -eq 0 ] && exit 0 || exit 1
