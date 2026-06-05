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
