package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/gastownhall/gascity/internal/account"
	"github.com/gastownhall/gascity/internal/citylayout"
	"github.com/spf13/cobra"
)

// newAccountCmd creates the "gc account" parent command with subcommands
// for managing the account registry.
func newAccountCmd(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: "Manage provider account registrations",
		Long: `Register, list, and manage provider accounts for the city.

Accounts map a short handle to an API key configuration directory.
Use gc account add to register accounts, gc account default to set
the preferred account, and gc account list to view all registrations.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				fmt.Fprintln(stderr, "gc account: missing subcommand (list, add, default, remove, status)") //nolint:errcheck // best-effort stderr
			} else {
				fmt.Fprintf(stderr, "gc account: unknown subcommand %q\n", args[0]) //nolint:errcheck // best-effort stderr
			}
			return errExit
		},
	}
	cmd.AddCommand(
		newAccountListCmd(stdout, stderr),
		newAccountAddCmd(stdout, stderr),
		newAccountDefaultCmd(stdout, stderr),
		newAccountRemoveCmd(stdout, stderr),
		newAccountStatusCmd(stdout, stderr),
	)
	return cmd
}

// newAccountListCmd creates the "gc account list" command.
func newAccountListCmd(stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all registered accounts",
		RunE: func(_ *cobra.Command, _ []string) error {
			if doAccountList(stdout, stderr) != 0 {
				return errExit
			}
			return nil
		},
	}
}

// doAccountList lists all registered accounts in a formatted table.
func doAccountList(stdout, stderr io.Writer) int {
	cityPath, err := resolveCity()
	if err != nil {
		fmt.Fprintf(stderr, "gc account list: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	regPath := citylayout.AccountsFilePath(cityPath)
	reg, err := account.Load(regPath)
	if err != nil {
		fmt.Fprintf(stderr, "gc account list: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	if len(reg.Accounts) == 0 {
		fmt.Fprintln(stderr, "error: no accounts registered. Run gc account add to register at least one account.") //nolint:errcheck // best-effort stderr
		return 1
	}

	w := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "HANDLE\tEMAIL\tDESCRIPTION\tCONFIG DIR\tDEFAULT") //nolint:errcheck // best-effort stdout
	for _, acct := range reg.Accounts {
		def := ""
		if acct.Handle == reg.Default {
			def = "default"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", acct.Handle, acct.Email, acct.Description, acct.ConfigDir, def) //nolint:errcheck // best-effort stdout
	}
	w.Flush() //nolint:errcheck // best-effort flush
	return 0
}

// newAccountAddCmd creates the "gc account add" command.
func newAccountAddCmd(stdout, stderr io.Writer) *cobra.Command {
	var handle, email, description, configDir string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Register a new provider account",
		Example: `  gc account add --handle work1 --email user@example.com --config-dir ~/.claude-accounts/work1
  gc account add --handle work2 --email user2@example.com --description "Second account" --config-dir ~/.claude-accounts/work2`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if doAccountAdd(handle, email, description, configDir, stdout, stderr) != 0 {
				return errExit
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&handle, "handle", "", "short name for the account (required)")
	cmd.Flags().StringVar(&email, "email", "", "email address associated with the account (required)")
	cmd.Flags().StringVar(&description, "description", "", "optional description")
	cmd.Flags().StringVar(&configDir, "config-dir", "", "path to the API key configuration directory (required)")
	return cmd
}

// doAccountAdd validates and registers a new account.
func doAccountAdd(handle, email, description, configDir string, stdout, stderr io.Writer) int {
	cityPath, err := resolveCity()
	if err != nil {
		fmt.Fprintf(stderr, "gc account add: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	regPath := citylayout.AccountsFilePath(cityPath)
	reg, err := account.Load(regPath)
	if err != nil {
		fmt.Fprintf(stderr, "gc account add: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	acct := account.Account{
		Handle:      handle,
		Email:       email,
		Description: description,
		ConfigDir:   configDir,
	}

	if err := account.ValidateNewAccount(reg, acct); err != nil {
		fmt.Fprintf(stderr, "gc account add: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	reg.Accounts = append(reg.Accounts, acct)
	if err := account.Save(regPath, reg); err != nil {
		fmt.Fprintf(stderr, "gc account add: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	fmt.Fprintf(stdout, "account %s registered\n", handle) //nolint:errcheck // best-effort stdout
	return 0
}

// newAccountDefaultCmd creates the "gc account default" command.
func newAccountDefaultCmd(stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "default <handle>",
		Short: "Set the default account for this city",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if doAccountDefault(args[0], stdout, stderr) != 0 {
				return errExit
			}
			return nil
		},
	}
}

// doAccountDefault sets the default account handle.
func doAccountDefault(handle string, stdout, stderr io.Writer) int {
	cityPath, err := resolveCity()
	if err != nil {
		fmt.Fprintf(stderr, "gc account default: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	regPath := citylayout.AccountsFilePath(cityPath)
	reg, err := account.Load(regPath)
	if err != nil {
		fmt.Fprintf(stderr, "gc account default: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	if len(reg.Accounts) == 0 {
		fmt.Fprintln(stderr, "error: no accounts registered. Run gc account add to register at least one account.") //nolint:errcheck // best-effort stderr
		return 1
	}

	// Verify the handle exists in the registry.
	found := false
	for _, acct := range reg.Accounts {
		if acct.Handle == handle {
			found = true
			break
		}
	}
	if !found {
		fmt.Fprintf(stderr, "gc account default: account %q is not registered\n", handle) //nolint:errcheck // best-effort stderr
		return 1
	}

	reg.Default = handle
	if err := account.Save(regPath, reg); err != nil {
		fmt.Fprintf(stderr, "gc account default: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	fmt.Fprintf(stdout, "default account set to %s\n", handle) //nolint:errcheck // best-effort stdout
	return 0
}

// newAccountRemoveCmd creates the "gc account remove" command.
func newAccountRemoveCmd(stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <handle>",
		Short: "Deregister an account by handle",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if doAccountRemove(args[0], stdout, stderr) != 0 {
				return errExit
			}
			return nil
		},
	}
}

// doAccountRemove removes an account from the registry. If the removed account
// is the current default, the default is cleared and a warning is emitted.
func doAccountRemove(handle string, stdout, stderr io.Writer) int {
	cityPath, err := resolveCity()
	if err != nil {
		fmt.Fprintf(stderr, "gc account remove: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	regPath := citylayout.AccountsFilePath(cityPath)
	reg, err := account.Load(regPath)
	if err != nil {
		fmt.Fprintf(stderr, "gc account remove: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	// Find and remove the account.
	idx := -1
	for i, acct := range reg.Accounts {
		if acct.Handle == handle {
			idx = i
			break
		}
	}
	if idx == -1 {
		fmt.Fprintf(stderr, "gc account remove: account %q is not registered\n", handle) //nolint:errcheck // best-effort stderr
		return 1
	}

	reg.Accounts = append(reg.Accounts[:idx], reg.Accounts[idx+1:]...)

	// If the removed account was the default, clear the default and warn.
	wasDefault := reg.Default == handle
	if wasDefault {
		reg.Default = ""
	}

	if err := account.Save(regPath, reg); err != nil {
		fmt.Fprintf(stderr, "gc account remove: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}

	// Remove the handle's entry from quota.json if it exists (GAP-1 fix).
	if err := removeQuotaEntry(cityPath, handle); err != nil {
		fmt.Fprintf(stderr, "gc account remove: warning: %v\n", err) //nolint:errcheck // best-effort stderr
		// Non-fatal — the account was already removed from accounts.json.
	}

	fmt.Fprintf(stdout, "account %s removed\n", handle) //nolint:errcheck // best-effort stdout
	if wasDefault {
		fmt.Fprintf(stderr, "warning: %s was the default account — run gc account default to set a new one\n", handle) //nolint:errcheck // best-effort stderr
	}
	return 0
}

// newAccountStatusCmd creates the "gc account status" command.
// In Phase 1 this is a placeholder — full implementation requires tmux ops
// from Phase 2.
func newAccountStatusCmd(stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the active account for sessions",
		Long: `Show which account each active session is using.

This command reads CLAUDE_CONFIG_DIR from tmux session environments and
reverse-maps the path to the matching account handle. Full implementation
requires Phase 2 tmux ops.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Fprintln(stderr, "gc account status: not yet implemented (requires Phase 2 tmux ops)") //nolint:errcheck // best-effort stderr
			return errExit
		},
	}
}

