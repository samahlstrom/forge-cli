#!/usr/bin/env bash
set -euo pipefail

# dolt-setup.sh — Initialize Dolt database for bead storage
# Part of the beads-dolt-backend forge addon

DOLT_DIR=".forge/beads/dolt"
BEAD_STATE_SCRIPT=".forge/bead-state.sh"

# --- Helpers ---

log()  { echo "[dolt-setup] $*"; }
warn() { echo "[dolt-setup] WARN: $*" >&2; }
die()  { echo "[dolt-setup] ERROR: $*" >&2; exit 1; }

# --- Check prerequisites ---

log "Checking for Dolt installation..."

if ! command -v dolt > /dev/null 2>&1; then
  log "Dolt not found. Attempting to install..."

  if command -v brew > /dev/null 2>&1; then
    log "Installing via Homebrew..."
    brew install dolt
  elif command -v curl > /dev/null 2>&1; then
    log "Installing via install script..."
    sudo bash -c 'curl -L https://github.com/dolthub/dolt/releases/latest/download/install.sh | bash'
  else
    die "Cannot install Dolt automatically. Install manually: https://docs.dolthub.com/introduction/installation"
  fi

  # Verify installation
  if ! command -v dolt > /dev/null 2>&1; then
    die "Dolt installation failed. Install manually: https://docs.dolthub.com/introduction/installation"
  fi
fi

DOLT_VERSION=$(dolt version 2>/dev/null | head -1)
log "Found: ${DOLT_VERSION}"

# --- Initialize Dolt database ---

if [ -d "${DOLT_DIR}/.dolt" ]; then
  log "Dolt database already exists at ${DOLT_DIR}"
else
  log "Initializing Dolt database at ${DOLT_DIR}..."
  mkdir -p "${DOLT_DIR}"

  # Configure Dolt user if not already set
  if ! dolt config --global --get user.name > /dev/null 2>&1; then
    GIT_USER=$(git config user.name 2>/dev/null || echo "forge-user")
    GIT_EMAIL=$(git config user.email 2>/dev/null || echo "forge@local")
    dolt config --global --add user.name "${GIT_USER}"
    dolt config --global --add user.email "${GIT_EMAIL}"
    log "Configured Dolt user: ${GIT_USER} <${GIT_EMAIL}>"
  fi

  cd "${DOLT_DIR}"
  dolt init
  cd - > /dev/null

  log "Dolt database initialized"
fi

# --- Create beads table schema ---

log "Creating beads table schema..."

cd "${DOLT_DIR}"

# Create the beads table if it doesn't exist
dolt sql -q "
CREATE TABLE IF NOT EXISTS beads (
  id VARCHAR(64) PRIMARY KEY,
  title VARCHAR(255) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'open',
  branch VARCHAR(128),
  parent_id VARCHAR(64),
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  metadata JSON,
  INDEX idx_status (status),
  INDEX idx_branch (branch),
  INDEX idx_parent (parent_id)
);
" 2>/dev/null || log "  beads table already exists or schema unchanged"

# Create the bead_events table for history tracking
dolt sql -q "
CREATE TABLE IF NOT EXISTS bead_events (
  id VARCHAR(64) PRIMARY KEY,
  bead_id VARCHAR(64) NOT NULL,
  event_type VARCHAR(32) NOT NULL,
  actor VARCHAR(128),
  timestamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  data JSON,
  INDEX idx_bead_id (bead_id),
  INDEX idx_timestamp (timestamp),
  FOREIGN KEY (bead_id) REFERENCES beads(id)
);
" 2>/dev/null || log "  bead_events table already exists or schema unchanged"

# Create the bead_links table for relationships
dolt sql -q "
CREATE TABLE IF NOT EXISTS bead_links (
  source_id VARCHAR(64) NOT NULL,
  target_id VARCHAR(64) NOT NULL,
  link_type VARCHAR(32) NOT NULL DEFAULT 'related',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (source_id, target_id, link_type),
  FOREIGN KEY (source_id) REFERENCES beads(id),
  FOREIGN KEY (target_id) REFERENCES beads(id)
);
" 2>/dev/null || log "  bead_links table already exists or schema unchanged"

# Commit the schema
if dolt status | grep -q "Changes\|new table"; then
  dolt add .
  dolt commit -m "Initialize bead storage schema"
  log "Schema committed to Dolt"
else
  log "Schema already up to date"
fi

cd - > /dev/null

# --- Configure bead-state.sh for Dolt backend ---

log "Configuring bead-state.sh to use Dolt backend..."

if [ -f "${BEAD_STATE_SCRIPT}" ]; then
  # Check if already configured for dolt
  if grep -q 'BEAD_BACKEND=dolt\|BEAD_BACKEND="dolt"' "${BEAD_STATE_SCRIPT}" 2>/dev/null; then
    log "  bead-state.sh already configured for Dolt"
  else
    # Add Dolt backend configuration at the top of the script (after shebang)
    TEMP_FILE=$(mktemp)
    {
      head -1 "${BEAD_STATE_SCRIPT}"
      echo ""
      echo "# --- Dolt backend configuration (added by beads-dolt-backend addon) ---"
      echo "export BEAD_BACKEND=dolt"
      echo "export BEAD_DOLT_DIR=\"${DOLT_DIR}\""
      echo "# --- End Dolt configuration ---"
      echo ""
      tail -n +2 "${BEAD_STATE_SCRIPT}"
    } > "${TEMP_FILE}"
    mv "${TEMP_FILE}" "${BEAD_STATE_SCRIPT}"
    chmod +x "${BEAD_STATE_SCRIPT}"
    log "  bead-state.sh updated with Dolt backend config"
  fi
