#!/usr/bin/env bash
set -euo pipefail

# browser-smoke.sh — Run Playwright visual smoke tests at mobile + desktop viewports
# Part of the browser-testing forge addon

SCREENSHOT_DIR=".forge/state/screenshots"
DEV_PORT="${DEV_PORT:-5173}"
DEV_URL="http://localhost:${DEV_PORT}"
TIMEOUT=30
STARTED_SERVER=false
PASS=0
FAIL=0
ERRORS=()

mkdir -p "${SCREENSHOT_DIR}/mobile" "${SCREENSHOT_DIR}/desktop"

# --- Helpers ---

log()  { echo "[browser-smoke] $*"; }
warn() { echo "[browser-smoke] WARN: $*" >&2; }
fail() { echo "[browser-smoke] FAIL: $*" >&2; ERRORS+=("$*"); FAIL=$((FAIL + 1)); }
pass() { log "PASS: $*"; PASS=$((PASS + 1)); }

cleanup() {
  if [ "$STARTED_SERVER" = true ]; then
    log "Stopping dev server (PID ${DEV_PID:-unknown})..."
    kill "$DEV_PID" 2>/dev/null || true
    wait "$DEV_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

# --- Check Playwright ---

if ! npx playwright --version > /dev/null 2>&1; then
  log "Playwright not found. Installing chromium..."
  npx playwright install chromium || {
    fail "Could not install Playwright. Aborting."
    exit 1
  }
fi

# --- Dev Server ---

check_server() {
  curl -sf --max-time 3 "${DEV_URL}" > /dev/null 2>&1
}

if check_server; then
  log "Dev server already running at ${DEV_URL}"
else
  log "Starting dev server..."
  npm run dev > /tmp/forge-dev-server.log 2>&1 &
  DEV_PID=$!
  STARTED_SERVER=true

  # Wait for server to be ready
  for i in $(seq 1 "$TIMEOUT"); do
    if check_server; then
      log "Dev server ready after ${i}s"
      break
    fi
    if ! kill -0 "$DEV_PID" 2>/dev/null; then
      fail "Dev server process exited unexpectedly. Check /tmp/forge-dev-server.log"
      echo '{"pass":0,"fail":1,"errors":["Dev server failed to start"]}' > "${SCREENSHOT_DIR}/results.json"
      exit 1
    fi
    sleep 1
  done

  if ! check_server; then
    fail "Dev server did not respond within ${TIMEOUT}s"
    echo '{"pass":0,"fail":1,"errors":["Dev server timeout"]}' > "${SCREENSHOT_DIR}/results.json"
    exit 1
  fi
fi

# --- Determine pages to test ---

PAGES=("/")

# Add pages from changed route files if in a git repo
if git rev-parse --is-inside-work-tree > /dev/null 2>&1; then
  while IFS= read -r file; do
    # Extract route path from SvelteKit-style route files
    if [[ "$file" =~ src/routes/(.+)/\+page ]]; then
      route="/${BASH_REMATCH[1]}"
      # Skip parameterized routes (contain [...] or [[...]])
      if [[ ! "$route" =~ \[ ]]; then
        PAGES+=("$route")
      fi
    fi
  done < <(git diff --name-only HEAD~1 2>/dev/null || true)
fi

# Deduplicate
PAGES=($(printf '%s\n' "${PAGES[@]}" | sort -u))

log "Testing ${#PAGES[@]} page(s): ${PAGES[*]}"

# --- Run tests ---

run_viewport_test() {
  local page="$1"
  local label="$2"
  local width="$3"
  local height="$4"

  local safe_name
  safe_name=$(echo "$page" | sed 's|/|_|g; s|^_||')
  [ -z "$safe_name" ] && safe_name="home"

  local screenshot_path="${SCREENSHOT_DIR}/${label}/${safe_name}.png"

  log "Testing ${page} at ${label} (${width}x${height})..."

  local script="
    const { chromium } = require('playwright');
    (async () => {
      const browser = await chromium.launch();
      const context = await browser.newContext({
        viewport: { width: ${width}, height: ${height} }
      });
      const page = await context.newPage();
      try {
        const response = await page.goto('${DEV_URL}${page}', {
          waitUntil: 'networkidle',
          timeout: ${TIMEOUT}000
        });
        const status = response ? response.status() : 0;
        await page.screenshot({ path: '${screenshot_path}', fullPage: true });
        if (status >= 400) {
          console.error('HTTP ' + status);
          process.exit(2);
        }
        // Check for horizontal overflow (mobile)
        if (${width} <= 768) {
          const hasOverflow = await page.evaluate(() => {
            return document.documentElement.scrollWidth > document.documentElement.clientWidth;
          });
          if (hasOverflow) {
            console.error('OVERFLOW');
            process.exit(3);
          }
        }
        process.exit(0);
      } catch (err) {
        console.error(err.message);
        try { await page.screenshot({ path: '${screenshot_path}', fullPage: true }); } catch (_) {}
        process.exit(1);
      } finally {
        await browser.close();
      }
    })();
  "

  local exit_code=0
  node -e "$script" 2>/tmp/forge-browser-err.txt || exit_code=$?

  case $exit_code in
    0) pass "${page} @ ${label}" ;;
    2)
      local status
      status=$(cat /tmp/forge-browser-err.txt)
      fail "${page} @ ${label}: HTTP error (${status})"
      ;;
    3) fail "${page} @ ${label}: horizontal overflow detected on mobile" ;;
    *) fail "${page} @ ${label}: $(cat /tmp/forge-browser-err.txt)" ;;
  esac
}

for page in "${PAGES[@]}"; do
  run_viewport_test "$page" "mobile"  375  812
  run_viewport_test "$page" "desktop" 1440 900
done

# --- Report ---

TOTAL=$((PASS + FAIL))

cat > "${SCREENSHOT_DIR}/results.json" <<EOF
{
  "total": ${TOTAL},
  "pass": ${PASS},
  "fail": ${FAIL},
  "pages": ${#PAGES[@]},
  "errors": [$(printf '"%s",' "${ERRORS[@]}" 2>/dev/null | sed 's/,$//')]
}
EOF

log "Results: ${PASS}/${TOTAL} passed, ${FAIL} failed"
log "Screenshots saved to ${SCREENSHOT_DIR}/"
log "Results written to ${SCREENSHOT_DIR}/results.json"

[ "$FAIL" -eq 0 ] && exit 0 || exit 1
