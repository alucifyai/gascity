# Docker Integration Test Fixes

## Problem

Running `make check-all` inside the `gc-test` Docker container
(`docker build -f Dockerfile.testing -t gc-test .` then
`docker run --rm gc-test make check-all`) failed across multiple test
groups: docsync, PGID, tutorial, gastown, and E2E tests.

## Root Causes

Six independent issues prevented tests from passing in Docker:

### 1. Missing test and docs files in Docker image

`.dockerignore` excluded `test/` and `docs/` directories entirely. The
docsync tests need docs, and integration tests need `test/agents/*.sh`
scripts.

### 2. PGID test assumes non-container environment

`TestGetProcessGroupID` in `internal/runtime/tmux/tmux_test.go` rejected
PGID=1 as invalid. Inside Docker, the test process IS PID 1 and PGID 1
is expected.

### 3. `gc init` auto-starts a city that interferes with test setup

`gc init` now registers and starts a default tutorial city via the
supervisor. Test helpers (`setupCity`, `setupGasTownCity`) overwrote
`city.toml` immediately after init, but the already-running supervisor
still used the old config. The `setupE2ECity` helper already had the fix
(calling `gc stop` after init); the other two helpers did not.

### 4. Missing `--skip-provider-readiness` in gastown setup

`setupGasTownCity` called `gc init` without `--skip-provider-readiness`.
In Docker, no provider CLIs (claude, codex, gemini) are authenticated, so
init blocked waiting for provider readiness.

### 5. Supervisor hangs without file-based beads provider

Without `[beads] provider = "file"` in `city.toml`, the supervisor
attempts to connect to dolt for beads reconciliation. But
`integrationEnv()` sets `GC_DOLT=skip`, so no dolt server is running.
The supervisor hangs indefinitely at "Adopting sessions...".

### 6. `ResolveProvider` ignores `prompt_mode` when `start_command` is set

When an agent has `start_command` configured, `ResolveProvider()` returned
early at Step 1 with hardcoded `PromptMode: "arg"`, completely skipping
Step 4 (`mergeAgentOverrides`) where `agent.PromptMode` would be applied.

This meant setting `prompt_mode = "none"` in `city.toml` had no effect
for agents with `start_command`. The supervisor appended a rendered prompt
suffix to every command, turning `sleep 3600` into
`sleep 3600 '[cityname] agent...'` which fails because `sleep` cannot
parse text arguments. Tmux sessions died immediately after creation.

### 7. "City started." vs "City started under supervisor."

In Docker, the supervisor falls back from systemd to direct process
management. This produces the output `"City started under supervisor."`
instead of `"City started."`. Tests that checked for the exact string
`"City started."` failed.

### 8. Test timeout too short

The integration test timeout was 8 minutes. With E2E tests consuming ~3
minutes on internal polling timeouts alone, plus the full gastown and
tutorial test suites, 8 minutes was insufficient. Tests were being killed
before completion.

## Changes

### `.dockerignore`

Removed blanket `test` and `docs` exclusions. Added negation patterns
(`!docs/**`, `!README.md`, `!CONTRIBUTING.md`, `!TESTING.md`) so docs
needed by docsync tests are included in the Docker image.

### `Makefile`

Increased `test-integration` timeout from `8m` to `15m`.

### `internal/config/resolve.go`

Fixed `ResolveProvider()` to respect `agent.PromptMode` in both
`start_command` early-return paths (agent-level and workspace-level).
When `agent.PromptMode` is set (e.g., `"none"`), it now overrides the
default `"arg"`. When unset, behavior is unchanged (defaults to `"arg"`).

This is a bug fix that affects production behavior: any user setting
`prompt_mode = "none"` on an agent with `start_command` was being ignored.

### `internal/runtime/tmux/tmux_test.go`

`TestGetProcessGroupID` now detects container environments (PID 1 or
PGID "1") and skips the PGID assertion with `t.Skipf` instead of failing.

### `test/integration/helpers_test.go`

- `setupCity()`: Added `gc stop` after `gc init` to stop the auto-started
  tutorial city before overwriting `city.toml`.
- `writeAgentsToml()`: Added `[beads] provider = "file"` section so the
  supervisor uses file-based beads (no dolt dependency). Added
  `prompt_mode = "none"` to each agent to prevent prompt suffix from
  being appended to bare commands like `sleep 3600`.

### `test/integration/gastown_helpers_test.go`

- `setupGasTownCity()`: Added `--skip-provider-readiness` to `gc init`.
  Added `gc stop` after init (same pattern as `setupCity`).
- `writeGasTownToml()`: Added `[beads] provider = "file"` section. Added
  `prompt_mode = "none"` to each agent.

### `test/integration/tutorial01_test.go`

`TestTutorial01_StartIsIdempotent`: Changed assertion from
`"City started."` to `"City started"` (without trailing period) so it
matches both `"City started."` and `"City started under supervisor."`.

### `test/integration/gastown_controller_test.go`

`TestGastown_ControllerStartStop`: Same `"City started"` assertion fix.

## Test Results After Fix

**Fixed (previously failing):**
- All docsync tests
- PGID test (now skips in container)
- All Tutorial01/02/03 tests
- Gastown config tests (ConfigStartStop, ConfigWithPool, ConfigValidate)
- Gastown controller tests (ControllerStartStop, ControllerIdleAgent)
- Gastown SuspendedAgentSkipped

**Remaining failures (pre-existing, not caused by Docker environment):**
- 5 E2E tests: session reconciliation/restart issues after config changes
  (ConfigDrift, SuspendResume_Agent), session naming mismatch (Kill),
  hook override ordering (Hooks_AgentOverride), tmux-only feature (PreStart)
- 5 gastown domain tests: beads provider mismatch between `bd` CLI
  (uses dolt) and `gc` commands (uses file store), formula directory
  path mismatch, mail timeout

These remaining failures are functional issues exposed now that `gc start`
no longer hangs. They were previously masked by the 8-minute timeout.

## How to Apply

```bash
git apply docker-test-fixes.patch
```

## How to Verify

```bash
docker build -f Dockerfile.testing -t gc-test .
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock gc-test make check-all
```
