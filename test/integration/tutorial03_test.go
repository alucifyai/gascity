//go:build integration

package integration

import (
	"testing"
	"time"

	"github.com/gastownhall/gascity/test/tmuxtest"
)

// TestTutorial03_BashAgent validates the Tutorial 03 (Ralph Loop) flow:
// a loop agent that drains the backlog by self-claiming beads from the
// ready queue. The bash script (test/agents/loop.sh) implements
// prompts/loop.md:
//
//  1. Check claim for already-assigned work
//  2. If nothing claimed, check ready queue
//  3. Claim first available bead
//  4. Close it
//  5. Repeat
//
// Three beads are created; the agent should drain them all without any
// external nudging.
func TestTutorial03_BashAgent(t *testing.T) {
	agents := []agentConfig{
		{Name: "mayor", StartCommand: "bash " + agentScript("loop.sh")},
	}

	var cityDir string
	if usingSubprocess() {
		cityDir = setupCityNoGuard(t, agents)
	} else {
		guard := tmuxtest.NewGuard(t)
		cityDir = setupCity(t, guard, agents)
		if !guard.HasSession(guard.SessionName("mayor")) {
			t.Fatal("expected mayor tmux session after gc start")
		}
	}

	// Create three beads — the loop agent should drain them all
	// by self-claiming from the ready queue.
	var beadIDs []string
	for _, title := range []string{
		"Implement 3-disk solver",
		"Add animation to disc moves",
		"Write unit tests for the solver",
	} {
		beadIDs = append(beadIDs, createBead(t, cityDir, title))
	}

	// Wait for all three beads to be closed.
	for _, id := range beadIDs {
		waitForBeadStatus(t, cityDir, id, "closed", 15*time.Second)
	}
}
