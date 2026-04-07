package main

import (
	"fmt"

	"github.com/gastownhall/gascity/internal/runtime/tmux"
)

// PaneInfo holds identifying information for a tmux pane.
type PaneInfo struct {
	SessionName string
	PaneID      string
}

// TmuxOps groups the tmux operations needed by the quota rotation subsystem.
// Each field is a function that performs one tmux operation.
// This struct-of-functions pattern (not an interface) follows CLAUDE.md
// conventions and enables FakeTmuxOps for testing without a real tmux server.
type TmuxOps struct {
	// CapturePane returns the last N lines of output from the named session.
	CapturePane func(sessionName string, lines int) (string, error)

	// ShowEnv returns the value of an environment variable in the named session.
	ShowEnv func(sessionName, key string) (string, error)

	// SetEnv sets an environment variable in the named session.
	SetEnv func(sessionName, key, value string) error

	// RespawnPane kills the current process in the named session's pane and
	// restarts it.
	RespawnPane func(sessionName string) error

	// ListPanes returns information about all active tmux panes.
	ListPanes func() ([]PaneInfo, error)

	// IsRunning reports whether the tmux server is available.
	IsRunning func() bool
}

// DefaultTmuxOps returns a TmuxOps that delegates to real tmux commands
// via the internal/runtime/tmux package.
func DefaultTmuxOps() TmuxOps {
	tm := tmux.NewTmux()
	return TmuxOps{
		CapturePane: func(sessionName string, lines int) (string, error) {
			return tm.CapturePane(sessionName, lines)
		},
		ShowEnv: func(sessionName, key string) (string, error) {
			return tm.GetEnvironment(sessionName, key)
		},
		SetEnv: func(sessionName, key, value string) error {
			return tm.SetEnvironment(sessionName, key, value)
		},
		RespawnPane: func(sessionName string) error {
			// Get the current pane command before respawning so we can
			// restart the same command.
			cmd, err := tm.GetPaneCommand(sessionName)
			if err != nil {
				return fmt.Errorf("getting pane command for %q: %w", sessionName, err)
			}
			paneID, err := tm.GetPaneID(sessionName)
			if err != nil {
				return fmt.Errorf("getting pane ID for %q: %w", sessionName, err)
			}
			return tm.RespawnPane(paneID, cmd)
		},
		ListPanes: func() ([]PaneInfo, error) {
			sessions, err := tm.ListSessions()
			if err != nil {
				return nil, err
			}
			var panes []PaneInfo
			for _, name := range sessions {
				paneID, err := tm.GetPaneID(name)
				if err != nil {
					// Skip sessions where we can't get the pane ID.
					continue
				}
				panes = append(panes, PaneInfo{
					SessionName: name,
					PaneID:      paneID,
				})
			}
			return panes, nil
		},
		IsRunning: func() bool {
			return tm.IsAvailable()
		},
	}
}
