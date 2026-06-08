package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchNotesPrefersExactTitleAndPathMatch(t *testing.T) {
	v := testVault(t)
	writeSearchNote(t, v, "Knowledge/Physical Context.md", "# Physical Context\n\nPlain note.\n")
	writeSearchNote(t, v, "Knowledge/Other.md", "# Other\n\nPhysical Context appears in body.\n")

	results, err := v.SearchNotes(SearchNotesOptions{Query: "Physical Context", Limit: 10, IncludeSnippets: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("got no results")
	}
	if results[0].Path != "Knowledge/Physical Context.md" {
		t.Fatalf("got top path %q, want Knowledge/Physical Context.md", results[0].Path)
	}
	if results[0].Title != "Physical Context" {
		t.Fatalf("got title %q", results[0].Title)
	}
}

func TestSearchNotesMatchesHeadings(t *testing.T) {
	v := testVault(t)
	writeSearchNote(t, v, "System/Ingest.md", "# Ingest\n\n## Responsibility Boundary\n\nRules live here.\n")

	results, err := v.SearchNotes(SearchNotesOptions{Query: "responsibility boundary", Prefix: "System/", IncludeSnippets: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Path != "System/Ingest.md" {
		t.Fatalf("got path %q", results[0].Path)
	}
	if len(results[0].MatchedHeadings) != 1 || results[0].MatchedHeadings[0] != "Responsibility Boundary" {
		t.Fatalf("got headings %v", results[0].MatchedHeadings)
	}
}

func TestSearchNotesMatchesBody(t *testing.T) {
	v := testVault(t)
	writeSearchNote(t, v, "Knowledge/Workflow.md", "# Workflow\n\nPOPO guidance appears in body text only.\n")

	results, err := v.SearchNotes(SearchNotesOptions{Query: "POPO", IncludeSnippets: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].Path != "Knowledge/Workflow.md" {
		t.Fatalf("got results %v, want Knowledge/Workflow.md", results)
	}
	if len(results[0].Snippets) == 0 || !strings.Contains(results[0].Snippets[0], "POPO") {
		t.Fatalf("got snippets %v", results[0].Snippets)
	}
}

func TestSearchNotesPrefixFiltering(t *testing.T) {
	v := testVault(t)
	writeSearchNote(t, v, "Knowledge/Ingest.md", "# Ingest\n")
	writeSearchNote(t, v, "System/Ingest.md", "# Ingest\n")

	results, err := v.SearchNotes(SearchNotesOptions{Query: "ingest", Prefix: "System/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1: %v", len(results), results)
	}
	if results[0].Path != "System/Ingest.md" {
		t.Fatalf("got path %q", results[0].Path)
	}
}

func TestSearchNotesNoResults(t *testing.T) {
	v := testVault(t)

	results, err := v.SearchNotes(SearchNotesOptions{Query: "no such phrase"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("got results %v, want none", results)
	}
}

func TestSearchNotesSnippetsStayShort(t *testing.T) {
	v := testVault(t)
	long := strings.Repeat("before ", 60) + "ingest responsibility boundary " + strings.Repeat("after ", 60)
	writeSearchNote(t, v, "System/Long.md", "# Long\n\n"+long+"\n")

	results, err := v.SearchNotes(SearchNotesOptions{Query: "ingest", Prefix: "System/", IncludeSnippets: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || len(results[0].Snippets) == 0 {
		t.Fatalf("got results %v", results)
	}
	if len(results[0].Snippets[0]) > maxSnippetLength {
		t.Fatalf("got snippet length %d, want <= %d", len(results[0].Snippets[0]), maxSnippetLength)
	}
}

func TestSearchNotesIgnoresNonMarkdownAndDeletedFiles(t *testing.T) {
	v := testVault(t)
	if err := os.WriteFile(filepath.Join(v.Root(), "Knowledge", "Skip.txt"), []byte("ingest"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(v.Root(), "Knowledge", "Deleted.md")
	if err := os.WriteFile(path, []byte("# Deleted\n\ningest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	results, err := v.SearchNotes(SearchNotesOptions{Query: "ingest"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("got results %v, want none", results)
	}
}

func writeSearchNote(t *testing.T, v *Vault, path, content string) {
	t.Helper()
	abs := filepath.Join(v.Root(), filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