// removeQuotaEntry removes the given handle's entry from quota.json.
// If quota.json does not exist, this is a no-op (Level 0 compatibility).
// Uses basic os.ReadFile + encoding/json + atomic write since the formal
// quota I/O layer is not available on the Phase 1 branch.
func removeQuotaEntry(cityPath, handle string) error {
	quotaPath := filepath.Join(cityPath, ".gc", "quota.json")

	raw, err := os.ReadFile(quotaPath)
	if os.IsNotExist(err) {
		return nil // No quota.json — nothing to clean up.
	}
	if err != nil {
		return fmt.Errorf("reading quota.json: %w", err)
	}

	var quota map[string]json.RawMessage
	if err := json.Unmarshal(raw, &quota); err != nil {
		return fmt.Errorf("parsing quota.json: %w", err)
	}

	accountsRaw, ok := quota["accounts"]
	if !ok {
		return nil // No "accounts" key — nothing to clean up.
	}

	var accounts map[string]json.RawMessage
	if err := json.Unmarshal(accountsRaw, &accounts); err != nil {
		return fmt.Errorf("parsing quota.json accounts: %w", err)
	}

	if _, exists := accounts[handle]; !exists {
		return nil // Handle not in quota.json — nothing to do.
	}

	delete(accounts, handle)

	// Marshal the updated accounts back into the quota map.
	updatedAccounts, err := json.Marshal(accounts)
	if err != nil {
		return fmt.Errorf("marshaling quota.json accounts: %w", err)
	}
	quota["accounts"] = updatedAccounts

	updatedRaw, err := json.MarshalIndent(quota, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling quota.json: %w", err)
	}

	// Atomic write: temp file + rename.
	tmpPath := quotaPath + ".tmp"
	if err := os.WriteFile(tmpPath, updatedRaw, 0o644); err != nil {
		return fmt.Errorf("writing quota.json temp file: %w", err)
	}
	if err := os.Rename(tmpPath, quotaPath); err != nil {
		return fmt.Errorf("renaming quota.json temp file: %w", err)
	}

	return nil
}
