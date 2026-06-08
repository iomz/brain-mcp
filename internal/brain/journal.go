package brain

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type JournalConfig struct {
	Root           string `json:"root"`
	DailyPattern   string `json:"daily_pattern"`
	MonthlyPattern string `json:"monthly_pattern"`
	YearlyPattern  string `json:"yearly_pattern"`
}

type JournalNote struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

type JournalAppendResult struct {
	Path            string `json:"path"`
	Heading         string `json:"heading,omitempty"`
	Diff            string `json:"diff"`
	AppendedPreview string `json:"appended_preview"`
}

func DefaultJournalConfig() JournalConfig {
	return JournalConfig{
		Root:           "Journal/",
		DailyPattern:   "Journal/YYYY-MM-DD.md",
		MonthlyPattern: "Journal/YYYY-MM.md",
		YearlyPattern:  "Journal/YYYY.md",
	}
}

func (v *Vault) JournalConfig() JournalConfig {
	return DefaultJournalConfig()
}

func (v *Vault) TodayJournal(now time.Time) (JournalNote, error) {
	path := "Journal/" + now.Format("2006-01-02") + ".md"
	_, abs, err := v.ResolveReadPath(path)
	if err != nil {
		return JournalNote{}, err
	}
	return JournalNote{Path: path, Exists: fileExists(abs)}, nil
}

func (v *Vault) AppendToTodayJournal(now time.Time, heading, content string) (JournalAppendResult, error) {
	journal, err := v.TodayJournal(now)
	if err != nil {
		return JournalAppendResult{}, err
	}
	clean, abs, err := v.ResolveWritePath(journal.Path)
	if err != nil {
		return JournalAppendResult{}, err
	}
	oldBytes, err := os.ReadFile(abs)
	if err != nil && !os.IsNotExist(err) {
		return JournalAppendResult{}, v.filePathError(journal.Path, clean, abs, err)
	}
	oldContent := string(oldBytes)

	heading = strings.TrimSpace(heading)
	appended := normalizeJournalAppendContent(content)
	nextContent := ""
	if heading == "" {
		nextContent = appendBlockToFile(oldContent, appended)
	} else {
		nextContent, heading, err = appendToJournalHeading(oldContent, heading, appended)
		if err != nil {
			return JournalAppendResult{}, err
		}
	}

	clean, patch, err := v.ApplyPatch(clean, nextContent)
	if err != nil {
		return JournalAppendResult{}, err
	}
	return JournalAppendResult{
		Path:            clean,
		Heading:         heading,
		Diff:            patch,
		AppendedPreview: appendedPreview(appended),
	}, nil
}

func (v *Vault) RecentJournals(limit int) ([]JournalNote, error) {
	if limit <= 0 {
		limit = 10
	}
	notes, err := v.ListNotes(strings.TrimSuffix(v.JournalConfig().Root, "/"))
	if err != nil {
		return nil, err
	}
	out := make([]JournalNote, 0, len(notes))
	for _, note := range notes {
		out = append(out, JournalNote{Path: note, Exists: true})
	}
	sort.Slice(out, func(i, j int) bool {
		iDate, iOK := journalDate(out[i].Path)
		jDate, jOK := journalDate(out[j].Path)
		if iOK && jOK {
			return iDate.After(jDate)
		}
		if iOK != jOK {
			return iOK
		}
		return out[i].Path > out[j].Path
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func journalDate(path string) (time.Time, bool) {
	name := strings.TrimSuffix(filepath.Base(path), ".md")
	parsed, err := time.Parse("2006-01-02", name)
	return parsed, err == nil
}

func fileExists(path string) bool {
	info, err := os.Stat(filepath.Clean(path))
	return err == nil && !info.IsDir()
}

func appendToJournalHeading(markdown, heading, content string) (string, string, error) {
	spec, err := parseHeadingSpec(heading)
	if err != nil {
		return "", "", err
	}
	headings := markdownHeadings(markdown)
	idx := findHeading(headings, spec)
	if idx >= 0 {
		end := sectionEnd(markdown, headings, idx)
		return markdown[:end] + appendContentBlock(markdown[headings[idx].end:end], normalizeSectionContent(content)) + markdown[end:], headings[idx].line, nil
	}
	headingLine := journalHeadingLine(heading)
	return appendBlockToFile(markdown, formatSectionBlock(headingLine, content)), headingLine, nil
}

func journalHeadingLine(heading string) string {
	heading = strings.TrimSpace(heading)
	if parsed, ok := parseMarkdownHeading(heading, 0, len(heading)); ok {
		return strings.Repeat("#", parsed.level) + " " + parsed.text
	}
	return "## " + heading
}

func normalizeJournalAppendContent(content string) string {
	content = strings.Trim(content, "\n")
	if content == "" {
		return ""
	}
	return content + "\n"
}

func appendedPreview(content string) string {
	content = strings.TrimSpace(content)
	if len(content) <= 200 {
		return content
	}
	return content[:200]
}
