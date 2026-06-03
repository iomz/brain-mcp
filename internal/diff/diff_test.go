package diff

import "testing"

func TestUnified(t *testing.T) {
	got := Unified("Knowledge/Self.md", "# Self\n", "# Self\n\nUpdated.\n")
	if got == "" {
		t.Fatal("expected diff")
	}
	if !containsAll(got, "--- a/Knowledge/Self.md", "+++ b/Knowledge/Self.md", "-# Self", "+Updated.") {
		t.Fatalf("unexpected diff:\n%s", got)
	}
}

func containsAll(s string, needles ...string) bool {
	for _, needle := range needles {
		if !contains(s, needle) {
			return false
		}
	}
	return true
}

func contains(s, needle string) bool {
	return len(needle) == 0 || len(s) >= len(needle) && (s == needle || contains(s[1:], needle) || s[:len(needle)] == needle)
}