else
  # Create a minimal bead-state.sh with Dolt backend
  mkdir -p "$(dirname "${BEAD_STATE_SCRIPT}")"
  cat > "${BEAD_STATE_SCRIPT}" <<'BEAD_SCRIPT'
#!/usr/bin/env bash
set -euo pipefail

# bead-state.sh — Bead state management (Dolt backend)
# Generated by beads-dolt-backend addon

export BEAD_BACKEND=dolt
BEAD_SCRIPT

  # Append the DOLT_DIR (not using heredoc to expand variable)
  echo "export BEAD_DOLT_DIR=\"${DOLT_DIR}\"" >> "${BEAD_STATE_SCRIPT}"

  cat >> "${BEAD_STATE_SCRIPT}" <<'BEAD_SCRIPT'

COMMAND="${1:-help}"
shift || true

dolt_query() {
  cd "${BEAD_DOLT_DIR}" && dolt sql -q "$1" -r json 2>/dev/null && cd - > /dev/null
}

dolt_exec() {
  cd "${BEAD_DOLT_DIR}" && dolt sql -q "$1" 2>/dev/null && cd - > /dev/null
}

dolt_commit() {
  cd "${BEAD_DOLT_DIR}" && dolt add . && dolt commit -m "$1" 2>/dev/null && cd - > /dev/null
}

case "$COMMAND" in
  list)
    dolt_query "SELECT id, title, status, branch, created_at FROM beads ORDER BY updated_at DESC"
    ;;
  get)
    [ -z "${1:-}" ] && echo "Usage: bead-state.sh get <id>" && exit 1
    dolt_query "SELECT * FROM beads WHERE id = '$1'"
    ;;
  create)
    ID="${1:-$(uuidgen | tr '[:upper:]' '[:lower:]')}"
    TITLE="${2:-Untitled bead}"
    BRANCH="${3:-$(git branch --show-current 2>/dev/null || echo 'none')}"
    dolt_exec "INSERT INTO beads (id, title, status, branch) VALUES ('${ID}', '${TITLE}', 'open', '${BRANCH}')"
    dolt_exec "INSERT INTO bead_events (id, bead_id, event_type, actor) VALUES ('$(uuidgen | tr '[:upper:]' '[:lower:]')', '${ID}', 'created', '$(whoami)')"
    dolt_commit "Create bead: ${TITLE}"
    echo "${ID}"
    ;;
  update)
    [ -z "${1:-}" ] || [ -z "${2:-}" ] && echo "Usage: bead-state.sh update <id> <status>" && exit 1
    dolt_exec "UPDATE beads SET status = '$2', updated_at = CURRENT_TIMESTAMP WHERE id = '$1'"
    dolt_exec "INSERT INTO bead_events (id, bead_id, event_type, actor, data) VALUES ('$(uuidgen | tr '[:upper:]' '[:lower:]')', '$1', 'status_change', '$(whoami)', '{\"status\":\"$2\"}')"
    dolt_commit "Update bead $1: status -> $2"
    ;;
  history)
    [ -z "${1:-}" ] && echo "Usage: bead-state.sh history <id>" && exit 1
    dolt_query "SELECT * FROM bead_events WHERE bead_id = '$1' ORDER BY timestamp DESC"
    ;;
  diff)
    cd "${BEAD_DOLT_DIR}" && dolt diff "${1:-HEAD~1}" && cd - > /dev/null
    ;;
  sync)
    cd "${BEAD_DOLT_DIR}"
    if dolt remote -v 2>/dev/null | grep -q origin; then
      dolt push origin main 2>/dev/null || echo "Push failed — configure remote with: cd ${BEAD_DOLT_DIR} && dolt remote add origin <url>"
    else
      echo "No remote configured. Add one with: cd ${BEAD_DOLT_DIR} && dolt remote add origin <url>"
    fi
    cd - > /dev/null
    ;;
  help|*)
    echo "Usage: bead-state.sh <command> [args]"
    echo ""
    echo "Commands:"
    echo "  list              List all beads"
    echo "  get <id>          Get bead details"
    echo "  create [id] [title] [branch]  Create a new bead"
    echo "  update <id> <status>          Update bead status"
    echo "  history <id>      Show bead event history"
    echo "  diff [ref]        Show Dolt diff (versioned changes)"
    echo "  sync              Push to remote Dolt (if configured)"
    ;;
esac
BEAD_SCRIPT

  chmod +x "${BEAD_STATE_SCRIPT}"
  log "  Created ${BEAD_STATE_SCRIPT} with Dolt backend"
fi

# --- Add to .gitignore ---

GITIGNORE=".gitignore"
if [ -f "$GITIGNORE" ]; then
  if ! grep -q '.forge/beads/dolt' "$GITIGNORE" 2>/dev/null; then
    echo "" >> "$GITIGNORE"
    echo "# Dolt bead database (local state)" >> "$GITIGNORE"
    echo ".forge/beads/dolt/" >> "$GITIGNORE"
    log "Added .forge/beads/dolt/ to .gitignore"
  fi
fi

# --- Done ---

log "Setup complete. Dolt bead backend is ready."
log ""
log "Quick start:"
log "  bash ${BEAD_STATE_SCRIPT} create '' 'My first bead'"
log "  bash ${BEAD_STATE_SCRIPT} list"
log "  bash ${BEAD_STATE_SCRIPT} history <id>"
log ""
log "For team sync, configure a DoltHub remote:"
log "  cd ${DOLT_DIR} && dolt remote add origin <dolthub-url>"
log "  bash ${BEAD_STATE_SCRIPT} sync"
