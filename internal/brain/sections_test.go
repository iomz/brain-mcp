package brain

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendSectionPreservesRestOfFileAndReturnsDiff(t *testing.T) {
	v := testVault(t)
	writeNoteFile(t, v, "Knowledge/Self.md", "# Self\n\nIntro.\n\n## Notes\n\nOld note.\n\n## Tasks\n\n- Keep\n")

	path, patch, err := v.AppendSection("Knowledge/Self.md", "Notes", "New note.")
	if err != nil {
		t.Fatal(err)
	}

	if path != "Knowledge/Self.md" {
		t.Fatalf("got path %q", path)
	}
	got := readNoteFile(t, v, "Knowledge/Self.md")
	want := "# Self\n\nIntro.\n\n## Notes\n\nOld note.\n\nNew note.\n\n## Tasks\n\n- Keep\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if !strings.Contains(patch, "+New note.") {
		t.Fatalf("diff missing appended content:\n%s", patch)
	}
}

func TestGetSectionReturnsHeadingAndBody(t *testing.T) {
	v := testVault(t)
	writeNoteFile(t, v, "Knowledge/Self.md", "# Self\n\nIntro.\n\n## Profile\n\nOld profile.\n\n### Detail\n\nOld detail.\n\n## Tasks\n\n- Keep\n")

	path, section, err := v.GetSection("Knowledge/Self.md", "## Profile")
	if err != nil {
		t.Fatal(err)
	}

	if path != "Knowledge/Self.md" {
		t.Fatalf("got path %q", path)
	}
	want := "## Profile\n\nOld profile.\n\n### Detail\n\nOld detail.\n\n"
	if section != want {
		t.Fatalf("got:\n%s\nwant:\n%s", section, want)
	}
}

func TestReplaceSectionPreservesSurroundingSections(t *testing.T) {
	v := testVault(t)
	writeNoteFile(t, v, "Knowledge/Self.md", "# Self\n\nIntro.\n\n## Profile\n\nOld profile.\n\n### Detail\n\nOld detail.\n\n## Tasks\n\n- Keep\n")

	_, patch, err := v.ReplaceSection("Knowledge/Self.md", "## Profile", "New profile.\n\n### Detail\n\nNew detail.")
	if err != nil {
		t.Fatal(err)
	}

	got := readNoteFile(t, v, "Knowledge/Self.md")
	want := "# Self\n\nIntro.\n\n## Profile\n\nNew profile.\n\n### Detail\n\nNew detail.\n\n## Tasks\n\n- Keep\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if !strings.Contains(patch, "-Old profile.") || !strings.Contains(patch, "+New profile.") {
		t.Fatalf("diff missing replacement:\n%s", patch)
	}
}

func TestReplaceSectionMissingHeading(t *testing.T) {
	v := testVault(t)

	_, _, err := v.ReplaceSection("Knowledge/Self.md", "## Missing", "Updated.")
	if !errors.Is(err, ErrSectionNotFound) {
		t.Fatalf("got %v, want %v", err, ErrSectionNotFound)
	}
}

func TestUpsertSectionReplacesExistingInsteadOfDuplicating(t *testing.T) {
	v := testVault(t)
	writeNoteFile(t, v, "Knowledge/Self.md", "# Self\n\n## Notes\n\nOld.\n")

	_, _, err := v.UpsertSection("Knowledge/Self.md", "## Notes", "New.", "")
	if err != nil {
		t.Fatal(err)
	}

	got := readNoteFile(t, v, "Knowledge/Self.md")
	want := "# Self\n\n## Notes\n\nNew.\n\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if strings.Count(got, "## Notes") != 1 {
		t.Fatalf("duplicate heading created:\n%s", got)
	}
}

func TestUpsertSectionInsertsUnderParentHeadingWhenMissing(t *testing.T) {
	v := testVault(t)
	writeNoteFile(t, v, "Knowledge/Self.md", "# Self\n\n## Parent\n\nParent body.\n\n## Sibling\n\nKeep.\n")

	_, _, err := v.UpsertSection("Knowledge/Self.md", "### Child", "Child body.", "## Parent")
	if err != nil {
		t.Fatal(err)
	}

	got := readNoteFile(t, v, "Knowledge/Self.md")
	want := "# Self\n\n## Parent\n\nParent body.\n\n### Child\n\nChild body.\n\n## Sibling\n\nKeep.\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpsertSectionFailsWhenParentHeadingDoesNotExist(t *testing.T) {
	v := testVault(t)

	_, _, err := v.UpsertSection("Knowledge/Self.md", "## Notes", "New.", "## Missing")
	if !errors.Is(err, ErrSectionNotFound) {
		t.Fatalf("got %v, want %v", err, ErrSectionNotFound)
	}
}

func TestUpsertSectionFailsOnDuplicateMatchingHeadings(t *testing.T) {
	v := testVault(t)
	writeNoteFile(t, v, "Knowledge/Self.md", "# Self\n\n## Notes\n\nOne.\n\n## Notes\n\nTwo.\n")

	_, _, err := v.UpsertSection("Knowledge/Self.md", "## Notes", "New.", "")
	if !errors.Is(err, ErrDuplicateHeading) {
		t.Fatalf("got %v, want %v", err, ErrDuplicateHeading)
	}
}

