# Remaining Docker Test Failures

10 integration tests still fail after the Docker environment fixes.
These are pre-existing functional issues, not Docker environment problems.
They were previously hidden because `gc start` hung indefinitely (masking
all downstream tests) and the 8-minute timeout killed the suite before
reaching them.

---

## E2E Tests (5 failures)

### TestE2E_Kill

**Symptom:** `gc session kill: session gc-1 is not active`

**Root cause:** The session bead exists but is not in an active state.
The test calls `gc session kill killme` which resolves the agent name
to a session bead. The session manager's `Kill()` method
(`internal/session/manager.go:395-404`) checks the bead's `state`
metadata and rejects the kill if the state is not `active`, `creating`,
or `draining`. The session `gc-1` is found but in a different state
(likely `archived` or `quarantined`).

**Why left unremediated:** This is a session state management issue in
the supervisor reconciliation path, not a Docker environment issue. The
supervisor creates the session bead during startup but may transition
it through states differently when using the direct-process fallback
(no systemd) vs the systemd-managed path.

**What it would take to fix:**
- Debug the session bead state after `setupE2ECity` completes — check
  what state `gc-1` is in and why it's not `active`
- Trace the supervisor reconciliation to see if the session is being
  created then immediately transitioned to a non-active state
- May need to add a short poll/wait in the test for the session to
  reach `active` state, or fix the supervisor to not transition new
  sessions away from `active` prematurely

---

### TestE2E_ConfigDrift

**Symptom:** Timeout (90s) waiting for second report after config change.

**Root cause:** The test flow is:
1. Start agent with `CUSTOM_VERSION=v1` — first report succeeds
2. Rewrite `city.toml` with `CUSTOM_VERSION=v2`
3. Delete old report file
4. Run `gc start` to trigger reconciliation
5. Wait for new report with `v2` — **times out**

The reconciliation detects the config hash change but does not restart
the agent. The agent continues running with the old environment, so no
new report is written.

**Why left unremediated:** This is a reconciliation logic issue in the
controller (`cmd/gc/config_hash.go`, `cmd/gc/reconcile.go`). The
reconciler may not properly handle the case where only environment
variables changed (vs command or structural changes). This requires
understanding the full reconciliation state machine.

**What it would take to fix:**
- Verify the config hash includes agent-level `env` changes
- Check if reconciliation correctly kills and restarts agents when
  only env vars change (not command or structure)
- The reconciler may need to compare per-agent config hashes rather
  than a single city-level hash
- Add test logging to capture what reconciliation decisions are made

---

### TestE2E_SuspendResume_Agent

**Symptom:** Timeout (90s) waiting for report after resume + `gc start`.

**Root cause:** The test flow is:
1. Start agent, wait for initial report — succeeds
2. Suspend agent via `gc agent suspend`
3. Kill the session via `gc session kill`
4. Delete old report
5. Run `gc start` — suspended agent correctly does NOT restart
6. Resume agent via `gc agent resume`
7. Run `gc start` again — agent should restart
8. Wait for new report — **times out**

After resume, `gc start` does not restart the agent. The bead store
records the suspension state change, but the reconciler does not
pick it up as a trigger to start a new session.

**Why left unremediated:** Same reconciliation issue as ConfigDrift.
The reconciler needs to detect that an agent transitioned from
`suspended=true` to `suspended=false` and treat it as a reason to
start a new session for that agent.

**What it would take to fix:**
- Check how `gc agent resume` updates the agent's config/state
- Verify the reconciler compares current running state against desired
  state (which now includes the resumed agent)
- The reconciler may be checking "is a session already registered for
  this agent?" and finding the old (killed) session bead, so it skips
  creating a new one
- May need to clean up or archive killed session beads so the
  reconciler sees the agent as needing a fresh session

---

### TestE2E_Hooks_AgentOverride

**Symptom:** `workspace gemini hook should be replaced by agent claude hook`

**Root cause:** The config resolution logic in
`ResolveInstallHooks()` (`internal/config/resolve.go:78-86`) correctly
implements replace semantics (agent-level hooks override workspace
hooks). The TOML generation in `writeE2EToml()` also correctly writes
both workspace and agent hooks. However, the gemini hook file
(`.gemini/settings.json`) is still present in the agent's workdir.

