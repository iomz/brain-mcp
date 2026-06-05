package brain

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPathValidationRejectsUnsafePaths(t *testing.T) {
	v := testVault(t)

	tests := []struct {
		name string
		path string
		err  error
	}{
		{"absolute", filepath.Join(v.Root(), "Knowledge", "Self.md"), ErrAbsolutePath},
		{"traversal", "../Self.md", ErrPathTraversal},
		{"nested traversal", "Knowledge/../Self.md", ErrPathTraversal},
		{"hidden", ".secret/Self.md", ErrHiddenPath},
		{".git", ".git/config.md", ErrHiddenPath},
		{"non markdown write", "Knowledge/Self.txt", ErrNotMarkdown},
		{"forbidden write", "Inbox/Note.md", ErrPathForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := v.ResolveWritePath(tt.path)
			if !errors.Is(err, tt.err) {
				t.Fatalf("got %v, want %v", err, tt.err)
			}
		})
	}
}

func TestPathValidationRejectsExplicitReadonlyPaths(t *testing.T) {
	v := testVaultWithPolicy(t, Policy{
		WritablePaths: []string{"Knowledge/"},
		ReadonlyPaths: []string{"Journal/"},
		RequireGit:    false,
	})

	_, _, err := v.ResolveWritePath("Journal/Today.md")
	if !errors.Is(err, ErrReadOnlyPath) {
		t.Fatalf("got %v, want %v", err, ErrReadOnlyPath)
	}
	var pathErr PathError
	if !errors.As(err, &pathErr) || pathErr.Code != ReasonReadOnlyRoot {
		t.Fatalf("got %v, want reason %s", err, ReasonReadOnlyRoot)
	}
}

func TestPathValidationRejectsSymlinkEscape(t *testing.T) {
	v := testVault(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "Outside.md"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(v.Root(), "Knowledge", "Escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	_, _, err := v.ResolveReadPath("Knowledge/Escape/Outside.md")
	if !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("got %v, want %v", err, ErrOutsideRoot)
	}
}

func testVault(t *testing.T) *Vault {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Knowledge", "Self.md"), []byte("# Self\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Journal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Journal", "Today.md"), []byte("# Today\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := NewVaultWithPolicy(root, Policy{
		WritablePaths: DefaultWritablePaths(),
		ReadonlyPaths: DefaultReadonlyPaths(),
		RequireGit:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func testVaultWithPolicy(t *testing.T, policy Policy) *Vault {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Knowledge", "Self.md"), []byte("# Self\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Journal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Journal", "Today.md"), []byte("# Today\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := NewVaultWithPolicy(root, policy)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
