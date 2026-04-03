# Docker Integration Tests Fix Report

## Overview

Integration tests (`go test -tags integration ./test/integration/...`) were failing
when run inside the Docker testing container (`Dockerfile.testing`). Tests cannot
run on Mac — they require Linux (Docker). This document catalogs all fixes applied
across 18 commits (`6e674c7e..4c4ab6db`) to make the tests pass.

**Environment:** Docker container built from `./Dockerfile.testing`, run with:
```bash
docker build -f Dockerfile.testing -t gc-test .
docker run --rm -it -v /var/run/docker.sock:/var/run/docker.sock gc-test bash
```

**Test command pattern:**
```bash
go test -tags integration -timeout 20m ./test/integration/... -v -run <TestName>
```

---

## Commit History

| Commit | Summary |
|--------|---------|
| `6e674c7e` | Added `Dockerfile.testing` for Linux-based integration testing |
| `5ddd3903` | Added Docker Buildx support to Dockerfile |
| `5801acaf` | Configured dolt/git identity and `.claude` credential mapping in Dockerfile |
| `e734ecd8` | Relaxed "City started" assertion (removed trailing dot) |
| `aa2085f0` | Added tmux session debug logging to `setupCity()` |
| `86cd42f3` | Fixed tmux guard: session name = agent name (not `gc-city-agent`), socket = city name |
| `4bfacecd` | Fixed `gc stop` to destroy dead tmux sessions, not just panels |
| `ddcc77f4` | Skip PGID=1 assertion inside containers |
| `1db06fe2` | Fixed "City started" assertion in gastown controller test |
| `cce93c14` | Config resolve: respect `agent.PromptMode` with `start_command` |
| `fc097fe3` | Switched integration tests from file store to dolt mode |
| `4c8ec7ec` | Fixed gastown setup: skip reinit, skip provider check, stop before toml update |
| `aeabdf1d` | Added `--include-infra` to bdstore list commands (bd CLI upgrade fix) |
| `df5e39df` | Fixed formula search path: `formulas/` not `.gc/formulas/` |
| `f3209cc2` | Fixed bd() helper to use `integrationEnv()` so hooks find gc binary |
| `bac678cc` | Fixed sendMail empty subject; replaced `gc agent claim` with `bd update` |
| `28fe2eef` | Rewrote `one-shot.sh` and `loop.sh` to use bd JSON commands |
| `b9fdf3ed` | Fixed `TestTutorial01_BashAgent`: stop city before toml overwrite |
| `4c4ab6db` | Updated `TestTutorial02_BashAgent` and `TestTutorial03_BashAgent` |

---

## Root Causes

The failures fell into eight categories:

1. **bd CLI upgrade broke backward compatibility** — `bd list` now hides infrastructure beads (message, agent, rig, role) by default, requiring `--include-infra` to see them.
2. **Removed `gc agent claim` command** — `gc agent` now only has `add`, `suspend`, `resume` subcommands. Bead claiming must use `bd update --assignee --status`.
3. **`gc init` auto-starts the city** — Tests that overwrote `city.toml` after `gc init` without stopping first would leave the default claude agent running.
4. **bd rejects empty titles** — `gc mail send <to> <body>` maps the body positionally, leaving subject (title) empty, which bd now rejects.
5. **Formula search path mismatch** — Tests wrote formulas to `.gc/formulas/` but the actual search path is `formulas/` (from `citylayout.FormulasRoot`).
6. **Environment mismatch in test helpers** — The `bd()` helper used `os.Environ()` instead of `integrationEnv()`, so bd hooks couldn't find the test-built `gc` binary.
7. **Tmux session naming mismatch** — Test guard expected `gc-<city>-<agent>` session names, but the actual format is just the sanitized agent name (per-city socket isolation makes city prefix redundant).
8. **Docker/container-specific issues** — PGID=1 inside containers, dead tmux sessions surviving `gc stop`, missing provider CLIs.

---

## Changes by File

### Docker Infrastructure

#### `Dockerfile.testing` (new file)
Full Docker build for integration testing on Linux. Installs Go, dolt, bd, tmux, git. Configures git/dolt identity. Adds Buildx support. Sets up `.claude` directory for credential mapping.

### Production Code

#### `cmd/gc/controller.go`
**Fix:** `gracefulStopAll` now calls `sp.Stop(name)` for agent processes that exited gracefully but left dead tmux sessions behind.

Before, `gc stop` would only kill the process but not destroy the tmux session container. On Docker, this left dead panes that prevented the tmux server from exiting cleanly, causing `TestTutorial01_StopKillsSession` to fail.

#### `internal/beads/bdstore.go`
**Fix:** Added `--include-infra` flag to all `bd list` invocations.

- `List()` — `bd list --json --limit 0 --all` → `bd list --json --limit 0 --all --include-infra`
- `ListByLabel()` — added `--include-infra`
- `ListByAssignee()` — added `--include-infra`

