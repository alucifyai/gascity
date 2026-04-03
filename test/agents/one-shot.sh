#!/bin/bash
# Bash agent: one-shot worker.
# Polls for assigned work, processes one bead, then exits.
#
# Required env vars (set by gc start):
#   GC_AGENT — this agent's name
#   GC_CITY  — path to the city directory
#   PATH     — must include gc and bd binaries

set -euo pipefail
cd "$GC_CITY"

while true; do
    # Step 1: Check for work assigned to this agent (in_progress status).
    # Uses bd list --json to get reliable machine-parseable output.
    assigned=$(bd list --json --assignee="$GC_AGENT" --status=in_progress --limit 1 2>/dev/null || true)

    # Extract bead ID from JSON — look for first "id" field.
    id=$(echo "$assigned" | { grep '"id"' || true; } | head -1 | sed 's/.*"id": *"\([^"]*\)".*/\1/')

    if [ -n "$id" ]; then
        # Step 2: Close the bead (simulates executing the work)
        bd close "$id" 2>/dev/null || true
        # Step 3: Done — one-shot agent exits after processing one bead
        exit 0
    fi

    sleep 0.5
done
