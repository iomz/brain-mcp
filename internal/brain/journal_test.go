package brain

import (
	"os"
	"path/filepath"
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
