package brain

import (
	"errors"
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

func TestListNotesCoversJournalNestedEmptyAndMissingDirs(t *testing.T) {
	v := testVault(t)
	if err := os.MkdirAll(filepath.Join(v.Root(), "Journal", "2026", "06"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v.Root(), "Journal", "2026-06-05.md"), []byte("# Today\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v.Root(), "Journal", "2026", "06", "Nested.md"), []byte("# Nested\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(v.Root(), "Archive", "Empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	notes, err := v.ListNotes("Journal")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Journal/2026-06-05.md", "Journal/2026/06/Nested.md", "Journal/Today.md"}
	if len(notes) != len(want) {
		t.Fatalf("got %v, want %v", notes, want)
	}
	for i := range want {
		if notes[i] != want[i] {
			t.Fatalf("got %v, want %v", notes, want)
		}
	}

	notes, err = v.ListNotes("Archive/Empty")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Fatalf("got %v, want empty list", notes)
	}

	_, err = v.ListNotes("Journal/Missing")
	var pathErr PathError
	if !errors.As(err, &pathErr) || pathErr.Code != ReasonFileMissing {
		t.Fatalf("got %v, want %s", err, ReasonFileMissing)
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

func TestCreateNoteCreatesMarkdownNote(t *testing.T) {
	v := testVault(t)

	path, bytesWritten, err := v.CreateNote("Knowledge/Preferences.md", "# Preferences\n\n- Sauna\n")
	if err != nil {
		t.Fatal(err)
	}
	if path != "Knowledge/Preferences.md" {
		t.Fatalf("got %q", path)
	}
	content, err := os.ReadFile(filepath.Join(v.Root(), "Knowledge", "Preferences.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "# Preferences\n\n- Sauna\n" {
		t.Fatalf("got content %q", content)
	}
	if bytesWritten != len(content) {
		t.Fatalf("got bytes %d, want %d", bytesWritten, len(content))
	}
}

func TestCreateNoteCreatesParentDirectories(t *testing.T) {
	v := testVault(t)

	path, _, err := v.CreateNote("Knowledge/Interests/Sauna.md", "# Sauna")
	if err != nil {
		t.Fatal(err)
	}
	if path != "Knowledge/Interests/Sauna.md" {
		t.Fatalf("got %q", path)
	}
	content, err := os.ReadFile(filepath.Join(v.Root(), "Knowledge", "Interests", "Sauna.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "# Sauna\n" {
		t.Fatalf("got content %q", content)
	}
}

func TestCreateNoteRefusesOverwrite(t *testing.T) {
	v := testVault(t)

	_, _, err := v.CreateNote("Knowledge/Self.md", "# Changed\n")
	if !errors.Is(err, ErrFileExists) {
		t.Fatalf("got %v, want %v", err, ErrFileExists)
	}
	_, content, readErr := v.ReadNote("Knowledge/Self.md")
	if readErr != nil {
		t.Fatal(readErr)
	}
	if content != "# Self\n" {
		t.Fatalf("got content %q", content)
	}
}

func TestCreateNoteRejectsUnsafePaths(t *testing.T) {
	v := testVault(t)

	tests := []struct {
		name string
		path string
		err  error
	}{
		{"absolute", filepath.Join(v.Root(), "Knowledge", "New.md"), ErrAbsolutePath},
		{"traversal", "../outside.md", ErrPathTraversal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := v.CreateNote(tt.path, "# New\n")
			if !errors.Is(err, tt.err) {
				t.Fatalf("got %v, want %v", err, tt.err)
			}
		})
	}
}

func TestCreateNoteEnsuresFinalNewline(t *testing.T) {
	v := testVault(t)

	if _, _, err := v.CreateNote("Knowledge/Newline.md", "# Newline"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(v.Root(), "Knowledge", "Newline.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "# Newline\n" {
		t.Fatalf("got content %q", content)
	}
}

func TestCreateNoteWritesUTF8Content(t *testing.T) {
	v := testVault(t)

	want := "# Preferences\n\n- 温泉とサウナ\n"
	if _, _, err := v.CreateNote("Knowledge/UTF8.md", want); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(v.Root(), "Knowledge", "UTF8.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != want {
		t.Fatalf("got content %q, want %q", content, want)
	}
}