func TestDeleteDuplicateSectionKeepsFirst(t *testing.T) {
	v := testVault(t)
	writeNoteFile(t, v, "Knowledge/Self.md", "# Self\n\n## Notes\n\nOne.\n\n## Other\n\nKeep.\n\n## Notes\n\nTwo.\n")

	_, _, err := v.DeleteDuplicateSection("Knowledge/Self.md", "## Notes", "first")
	if err != nil {
		t.Fatal(err)
	}

	got := readNoteFile(t, v, "Knowledge/Self.md")
	want := "# Self\n\n## Notes\n\nOne.\n\n## Other\n\nKeep.\n\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDeleteDuplicateSectionKeepsLast(t *testing.T) {
	v := testVault(t)
	writeNoteFile(t, v, "Knowledge/Self.md", "# Self\n\n## Notes\n\nOne.\n\n## Other\n\nKeep.\n\n## Notes\n\nTwo.\n")

	_, _, err := v.DeleteDuplicateSection("Knowledge/Self.md", "## Notes", "last")
	if err != nil {
		t.Fatal(err)
	}

	got := readNoteFile(t, v, "Knowledge/Self.md")
	want := "# Self\n\n## Other\n\nKeep.\n\n## Notes\n\nTwo.\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDeleteDuplicateSectionNoOpsWithOneSection(t *testing.T) {
	v := testVault(t)
	writeNoteFile(t, v, "Knowledge/Self.md", "# Self\n\n## Notes\n\nOne.\n")

	_, patch, err := v.DeleteDuplicateSection("Knowledge/Self.md", "## Notes", "first")
	if err != nil {
		t.Fatal(err)
	}

	got := readNoteFile(t, v, "Knowledge/Self.md")
	want := "# Self\n\n## Notes\n\nOne.\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(patch, "+") || strings.Contains(patch, "-") {
		t.Fatalf("expected no-op diff, got:\n%s", patch)
	}
}

func TestReplaceTextReplacesExactlyOneOccurrence(t *testing.T) {
	v := testVault(t)
	writeNoteFile(t, v, "Knowledge/Self.md", "# Self\n\nOld sentence.\n")

	_, patch, err := v.ReplaceText("Knowledge/Self.md", "Old sentence.", "New sentence.")
	if err != nil {
		t.Fatal(err)
	}

	got := readNoteFile(t, v, "Knowledge/Self.md")
	if got != "# Self\n\nNew sentence.\n" {
		t.Fatalf("got:\n%s", got)
	}
	if !strings.Contains(patch, "-Old sentence.") || !strings.Contains(patch, "+New sentence.") {
		t.Fatalf("diff missing text replacement:\n%s", patch)
	}
}

func TestReplaceTextFailsOnZeroOccurrences(t *testing.T) {
	v := testVault(t)

	_, _, err := v.ReplaceText("Knowledge/Self.md", "Missing", "New")
	if !errors.Is(err, ErrTextNotFound) {
		t.Fatalf("got %v, want %v", err, ErrTextNotFound)
	}
}

func TestReplaceTextFailsOnMultipleOccurrences(t *testing.T) {
	v := testVault(t)
	writeNoteFile(t, v, "Knowledge/Self.md", "# Self\n\nSame.\n\nSame.\n")

	_, _, err := v.ReplaceText("Knowledge/Self.md", "Same.", "New.")
	if !errors.Is(err, ErrTextNotUnique) {
		t.Fatalf("got %v, want %v", err, ErrTextNotUnique)
	}
}

func TestSectionWriteToolsRejectReadonlyPaths(t *testing.T) {
	v := testVault(t)

	writeChecks := []struct {
		name string
		fn   func() error
	}{
		{"replace section", func() error {
			_, _, err := v.ReplaceSection("Journal/Today.md", "# Today", "Updated.")
			return err
		}},
		{"upsert section", func() error {
			_, _, err := v.UpsertSection("Journal/Today.md", "## Notes", "Updated.", "")
			return err
		}},
		{"delete duplicate section", func() error {
			_, _, err := v.DeleteDuplicateSection("Journal/Today.md", "# Today", "first")
			return err
		}},
		{"replace text", func() error {
			_, _, err := v.ReplaceText("Journal/Today.md", "# Today", "# Updated")
			return err
		}},
	}

	for _, tt := range writeChecks {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fn(); !errors.Is(err, ErrReadOnlyPath) {
				t.Fatalf("got %v, want %v", err, ErrReadOnlyPath)
			}
		})
	}
}

func TestSectionWriteToolsRejectUnsafePaths(t *testing.T) {
	v := testVault(t)

	paths := []struct {
		name string
		path string
		err  error
	}{
		{"traversal", "../Knowledge/Self.md", ErrPathTraversal},
		{"absolute", filepath.Join(v.Root(), "Knowledge", "Self.md"), ErrAbsolutePath},
		{".git", ".git/config.md", ErrHiddenPath},
		{"hidden", "Knowledge/.secret/Self.md", ErrHiddenPath},
	}

	for _, tt := range paths {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := v.UpsertSection(tt.path, "## Notes", "New.", "")
			if !errors.Is(err, tt.err) {
				t.Fatalf("got %v, want %v", err, tt.err)
			}
		})
	}
}

func writeNoteFile(t *testing.T, v *Vault, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(v.Root(), filepath.FromSlash(path))), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v.Root(), filepath.FromSlash(path)), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readNoteFile(t *testing.T, v *Vault, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(v.Root(), filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