The likely cause is that hooks are installed during `gc init` or during
the first `gc start` call in `setupE2ECity()`. The workspace-level
`install_agent_hooks = ["gemini"]` gets applied first (installing
gemini hooks), and then when the agent starts with its own
`install_agent_hooks = ["claude"]`, the claude hooks are installed but
the already-existing gemini files are not cleaned up.

**Why left unremediated:** This is a hook installation ordering issue
in the startup pipeline. The fix requires understanding when and how
hooks are installed relative to config resolution, and whether the
hook installer should clean up files from hooks that are no longer in
the resolved list.

**What it would take to fix:**
- Trace the hook installation path during `gc start` — determine
  when workspace hooks vs agent hooks are installed
- Option A: Before installing hooks, delete any existing hook files
  from providers NOT in the resolved list
- Option B: Change the installation order so agent-level hooks are
  resolved BEFORE any files are written
- Check `cmd/gc/hooks.go` and `internal/hooks/` for the installation
  logic

---

### TestE2E_PreStart

**Symptom:** `pre_start marker file not found — pre_start command did not execute`

**Root cause:** `pre_start` commands are only implemented in the tmux
provider (`internal/runtime/tmux/adapter.go:158`). The test correctly
skips when `GC_SESSION=subprocess`, but in Docker the session provider
is tmux with a supervisor fallback — `usingSubprocess()` returns
`false`, so the test runs.

However, the supervisor's session creation path may not execute
`pre_start` commands. The supervisor calls the tmux adapter's
`Start()` method, which calls `doStartSession()`, which calls
`runPreStart()`. But the supervisor may use a different code path
(e.g., the exec provider or a simplified start) that bypasses
`runPreStart()`.

**Why left unremediated:** This requires tracing the supervisor's
session creation path in Docker to determine whether it uses the tmux
adapter directly or goes through an intermediate layer that strips
`pre_start`. The fix might be straightforward (ensure the supervisor
calls the full tmux adapter pipeline) or might require architectural
changes.

**What it would take to fix:**
- Determine which code path the supervisor uses to create sessions in
  Docker (tmux adapter directly, or via exec/subprocess wrapper)
- If the supervisor bypasses `runPreStart`, add it to the supervisor's
  session creation pipeline
- Alternatively, make the test detect the supervisor fallback mode and
  skip (similar to the subprocess skip)

---

## Gastown Domain Tests (5 failures)

### TestGastown_EventsBeadLifecycle & TestGastown_EventsFiltering

**Symptom:** `expected events of type bead.created, got 'No events.'`

**Root cause:** These tests create beads via `createBead()` which calls
the `bd` CLI (`bd create`). The `bd` binary writes to its own
dolt-backed beads store. However, `city.toml` now has
`[beads] provider = "file"`, so `gc events` reads from the file-based
event store.

The `bd create` command does not emit events to the gc event bus —
events are only emitted by `gc` commands that go through the SDK's
event pipeline. Since the bead was created via `bd` (a separate binary
with its own store), no `bead.created` event is recorded in gc's event
log.

**Why left unremediated:** This is a fundamental store mismatch between
the `bd` CLI and the `gc` SDK. Fixing it requires either:

**What it would take to fix (choose one):**
- **Option A:** Replace `bd create` calls with equivalent `gc bead`
  commands that go through the SDK pipeline and emit events. Requires
  `gc bead create` to exist and work with file-based beads.
- **Option B:** Remove `[beads] provider = "file"` from
  `writeGasTownToml` and instead fix the dolt connectivity issue.
  This means running dolt in the test environment (removing
  `GC_DOLT=skip` for tests that need it) or making the supervisor
  handle `GC_DOLT=skip` gracefully (timeout + fallback instead of
  hanging indefinitely).
- **Option C:** Have `initBd()` use `integrationEnv()` instead of
  `os.Environ()` so the `bd` and `gc` binaries share the same
  environment, then remove `GC_DOLT=skip` from `integrationEnv()` so
  dolt actually runs. This is the most correct fix but requires
  verifying dolt works properly in Docker.

---

### TestGastown_FormulaList

**Symptom:** `expected 'test-patrol' in formula list` — the formula
list shows `cooking, mol-do-work, mol-polecat-base, mol-polecat-commit,
pancakes` but not `test-patrol`.

