//go:build integration

package integration

import (
	"testing"
	"time"

	"github.com/gastownhall/gascity/test/tmuxtest"
)

// TestTutorial02_BashAgent validates the Tutorial 02 (Named Crew) flow:
// multiple named agents, each with their own hooked beads. Two one-shot
// agents run concurrently; each gets a different bead assigned via hook.
//
// This tests the key Tutorial 02 concepts:
//   - Multiple [[agent]] in city.toml
//   - Hook-based assignment to specific named agents
//   - Each agent processes only its own hooked work
func TestTutorial02_BashAgent(t *testing.T) {
	agents := []agentConfig{
		{Name: "alice", StartCommand: "bash " + agentScript("one-shot.sh")},
		{Name: "bob", StartCommand: "bash " + agentScript("one-shot.sh")},
	}

	var cityDir string
	if usingSubprocess() {
		cityDir = setupCityNoGuard(t, agents)
	} else {
		guard := tmuxtest.NewGuard(t)
		cityDir = setupCity(t, guard, agents)
		for _, name := range []string{"alice", "bob"} {
			if !guard.HasSession(guard.SessionName(name)) {
				t.Fatalf("expected %s tmux session after gc start", name)
			}
		}
	}

	// Create two beads and claim each for a different agent.
	aliceBead := createBead(t, cityDir, "Task for Alice")
	bobBead := createBead(t, cityDir, "Task for Bob")
	claimBead(t, cityDir, "alice", aliceBead)
	claimBead(t, cityDir, "bob", bobBead)

	// Wait for both one-shot agents to close their beads.
	waitForBeadStatus(t, cityDir, aliceBead, "closed", 10*time.Second)
	waitForBeadStatus(t, cityDir, bobBead, "closed", 10*time.Second)
}
