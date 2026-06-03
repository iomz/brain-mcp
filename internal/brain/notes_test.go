package brain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadWriteAndListNotes(t *testing.T) {
	v := testVault(t)

	path, content, err := v.ReadNote("Knowledge/Self.md")
	if err != nil {
		t.Fatal(err)
	}
	if path != "Knowledge/Self.md" || content != "# Self\n" {
		t.Fatalf("unexpected read: %q %q", path, content)
	}

	if _, _, err := v.ApplyPatch("Knowledge/Self.md", "# Self\n\nUpdated.\n"); err != nil {
		t.Fatal(err)
	}
	_, content, err = v.ReadNote("Knowledge/Self.md")
	if err != nil {
		t.Fatal(err)
	}
	if content != "# Self\n\nUpdated.\n" {
		t.Fatalf("unexpected content: %q", content)
	}

	if err := os.WriteFile(filepath.Join(v.Root(), "Knowledge", "Other.md"), []byte("# Other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v.Root(), "Knowledge", "Skip.txt"), []byte("skip"), 0o644); err != nil {
		t.Fatal(err)
	}
	notes, err := v.ListNotes("Knowledge")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Knowledge/Other.md", "Knowledge/Self.md"}
	if len(notes) != len(want) {
		t.Fatalf("got %v, want %v", notes, want)
	}
	for i := range want {
		if notes[i] != want[i] {
			t.Fatalf("got %v, want %v", notes, want)
		}
	}
}

func TestAllowedPathWriteSuccess(t *testing.T) {
	v := testVault(t)

	path, patch, err := v.ApplyPatch("System/Config.md", "# Config\n")
	if err != nil {
		t.Fatal(err)
	}
	if path != "System/Config.md" {
		t.Fatalf("got %q", path)
	}
	if patch == "" {
		t.Fatal("expected diff before write")
	}
}