**Why:** The `bd` CLI upgrade introduced default filtering that hides infrastructure bead types (message, agent, rig, role). Gas City uses all bead types, so the filtering must be disabled. This was the root cause of `TestGastown_HandoffRemote` (handoff created message beads that were invisible to `bd list` and `gc mail inbox`).

#### `internal/beads/bdstore_test.go`
**Fix:** Updated four fake runner expectations to match the new `--include-infra` flag in bd list commands.

#### `internal/config/resolve.go`
**Fix:** `ResolveProvider` now respects `agent.PromptMode` when a `start_command` is set, instead of always defaulting to `"arg"`. Both the agent-level and workspace-level start_command paths now check `agent.PromptMode` first.

#### `internal/runtime/tmux/tmux_test.go`
**Fix:** `TestGetProcessGroupID` now skips the PGID assertion when running inside a container where PGID is 1 (the init process).

### Test Agent Scripts

#### `test/agents/one-shot.sh`
**Fix:** Complete rewrite of work-discovery logic.

- **Before:** Used `gc agent claimed "$GC_AGENT"` (nonexistent command), parsed `ID:` prefix from output.
- **After:** Uses `bd list --json --assignee="$GC_AGENT" --status=in_progress --limit 1`, parses bead ID from pretty-printed JSON with `grep '"id"' | sed`. Handles `set -euo pipefail` safely with `{ grep ... || true; }`.

#### `test/agents/loop.sh`
**Fix:** Complete rewrite of both work-discovery and claim logic.

