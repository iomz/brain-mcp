package brain

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const (
	defaultSearchLimit = 10
	maxSnippetLength   = 180
	maxSnippets        = 3
)

type SearchNotesOptions struct {
	Query           string
	Prefix          string
	Limit           int
	IncludeSnippets bool
}

type SearchResult struct {
	Path            string   `json:"path"`
	Title           string   `json:"title"`
	Score           int      `json:"score"`
	MatchedHeadings []string `json:"matched_headings"`
	Snippets        []string `json:"snippets,omitempty"`
}

func (v *Vault) SearchNotes(opts SearchNotesOptions) ([]SearchResult, error) {
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return nil, nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	prefix := opts.Prefix
	if prefix == "" {
		prefix = "."
	}
	cleanPrefix, abs, err := v.ResolveReadPath(prefix)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, v.filePathError(prefix, cleanPrefix, abs, err)
	}
	if !info.IsDir() {
		return nil, fs.ErrInvalid
	}

	needle := strings.ToLower(query)
	terms := searchTerms(query)
	results := []SearchResult{}
	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() != "." && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			if err := v.ensureNoSymlinkEscape(path); err != nil {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || strings.ToLower(filepath.Ext(d.Name())) != ".md" {
			return nil
		}
		if err := v.ensureNoSymlinkEscape(path); err != nil {
			return nil
		}
		rel, err := filepath.Rel(v.root, path)
		if err != nil {
			return err
		}
		clean := filepath.ToSlash(rel)
		if _, _, err := v.ResolveReadPath(clean); err != nil {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		result := scoreNote(clean, string(data), needle, terms, opts.IncludeSnippets)
		if result.Score > 0 {
			results = append(results, result)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Path < results[j].Path
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func scoreNote(path, content, phrase string, terms []string, includeSnippets bool) SearchResult {
	headings := searchMarkdownHeadings(content)
	title := noteTitle(path, headings)
	lowerPath := strings.ToLower(path)
	lowerTitle := strings.ToLower(title)
	lowerContent := strings.ToLower(content)
	pathTermCounts := termCounts(path)
	titleTermCounts := termCounts(title)
	contentTermCounts := termCounts(content)

	score := 0
	if strings.Contains(lowerPath, phrase) {
		score += 120
	}
	if strings.Contains(lowerTitle, phrase) {
		score += 140
	}
	if strings.Contains(lowerContent, phrase) {
		score += 50
	}

	matchedHeadings := []string{}
	for _, heading := range headings {
		lowerHeading := strings.ToLower(heading)
		if strings.Contains(lowerHeading, phrase) {
			score += 100
			matchedHeadings = appendUnique(matchedHeadings, heading)
			continue
		}
		headingTermCounts := termCounts(heading)
		if containsAnyTerm(headingTermCounts, terms) {
			score += 35 * matchingTermCount(headingTermCounts, terms)
			matchedHeadings = appendUnique(matchedHeadings, heading)
		}
	}

	for _, term := range terms {
		if pathTermCounts[term] > 0 {
			score += 40
		}
		if titleTermCounts[term] > 0 {
			score += 45
		}
		score += contentTermCounts[term]
	}

	result := SearchResult{
		Path:            path,
		Title:           title,
		Score:           score,
		MatchedHeadings: matchedHeadings,
	}
	if includeSnippets {
		result.Snippets = snippets(content, phrase, terms)
	}
	return result
}

func searchMarkdownHeadings(content string) []string {
	headings := []string{}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		i := 0
		for i < len(trimmed) && trimmed[i] == '#' {
			i++
		}
		if i == 0 || i > 6 || i >= len(trimmed) || trimmed[i] != ' ' {
			continue
		}
		heading := strings.TrimSpace(trimmed[i+1:])
		if heading != "" {
			headings = append(headings, heading)
		}
	}
	return headings
}

func noteTitle(path string, headings []string) string {
	if len(headings) > 0 {
		return headings[0]
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return strings.TrimSpace(base)
}

func searchTerms(query string) []string {
	seen := map[string]bool{}
	terms := []string{}
	for _, term := range tokenizeSearchText(query) {
		if seen[term] {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
	}
	return terms
}

func termCounts(value string) map[string]int {
	counts := map[string]int{}
	for _, term := range tokenizeSearchText(value) {
		counts[term]++
	}
	return counts
}

func tokenizeSearchText(value string) []string {
	terms := []string{}
	for _, term := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if term != "" {
			terms = append(terms, term)
		}
	}
	return terms
}

func containsAnyTerm(counts map[string]int, terms []string) bool {
	for _, term := range terms {
		if counts[term] > 0 {
			return true
		}
	}
	return false
}

func matchingTermCount(counts map[string]int, terms []string) int {
	count := 0
	for _, term := range terms {
		if counts[term] > 0 {
			count++
		}
	}
	return count
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func snippets(content, phrase string, terms []string) []string {
	out := []string{}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.Join(strings.Fields(line), " ")
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if !strings.Contains(lower, phrase) && !containsAnyTerm(termCounts(trimmed), terms) {
			continue
		}
		out = append(out, truncateSnippet(trimmed))
		if len(out) >= maxSnippets {
			break
		}
	}
	return out
}

func truncateSnippet(snippet string) string {
	if len(snippet) <= maxSnippetLength {
		return snippet
	}
	return strings.TrimSpace(snippet[:maxSnippetLength-3]) + "..."
}
