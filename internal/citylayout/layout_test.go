package citylayout

import "testing"

func TestAccountsFilePath(t *testing.T) {
	got := AccountsFilePath("myCity")
	want := "myCity/.gc/accounts.json"
	if got != want {
		t.Fatalf("AccountsFilePath(%q) = %q, want %q", "myCity", got, want)
	}
}
