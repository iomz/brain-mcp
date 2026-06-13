package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTodayJournalResolvesDailyPath(t *testing.T) {
	v := testVault(t)

	journal, err := v.TodayJournal(time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if journal.Path != "Journal/2026-06-05.md" {
		t.Fatalf("got %q", journal.Path)
	}
	if journal.Exists {
		t.Fatal("expected missing daily journal to resolve without existing")
	}

	if err := os.WriteFile(filepath.Join(v.Root(), "Journal", "2026-06-05.md"), []byte("# Today\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal, err = v.TodayJournal(time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !journal.Exists {
		t.Fatal("expected existing daily journal")
	}
}

func TestRecentJournalsNewestFirst(t *testing.T) {
	v := testVault(t)
	for _, name := range []string{"2026-06-04.md", "2026-06-05.md", "2026-05-31.md"} {
		if err := os.WriteFile(filepath.Join(v.Root(), "Journal", name), []byte("# Journal\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	journals, err := v.RecentJournals(2)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Journal/2026-06-05.md", "Journal/2026-06-04.md"}
	for i := range want {
		if journals[i].Path != want[i] || !journals[i].Exists {
			t.Fatalf("got %+v, want %s exists", journals, want[i])
		}
	}
}

func TestAppendToTodayJournalAppendsToFileEndWithoutHeading(t *testing.T) {
	v := testVault(t)
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	writeNoteFile(t, v, "Journal/2026-06-05.md", "# Today\n\nExisting.\n")

	result, err := v.AppendToTodayJournal(now, "", "New entry.")
	if err != nil {
		t.Fatal(err)
	}

	if result.Path != "Journal/2026-06-05.md" || result.Heading != "" {
		t.Fatalf("got result %+v", result)
	}
	got := readNoteFile(t, v, "Journal/2026-06-05.md")
	want := "# Today\n\nExisting.\n\nNew entry.\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if !strings.Contains(result.Diff, "+New entry.") || result.AppendedPreview != "New entry." {
		t.Fatalf("result missing diff or preview: %+v", result)
	}
}

func TestAppendToTodayJournalAppendsToExistingHeading(t *testing.T) {
	v := testVault(t)
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	writeNoteFile(t, v, "Journal/2026-06-05.md", "# Today\n\n## Notes\n\nOld.\n\n## Tasks\n\n- Keep\n")

	result, err := v.AppendToTodayJournal(now, "Notes", "New.")
	if err != nil {
		t.Fatal(err)
	}

	if result.Heading != "## Notes" {
		t.Fatalf("got heading %q", result.Heading)
	}
	got := readNoteFile(t, v, "Journal/2026-06-05.md")
	want := "# Today\n\n## Notes\n\nOld.\n\nNew.\n\n## Tasks\n\n- Keep\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestAppendToTodayJournalCreatesMissingHeading(t *testing.T) {
	v := testVault(t)
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	writeNoteFile(t, v, "Journal/2026-06-05.md", "# Today\n\nExisting.\n")

	result, err := v.AppendToTodayJournal(now, "Notes", "New.")
	if err != nil {
		t.Fatal(err)
	}

	if result.Heading != "## Notes" {
		t.Fatalf("got heading %q", result.Heading)
	}
	got := readNoteFile(t, v, "Journal/2026-06-05.md")
	want := "# Today\n\nExisting.\n\n## Notes\n\nNew.\n\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestAppendToTodayJournalStripsLeadingDuplicateHeading(t *testing.T) {
	v := testVault(t)
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	writeNoteFile(t, v, "Journal/2026-06-05.md", "# Today\n\nExisting.\n")

	result, err := v.AppendToTodayJournal(now, "Notes", "## Notes\n\nNew.")
	if err != nil {
		t.Fatal(err)
	}

	if result.Heading != "## Notes" {
		t.Fatalf("got heading %q", result.Heading)
	}
	got := readNoteFile(t, v, "Journal/2026-06-05.md")
	want := "# Today\n\nExisting.\n\n## Notes\n\nNew.\n\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if strings.Count(got, "## Notes") != 1 {
		t.Fatalf("duplicate heading created:\n%s", got)
	}
}

func TestAppendToTodayJournalCreatesMissingJournal(t *testing.T) {
	v := testVault(t)
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)

	result, err := v.AppendToTodayJournal(now, "## Notes", "First.")
	if err != nil {
		t.Fatal(err)
	}

	if result.Path != "Journal/2026-06-05.md" || result.Heading != "## Notes" {
		t.Fatalf("got result %+v", result)
	}
	got := readNoteFile(t, v, "Journal/2026-06-05.md")
	want := "## Notes\n\nFirst.\n\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