**Root cause:** The test writes the formula file to
`.gc/formulas/test-patrol.formula.toml` but `gc formula list` searches
the formula directories configured via the pack system
(`cfg.FormulaLayers.City`). The default formulas from `gc init` are in
`formulas/` at the city root (not `.gc/formulas/`).

The formulas shown (cooking, mol-polecat-*, pancakes) are default
formulas installed by `gc init` into the `formulas/` directory. The
test's formula in `.gc/formulas/` is not in the search path.

**Why left unremediated:** This is a test bug — the formula is written
to the wrong directory. The fix is straightforward but requires
verifying the correct formula search path.

**What it would take to fix:**
- Change the test to write to `formulas/` (city root) instead of
  `.gc/formulas/`
- OR determine the correct search path from `cfg.FormulaLayers.City`
  and write there
- Verify by checking `cmd/gc/cmd_formula.go:allFormulaSearchPaths()`

---

### TestGastown_FormulaNonexistent

**Symptom:** `expected 'not found' in error` — the error output does
not contain the string "not found".

**Root cause:** The test runs `gc formula show nonexistent` and expects
an error containing "not found". The command does error (err != nil
check passes) but the error message uses different wording.

**Why left unremediated:** This is a simple assertion mismatch between
the test expectation and the actual error message format.

**What it would take to fix:**
- Run `gc formula show nonexistent` and capture the actual error message
- Update the test assertion to match the actual wording
- OR update `cmd/gc/cmd_formula.go` to use "not found" in the error

---

### TestGastown_MailArchive

**Symptom:** Test timed out (killed by 15-minute suite timeout).

**Root cause:** The test calls `gc mail` commands which interact with
the beads store. With `[beads] provider = "file"`, mail operations
should work through the file store. However, the test also calls
`initBd()` during setup, which creates a dolt-backed beads database.
The `gc mail` commands may be trying to use the dolt store (via
environment variables or auto-detection) and hanging because dolt
isn't running (`GC_DOLT=skip`).

Alternatively, the `gc stop` in the cleanup handler is hanging
(the panic stacktrace shows `setupGasTownCity.func1` in the cleanup
calling `gc stop`), which prevents the test from completing.

**Why left unremediated:** Same beads provider mismatch as the events
tests. The mail subsystem may have additional dolt dependencies beyond
what `[beads] provider = "file"` covers.

**What it would take to fix:**
- Same options as the events tests (Option A/B/C above)
- Additionally, check if `gc mail` has its own beads/dolt connection
  path that bypasses the city config's `[beads]` section
- The cleanup hang may need a timeout on the `gc stop` call in
  `t.Cleanup`

---

## Summary

| Test | Category | Difficulty | Approach |
|------|----------|------------|----------|
| TestE2E_Kill | Session state | Medium | Debug bead state after startup |
| TestE2E_ConfigDrift | Reconciliation | Hard | Fix config-change detection in reconciler |
| TestE2E_SuspendResume_Agent | Reconciliation | Hard | Fix resume-triggered restart in reconciler |
| TestE2E_Hooks_AgentOverride | Hook ordering | Medium | Clean up stale hooks before install |
| TestE2E_PreStart | Provider support | Medium | Ensure supervisor uses full tmux pipeline |
| TestGastown_EventsBeadLifecycle | Store mismatch | Hard | Unify bd/gc beads stores or fix dolt in Docker |
| TestGastown_EventsFiltering | Store mismatch | Hard | Same as above |
| TestGastown_FormulaList | Test bug | Easy | Write formula to correct directory |
| TestGastown_FormulaNonexistent | Assertion | Easy | Match actual error message wording |
| TestGastown_MailArchive | Store mismatch | Hard | Same as events + check mail dolt deps |

**Quick wins (Easy):** FormulaList, FormulaNonexistent — simple test
fixes, no production code changes needed.

**Medium effort:** Kill, Hooks_AgentOverride, PreStart — require
debugging specific code paths but the fixes are likely localized.

**Hard:** ConfigDrift, SuspendResume_Agent, Events*, MailArchive —
require changes to the reconciliation engine or resolving the
fundamental bd/gc beads store mismatch.