- **Before:** Used `gc agent claimed` for assigned work, `bd ready` with `grep "^gc-"` for ready queue (hardcoded prefix that doesn't match city-derived prefixes like `g3-`), `gc agent claim` to claim.
- **After:** Uses `bd list --json --assignee=...` for assigned work, `bd ready --json --limit 1` for ready queue (JSON parsing works with any prefix), `bd update --assignee --status in_progress` to claim. Includes comment explaining why `bd update --claim` is not used (it sets assignee to `beads.role`, which is unconfigured in tests).

### Test Infrastructure

#### `test/tmuxtest/guard.go`
**Fix 1:** `NewGuard` now uses city name as the tmux socket name (not a hardcoded default). This matches the supervisor's behavior in `tmuxConfigFromSession` which defaults the socket to the city name.

**Fix 2:** `SessionName()` now returns just the sanitized agent name (e.g., `"mayor"`) instead of `"gc-<city>-<agent>"`. Per-city tmux socket isolation makes the city prefix redundant — the session name format is just `strings.ReplaceAll(agentName, "/", "--")`.

**Fix 3:** Refactored `NewGuard`/`NewGuardWithSocket` to share a `newGuard` constructor.

### Test Setup Helpers

#### `test/integration/helpers_test.go`
**Fix 1:** Added `gc stop` after `gc init` in `setupCity()`.

`gc init` auto-starts the city with the default tutorial config (a claude agent with `prompts/mayor.md`). Without stopping first, the test's `writeAgentsToml` overwrites `city.toml` but the old claude session keeps running. Now the sequence is: `gc init` → `gc stop` → `writeAgentsToml` → `gc start`.

**Fix 2:** Added `prompt_mode = "none"` to `writeAgentsToml()`.

Prevents the reconciler from trying to inject prompts into bash agent sessions.

**Fix 3:** Added `listTmuxSessions()` diagnostic helper for debugging tmux state.

#### `test/integration/gastown_helpers_test.go`
**Fix 1:** Added `--skip-provider-readiness` to `gc init` in `setupGasTownCity()`.

Docker/CI has no provider CLIs (claude, codex, etc.) installed.

**Fix 2:** Added `gc stop` after `gc init` in `setupGasTownCity()`.

Same auto-start issue as `setupCity()`.

**Fix 3:** Made `initBd()` skip if `.beads/` already exists.

`gc init` + `startBeadsLifecycle` already initializes bd. Re-running `bd init` would fail with "already initialized."

**Fix 4:** Rewrote `claimBead()` to use `bd update --assignee --status in_progress`.

Old code used nonexistent `gc agent claim`. Includes comment explaining why `bd update --claim` is not used (it sets assignee to `beads.role`, which is unconfigured in tests).

**Fix 5:** Updated `sendMail()` to use `-s`/`-m` flags.

`gc mail send <to> <body>` maps body positionally, leaving subject empty. bd now rejects empty titles. Changed to `gc mail send <to> -s <subject> -m <body>`.

**Fix 6:** Added `prompt_mode = "none"` to `writeGasTownToml()`.

#### `test/integration/integration_test.go`
**Fix 1:** Changed `bd()` helper from `os.Environ()` to `integrationEnv()`.

bd hooks (`.beads/hooks/on_create`, `on_close`, `on_update`) call `gc event emit`, which needs the test-built `gc` binary in PATH.

**Fix 2:** Removed `GC_DOLT=skip` and `GC_BEADS=file` from `integrationEnv()`.

Tests now run in dolt mode (the real bd provider) instead of file store mode, matching production behavior.

### Test Files

#### `test/integration/gastown_controller_test.go`
**Fix:** Relaxed "City started" assertion to use `strings.Contains(out, "City started")` instead of exact match with trailing dot.

#### `test/integration/gastown_formula_test.go`
**Fix 1:** Changed formula directory from `.gc/formulas` to `formulas/`.

The default search path is `citylayout.FormulasRoot` = `"formulas"`, resolved relative to city root. Tests were writing formulas to the wrong directory.

**Fix 2:** Updated `TestGastown_FormulaNonexistent` assertion for `SilenceErrors`.

The root cobra command has `SilenceErrors: true`, so errors from `formula.Compile` are not printed — only the exit code is non-zero. Changed assertion to tolerate empty output.

#### `test/integration/gastown_handoff_test.go`
**Fix:** Removed debug logging (config.yaml dump, dolt-port dump, direct bd create test, bd list debug).

These were added during F5 diagnosis and are no longer needed after the `--include-infra` fix.

#### `test/integration/tutorial01_test.go`
**Fix 1:** Relaxed "City started" assertion (removed trailing dot expectation).

**Fix 2:** Rewrote `TestTutorial01_BashAgent` to use shared helpers.

- `bd create` + `extractBeadID` → `createBead()`
- `gc agent claim` → `claimBead()` (uses `bd update --assignee --status`)
- Manual polling loop → `waitForBeadStatus()`

#### `test/integration/tutorial02_test.go`
**Fix:** Same pattern as Tutorial01.

- Replaced `gc agent claim` with `claimBead()` for both alice and bob
- Replaced manual polling with `waitForBeadStatus()` for both beads
- Removed unused `"strings"` import

#### `test/integration/tutorial03_test.go`
**Fix:** Simplified bead creation and polling.

- `bd create` + `extractBeadID` → `createBead()`
- Manual all-closed polling loop → `waitForBeadStatus()` per bead
- Removed unused `"strings"` import
- No `claimBead` needed — the loop agent self-claims from the ready queue

---

## Tests Fixed

| Test | Root Cause | Fix Category |
|------|-----------|--------------|
| `TestTutorial01_StartCreatesSession` | tmux session name mismatch | session naming (#7) |
| `TestTutorial01_StopKillsSession` | dead tmux sessions not destroyed | controller fix (#8) |
| `TestTutorial01_StartIsIdempotent` | "City started" assertion too strict | assertion fix |
| `TestGetProcessGroupID` | PGID=1 in container | container fix (#8) |
| `TestGastown_ConfigStartStop` | reinit + provider readiness + "City started" assertion | setup fixes (#3, #8) |
| `TestGastown_EventsBeadLifecycle` | bd hooks couldn't find gc binary | env mismatch (#6) |
| `TestGastown_EventsMailLifecycle` | empty subject rejected by bd | empty title (#4) |
| `TestGastown_FormulaList` | wrong formula directory | search path (#5) |
| `TestGastown_FormulaShow` | wrong formula directory | search path (#5) |
| `TestGastown_FormulaNonexistent` | SilenceErrors swallows output | assertion fix |
| `TestGastown_HandoffRemote` | message beads hidden by bd list | --include-infra (#1) |
| `TestGastown_RefineryProcessing` | gc agent claim removed | bd update (#2) |
| `TestGastown_RefinerySequentialQueue` | gc agent claim removed | bd update (#2) |
| `TestTutorial01_BashAgent` | gc agent claim + auto-start | #2 + #3 |
| `TestTutorial02_BashAgent` | gc agent claim + auto-start | #2 + #3 |
| `TestTutorial03_BashAgent` | gc agent claim in loop.sh | #2 |

---

## File Change Summary

```
 Dockerfile.testing                          | 105 +++++++++++++++
 cmd/gc/controller.go                        |   6 +-
 internal/beads/bdstore.go                   |   6 +-
 internal/beads/bdstore_test.go              |   8 +-
 internal/config/resolve.go                  |  12 +-
 internal/runtime/tmux/tmux_test.go          |   6 +-
 test/agents/loop.sh                         |  33 +++--
 test/agents/one-shot.sh                     |  20 ++-
 test/integration/gastown_controller_test.go |   4 +-
 test/integration/gastown_formula_test.go    |  16 ++-
 test/integration/gastown_helpers_test.go    |  35 ++++-
 test/integration/helpers_test.go            |  33 ++++-
 test/integration/integration_test.go        |   7 +-
 test/integration/tutorial01_test.go         |  37 +----
 test/integration/tutorial02_test.go         |  58 ++------
 test/integration/tutorial03_test.go         |  32 +---
 test/tmuxtest/guard.go                      |  92 ++++++++++--
 17 files changed, 335 insertions(+), 175 deletions(-)
```
