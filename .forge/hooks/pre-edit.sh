#!/bin/bash
# .forge/hooks/pre-edit.sh — PreToolUse hook for Edit|Write
# Blocks file edits without an active (in-progress) bd task

INPUT=$(cat)
FILE_PATH=$(echo "$INPUT" | jq -r '.file_path // .path // empty' 2>/dev/null)

# If we can't determine the file path, allow (fail open)
if [ -z "$FILE_PATH" ]; then
	exit 0
fi

# Allow harness files, docs, and config — these don't need tracking
case "$FILE_PATH" in
	*.md|*.yaml|*.yml|*.txt|*.gitignore|*.env*) exit 0 ;;
	.forge/*|.claude/*|.beads/*|forge.yaml|CLAUDE.md) exit 0 ;;
	package.json|package-lock.json|tsconfig.json) exit 0 ;;
	pyproject.toml|requirements.txt|go.mod|go.sum) exit 0 ;;
	Cargo.toml|Cargo.lock) exit 0 ;;
esac

# Check if bd is available
if ! command -v bd &>/dev/null; then
	exit 0
fi

# Check if tracking is enforced
ENFORCE=$(grep -oP 'enforce_tracking:\s*\K\w+' forge.yaml 2>/dev/null || echo "true")
if [ "$ENFORCE" != "true" ]; then
	exit 0
fi

# Check for in-progress tasks
IN_PROGRESS=$(bd list --status in_progress --json 2>/dev/null)
if [ -z "$IN_PROGRESS" ] || [ "$IN_PROGRESS" = "[]" ]; then
	echo '{"decision":"block","reason":"No active task. Run /deliver to start tracked work, or use /deliver --quick for a lightweight change."}'
	exit 0
fi

# Allow the edit
exit 0
