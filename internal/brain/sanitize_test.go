package brain

import "testing"

func TestSanitizeFilenameTitlePlainDuplicateTitleFirstLine(t *testing.T) {
	content := "Brain Structure Migration Proposal\nStatus: Draft proposal\nCreated: 2026-06-13\n# Goals\n"
	want := "Status: Draft proposal\nCreated: 2026-06-13\n# Goals\n"

	if got := sanitizeFilenameTitle("System/Brain Structure Migration Proposal.md", content); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSanitizeFilenameTitleH1DuplicateTitleFirstLine(t *testing.T) {
	content := "# Brain Structure Migration Proposal\nStatus: Draft proposal\n"
	want := "Status: Draft proposal\n"

	if got := sanitizeFilenameTitle("System/Brain Structure Migration Proposal.md", content); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSanitizeFilenameTitleSetextDuplicateTitle(t *testing.T) {
	content := "Brain Structure Migration Proposal\n==================================\nStatus: Draft proposal\n"
	want := "Status: Draft proposal\n"

	if got := sanitizeFilenameTitle("System/Brain Structure Migration Proposal.md", content); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSanitizeFilenameTitleNoDuplicateTitle(t *testing.T) {
	content := "# Goals\nBrain Structure Migration Proposal\n"

	if got := sanitizeFilenameTitle("System/Brain Structure Migration Proposal.md", content); got != content {
		t.Fatalf("got %q, want unchanged", got)
	}
}

func TestSanitizeFilenameTitlePartialTitleShouldNotBeRemoved(t *testing.T) {
	content := "Brain Structure Migration Proposal: Draft\nStatus: Draft proposal\n"

	if got := sanitizeFilenameTitle("System/Brain Structure Migration Proposal.md", content); got != content {
		t.Fatalf("got %q, want unchanged", got)
	}
}

func TestSanitizeFilenameTitleLaterTitleOccurrenceShouldNotBeRemoved(t *testing.T) {
	content := "# Goals\n\nBrain Structure Migration Proposal\n"

	if got := sanitizeFilenameTitle("System/Brain Structure Migration Proposal.md", content); got != content {
		t.Fatalf("got %q, want unchanged", got)
	}
}

func TestSanitizeFilenameTitleNonDuplicateH1ShouldNotBeRemoved(t *testing.T) {
	content := "# Filename-as-Title Rule\nIn Brain, the filename is the note title.\n"

	if got := sanitizeFilenameTitle("System/Note Formatting Rules.md", content); got != content {
		t.Fatalf("got %q, want unchanged", got)
	}
}

func TestSanitizeFilenameTitleH1ShouldNotBeDowngradedToH2(t *testing.T) {
	content := "# Filename-as-Title Rule\n## Details\n"

	if got := sanitizeFilenameTitle("System/Note Formatting Rules.md", content); got != content {
		t.Fatalf("got %q, want unchanged", got)
	}
}

func TestSanitizeFilenameTitleReadmeShouldNotBeSanitized(t *testing.T) {
	content := "# _README\nGateway note\n"

	if got := sanitizeFilenameTitle("System/_README.md", content); got != content {
		t.Fatalf("got %q, want unchanged", got)
	}
}

func TestApplyPatchSanitizesFilenameTitle(t *testing.T) {
	v := testVault(t)

	_, _, err := v.ApplyPatch("System/Config.md", "# Config\n\n# Settings\n")
	if err != nil {
		t.Fatal(err)
	}
	_, content, err := v.ReadNote("System/Config.md")
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Settings\n"; content != want {
		t.Fatalf("got content %q, want %q", content, want)
	}
}

func TestCreateNoteSanitizesFilenameTitle(t *testing.T) {
	v := testVault(t)

	_, _, err := v.CreateNote("Knowledge/Preferences.md", "Preferences\n\n# Hardware\n")
	if err != nil {
		t.Fatal(err)
	}
	_, content, err := v.ReadNote("Knowledge/Preferences.md")
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Hardware\n"; content != want {
		t.Fatalf("got content %q, want %q", content, want)
	}
}
