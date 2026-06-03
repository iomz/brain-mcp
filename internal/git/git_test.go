package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitRequiresMessage(t *testing.T) {
	root := testRepo(t)

	_, err := Commit(root, "  ")
	if !errors.Is(err, ErrEmptyCommitMessage) {
		t.Fatalf("got %v, want %v", err, ErrEmptyCommitMessage)
	}
}

func TestCommitRefusesCleanWorkingTree(t *testing.T) {
	root := testRepo(t)

	_, err := Commit(root, "clean commit")
	if !errors.Is(err, ErrCleanWorkingTree) {
		t.Fatalf("got %v, want %v", err, ErrCleanWorkingTree)
	}
}

func TestCommitReturnsHashAndLeavesCleanTree(t *testing.T) {
	root := testRepo(t)
	if err := os.WriteFile(filepath.Join(root, "Knowledge", "Self.md"), []byte("# Self\n\nUpdated.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hash, err := Commit(root, "update self")
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != 40 {
		t.Fatalf("got hash %q", hash)
	}
	if status, err := Status(root); err != nil {
		t.Fatal(err)
	} else if strings.TrimSpace(status) != "" {
		t.Fatalf("got dirty status %q", status)
	}
}

func testRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Knowledge", "Self.md"), []byte("# Self\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "brain-mcp@example.test")
	runGit(t, root, "config", "user.name", "brain-mcp tests")
	runGit(t, root, "config", "commit.gpgsign", "false")
	runGit(t, root, "add", "--all", "--")
	runGit(t, root, "commit", "-m", "initial")
	return root
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
