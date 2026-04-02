// Package tmuxtest provides helpers for integration tests that need real tmux.
//
// Guard manages tmux session lifecycle for tests: it generates unique city
// names with a "gctest-" prefix, tracks created sessions, and guarantees
// cleanup even on test failures. Three layers prevent orphan sessions:
//
//  1. Pre-sweep (TestMain): kill all gc-gctest-* sessions from prior crashes.
//  2. Per-test (t.Cleanup): kill sessions created by this guard.
//  3. Post-sweep (TestMain defer): final sweep after all tests complete.
//
// All operations use an isolated tmux socket ("gc-test" by default) so tests
// never interfere with the user's running tmux server.
package tmuxtest

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

// DefaultSocketName is the tmux socket used by test infrastructure.
// Using a dedicated socket isolates tests from the user's tmux server.
const DefaultSocketName = "gc-test"

// RequireTmux skips the test if tmux is not installed.
func RequireTmux(t testing.TB) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
}

// Guard manages tmux session lifecycle for a single test. It generates a
// unique city name with the "gctest-" prefix and guarantees cleanup of all
// sessions matching that city via t.Cleanup.
type Guard struct {
	t          testing.TB
	cityName   string // "gctest-<8hex>"
	socketName string // tmux socket for isolation
}

// NewGuard creates a guard with a unique city name. The tmux socket defaults
// to the city name — matching the default in tmuxConfigFromSession — so the
// Guard checks the same socket the supervisor will use.
func NewGuard(t testing.TB) *Guard {
	t.Helper()
	RequireTmux(t)

	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("tmuxtest: generating random city name: %v", err)
	}
	cityName := fmt.Sprintf("gctest-%x", b)

	// Use cityName as the tmux socket — this matches the default behaviour
	// of cmd/gc/providers.go:tmuxConfigFromSession which falls back to
	// cityName when [session] socket is not configured.
	return newGuard(t, cityName, cityName)
}

// NewGuardWithSocket creates a guard using the specified tmux socket.
func NewGuardWithSocket(t testing.TB, socketName string) *Guard {
	t.Helper()
	RequireTmux(t)

	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("tmuxtest: generating random city name: %v", err)
	}
	cityName := fmt.Sprintf("gctest-%x", b)

	return newGuard(t, cityName, socketName)
}

// newGuard is the shared constructor for Guard.
func newGuard(t testing.TB, cityName, socketName string) *Guard {
	t.Helper()
	g := &Guard{t: t, cityName: cityName, socketName: socketName}
	t.Cleanup(func() {
		g.killGuardSessions()
	})
	return g
}

// CityName returns the unique city name (e.g., "gctest-a1b2c3d4").
func (g *Guard) CityName() string {
	return g.cityName
}

// SocketName returns the tmux socket name used by this guard.
func (g *Guard) SocketName() string {
	return g.socketName
}

// SessionName returns the expected tmux session name for an agent.
// Mirrors internal/agent.SessionNameFor with no session_template:
// per-city tmux socket isolation makes a city prefix redundant,
// so the session name is just the sanitized agent name.
func (g *Guard) SessionName(agentName string) string {
	return strings.ReplaceAll(agentName, "/", "--")
}

// HasSession checks if a specific tmux session exists.
func (g *Guard) HasSession(name string) bool {
	g.t.Helper()
	args := tmuxArgs(g.socketName, "has-session", "-t", name)
	out, err := exec.Command("tmux", args...).CombinedOutput()
	if err != nil {
		// tmux has-session exits 1 when session doesn't exist
		// and also when no server is running. Both mean "not found".
		_ = out
		return false
	}
	return true
}

// killGuardSessions kills all tmux sessions on this guard's socket.
// With per-city socket isolation, every session on the socket belongs
// to this test, so we kill the entire tmux server for clean teardown.
func (g *Guard) killGuardSessions() {
	g.t.Helper()
	args := tmuxArgs(g.socketName, "kill-server")
	_ = exec.Command("tmux", args...).Run()
}

// KillAllTestSessions kills all tmux sessions matching "gc-gctest-*".
// Call from TestMain before and after test runs to clean up orphans.
// Checks both the legacy default socket and any per-city sockets
// (named "gctest-*") that the Guard now uses.
func KillAllTestSessions(t testing.TB) {
	// Legacy socket (for tests using NewGuardWithSocket explicitly).
	KillAllTestSessionsOnSocket(t, DefaultSocketName)
	// Per-city sockets: each NewGuard uses cityName as the tmux socket.
	// Scan the tmux socket directory for "gctest-*" sockets.
	for _, sock := range discoverTestSockets() {
		KillAllTestSessionsOnSocket(t, sock)
	}
}

// KillAllTestSessionsOnSocket kills orphaned test sessions on the given socket.
// For per-city sockets (named "gctest-*"), kills the entire tmux server since
// all sessions on that socket belong to tests. For shared sockets (e.g.
// DefaultSocketName), kills only sessions with the "gc-gctest-" prefix.
func KillAllTestSessionsOnSocket(t testing.TB, socketName string) {
	t.Helper()
	if strings.HasPrefix(socketName, "gctest-") {
		// Per-city socket: every session belongs to tests; kill the server.
		args := tmuxArgs(socketName, "kill-server")
		_ = exec.Command("tmux", args...).Run()
		return
	}
	// Shared socket: only kill sessions with the test prefix.
	sessions := listSessionsWithPrefix(socketName, "gc-gctest-")
	for _, s := range sessions {
		args := tmuxArgs(socketName, "kill-session", "-t", s)
		_ = exec.Command("tmux", args...).Run()
	}
	if len(sessions) > 0 {
		t.Logf("tmuxtest: cleaned up %d orphaned test session(s)", len(sessions))
	}
}

// tmuxArgs prepends -L socketName to the given tmux arguments when socketName
// is non-empty.
func tmuxArgs(socketName string, args ...string) []string {
	if socketName == "" {
		return args
	}
	return append([]string{"-L", socketName}, args...)
}

// listSessionsWithPrefix returns all tmux session names starting with prefix.
func listSessionsWithPrefix(socketName, prefix string) []string {
	args := tmuxArgs(socketName, "list-sessions", "-F", "#{session_name}")
	out, err := exec.Command("tmux", args...).Output()
	if err != nil {
		// No tmux server running means no sessions to clean.
		return nil
	}
	var matches []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" && strings.HasPrefix(line, prefix) {
			matches = append(matches, line)
		}
	}
	return matches
}

// discoverTestSockets finds tmux sockets named "gctest-*" in the standard
// tmux socket directory (/tmp/tmux-<uid>/ on Linux). These are created by
// NewGuard which uses the city name as the socket. Best-effort: returns nil
// if the directory doesn't exist or can't be read.
func discoverTestSockets() []string {
	u, err := user.Current()
	if err != nil {
		return nil
	}
	// tmux stores sockets in /tmp/tmux-<uid>/ by default, or TMUX_TMPDIR.
	socketDir := os.Getenv("TMUX_TMPDIR")
	if socketDir == "" {
		socketDir = filepath.Join(os.TempDir(), "tmux-"+u.Uid)
	}
	entries, err := os.ReadDir(socketDir)
	if err != nil {
		return nil
	}
	var sockets []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "gctest-") {
			sockets = append(sockets, e.Name())
		}
	}
	return sockets
}
