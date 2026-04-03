#!/bin/bash
# Bash agent: loop worker.
# Continuously drains the backlog: check assigned → claim from ready → close → repeat.
#
# Required env vars (set by gc start):
#   GC_AGENT — this agent's name
#   GC_CITY  — path to the city directory
#   PATH     — must include gc and bd binaries

set -euo pipefail
cd "$GC_CITY"

while true; do
    # Step 1: Check for work already assigned to this agent.
    assigned=$(bd list --json --assignee="$GC_AGENT" --status=in_progress --limit 1 2>/dev/null || true)
    id=$(echo "$assigned" | { grep '"id"' || true; } | head -1 | sed 's/.*"id": *"\([^"]*\)".*/\1/')

    if [ -n "$id" ]; then
        # Step 2: Close the bead (simulates executing the work)
        bd close "$id" 2>/dev/null || true
        continue
    fi

    # Step 3: Check for available work in ready queue.
    ready=$(bd ready --json --limit 1 2>/dev/null || true)
    ready_id=$(echo "$ready" | { grep '"id"' || true; } | head -1 | sed 's/.*"id": *"\([^"]*\)".*/\1/')

    if [ -n "$ready_id" ]; then
        # Step 4: Claim the bead (assign to self and set in_progress).
        # Note: bd update --claim sets assignee to beads.role which may be
        # unconfigured. Use explicit --assignee + --status instead.
        bd update "$ready_id" --assignee "$GC_AGENT" --status in_progress 2>/dev/null || true
        # Will process on next iteration (now assigned)
        continue
    fi

    # No work available — keep polling
    sleep 0.5
done
