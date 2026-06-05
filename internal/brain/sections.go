package brain

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/iomz/brain-mcp/internal/diff"
)

var (
	ErrHeadingRequired  = errors.New("heading is required")
	ErrSectionNotFound  = errors.New("section not found")
	ErrDuplicateHeading = errors.New("multiple matching headings found")
	ErrTextNotFound     = errors.New("text not found")
	ErrTextNotUnique    = errors.New("text occurs multiple times")
)

type headingSpec struct {
	level    int
	text     string
	hasLevel bool
}

type markdownHeading struct {
	level int
	text  string
	line  string
	start int
	end   int
}

func (v *Vault) AppendSection(path, heading, content string) (string, string, error) {
	clean, abs, oldContent, err := v.readWritableMarkdown(path)
	if err != nil {
		return "", "", err
	}
	nextContent, err := editSection(oldContent, heading, content, true)
	if err != nil {
		return "", "", err
	}
	if err := v.writeResolvedSection(clean, abs, nextContent); err != nil {
		return "", "", err
	}
	return clean, diff.Unified(clean, oldContent, nextContent), nil
}

func (v *Vault) GetSection(path, heading string) (string, string, error) {
	clean, _, markdown, err := v.readMarkdown(path)
	if err != nil {
		return "", "", err
	}
	idx, headings, err := findUniqueExactHeading(markdown, heading)
	if err != nil {
		return "", "", err
	}
	return clean, markdown[headings[idx].start:sectionEnd(markdown, headings, idx)], nil
}

func (v *Vault) ReplaceSection(path, heading, content string) (string, string, error) {
	clean, _, oldContent, err := v.readWritableMarkdown(path)
	if err != nil {
		return "", "", err
	}
	nextContent, err := replaceSectionExact(oldContent, heading, content)
	if err != nil {
		return "", "", err
	}
	clean, patch, err := v.ApplyPatch(clean, nextContent)
	if err != nil {
		return "", "", err
	}
	return clean, patch, nil
}

func (v *Vault) UpsertSection(path, heading, content, parentHeading string) (string, string, error) {
	clean, abs, err := v.ResolveWritePath(path)
	if err != nil {
		return "", "", err
	}
	oldContentBytes, err := os.ReadFile(abs)
	if err != nil && !os.IsNotExist(err) {
		return "", "", v.filePathError(path, clean, abs, err)
	}
	oldContent := string(oldContentBytes)
	nextContent, err := upsertSectionExact(oldContent, heading, content, parentHeading)
	if err != nil {
		return "", "", err
	}
	clean, patch, err := v.ApplyPatch(clean, nextContent)
	if err != nil {
		return "", "", err
	}
	return clean, patch, nil
}

func (v *Vault) DeleteDuplicateSection(path, heading, keep string) (string, string, error) {
	clean, _, oldContent, err := v.readWritableMarkdown(path)
	if err != nil {
		return "", "", err
	}
	nextContent, err := deleteDuplicateSectionExact(oldContent, heading, keep)
	if err != nil {
		return "", "", err
	}
	clean, patch, err := v.ApplyPatch(clean, nextContent)
	if err != nil {
		return "", "", err
	}
	return clean, patch, nil
}

func (v *Vault) ReplaceText(path, oldText, newText string) (string, string, error) {
	clean, _, oldContent, err := v.readWritableMarkdown(path)
	if err != nil {
		return "", "", err
	}
	count := strings.Count(oldContent, oldText)
	if count == 0 {
		return "", "", ErrTextNotFound
	}
	if count > 1 {
		return "", "", ErrTextNotUnique
	}
	nextContent := strings.Replace(oldContent, oldText, newText, 1)
	clean, patch, err := v.ApplyPatch(clean, nextContent)
	if err != nil {
		return "", "", err
	}
	return clean, patch, nil
}

func (v *Vault) readMarkdown(path string) (string, string, string, error) {
	clean, abs, err := v.ResolveReadPath(path)
	if err != nil {
		return "", "", "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", "", "", v.filePathError(path, clean, abs, err)
	}
	return clean, abs, string(data), nil
}

func (v *Vault) readWritableMarkdown(path string) (string, string, string, error) {
	clean, abs, err := v.ResolveWritePath(path)
	if err != nil {
		return "", "", "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", "", "", v.filePathError(path, clean, abs, err)
	}
	return clean, abs, string(data), nil
}

func (v *Vault) writeResolvedSection(clean, abs, content string) error {
	parent := filepath.Dir(abs)
	if err := v.ensureNoSymlinkEscape(parent); err != nil {
		return err
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(content), 0o644)
}

func editSection(markdown, heading, sectionContent string, appendContent bool) (string, error) {
	spec, err := parseHeadingSpec(heading)
	if err != nil {
		return "", err
	}
	headings := markdownHeadings(markdown)
	idx := findHeading(headings, spec)
	if idx < 0 {
		return "", ErrSectionNotFound
	}

	sectionStart := headings[idx].end
	sectionEnd := len(markdown)
	for i := idx + 1; i < len(headings); i++ {
		if headings[i].level <= headings[idx].level {
			sectionEnd = headings[i].start
			break
		}
	}

	content := normalizeSectionContent(sectionContent)
	if appendContent {
		return markdown[:sectionEnd] + appendContentBlock(markdown[sectionStart:sectionEnd], content) + markdown[sectionEnd:], nil
	}
	return markdown[:sectionStart] + content + markdown[sectionEnd:], nil
}

func replaceSectionExact(markdown, heading, content string) (string, error) {
	idx, headings, err := findUniqueExactHeading(markdown, heading)
	if err != nil {
		return "", err
	}
	start := headings[idx].end
	end := sectionEnd(markdown, headings, idx)
	return markdown[:start] + normalizeSectionContent(content) + markdown[end:], nil
}

func upsertSectionExact(markdown, heading, content, parentHeading string) (string, error) {
	matches, headings, err := findExactHeadings(markdown, heading)
	if err != nil {
		return "", err
	}
	if len(matches) > 1 {
		return "", ErrDuplicateHeading
	}
	if len(matches) == 1 {
		idx := matches[0]
		start := headings[idx].end
		end := sectionEnd(markdown, headings, idx)
		return markdown[:start] + normalizeSectionContent(content) + markdown[end:], nil
	}

	block := formatSectionBlock(heading, content)
	if parentHeading == "" {
		return appendBlockToFile(markdown, block), nil
	}
	parentIdx, _, err := findUniqueExactHeading(markdown, parentHeading)
	if err != nil {
		return "", err
	}
	insertAt := sectionEnd(markdown, headings, parentIdx)
	return markdown[:insertAt] + insertionBlock(markdown[:insertAt], markdown[insertAt:], block) + markdown[insertAt:], nil
}

func deleteDuplicateSectionExact(markdown, heading, keep string) (string, error) {
	matches, headings, err := findExactHeadings(markdown, heading)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", ErrSectionNotFound
	}
	if len(matches) == 1 {
		return markdown, nil
	}
	keepIdx := matches[0]
	if keep == "last" {
		keepIdx = matches[len(matches)-1]
	} else if keep != "first" {
		return "", errors.New("keep must be first or last")
	}
	var b strings.Builder
	pos := 0
	for _, idx := range matches {
		if idx == keepIdx {
			continue
		}
		start := headings[idx].start
		end := sectionEnd(markdown, headings, idx)
		b.WriteString(markdown[pos:start])
		pos = end
	}
	b.WriteString(markdown[pos:])
	return b.String(), nil
}

func findUniqueExactHeading(markdown, heading string) (int, []markdownHeading, error) {
	matches, headings, err := findExactHeadings(markdown, heading)
	if err != nil {
		return -1, nil, err
	}
	if len(matches) == 0 {
		return -1, nil, ErrSectionNotFound
	}
	if len(matches) > 1 {
		return -1, nil, ErrDuplicateHeading
	}
	return matches[0], headings, nil
}

func findExactHeadings(markdown, heading string) ([]int, []markdownHeading, error) {
	heading = strings.TrimSpace(heading)
	if heading == "" {
		return nil, nil, ErrHeadingRequired
	}
	if _, ok := parseMarkdownHeading(heading, 0, len(heading)); !ok {
		return nil, nil, ErrHeadingRequired
	}
	headings := markdownHeadings(markdown)
	var matches []int
	for i, h := range headings {
		if h.line == heading {
			matches = append(matches, i)
		}
	}
	return matches, headings, nil
}

func sectionEnd(markdown string, headings []markdownHeading, idx int) int {
	for i := idx + 1; i < len(headings); i++ {
		if headings[i].level <= headings[idx].level {
			return headings[i].start
		}
	}
	return len(markdown)
}

func parseHeadingSpec(raw string) (headingSpec, error) {
	line := strings.TrimSpace(raw)
	if line == "" {
		return headingSpec{}, ErrHeadingRequired
	}
	if h, ok := parseMarkdownHeading(line, 0, len(line)); ok {
		return headingSpec{level: h.level, text: h.text, hasLevel: true}, nil
	}
	return headingSpec{text: strings.TrimSpace(line)}, nil
}

func findHeading(headings []markdownHeading, spec headingSpec) int {
	for i, heading := range headings {
		if heading.text != spec.text {
			continue
		}
		if spec.hasLevel && heading.level != spec.level {
			continue
		}
		return i
	}
	return -1
}

func markdownHeadings(markdown string) []markdownHeading {
	var headings []markdownHeading
	for start := 0; start < len(markdown); {
		end := strings.IndexByte(markdown[start:], '\n')
		if end < 0 {
			end = len(markdown)
		} else {
			end += start + 1
		}
		if heading, ok := parseMarkdownHeading(markdown[start:end], start, end); ok {
			headings = append(headings, heading)
		}
		start = end
	}
	return headings
}

func parseMarkdownHeading(line string, start, end int) (markdownHeading, bool) {
	text := strings.TrimRight(line, "\r\n")
	i := 0
	for i < len(text) && i < 3 && text[i] == ' ' {
		i++
	}
	level := 0
	for i+level < len(text) && level < 6 && text[i+level] == '#' {
		level++
	}
	if level == 0 {
		return markdownHeading{}, false
	}
	afterHashes := i + level
	if afterHashes < len(text) && text[afterHashes] != ' ' && text[afterHashes] != '\t' {
		return markdownHeading{}, false
	}
	content := strings.TrimSpace(text[afterHashes:])
	content = strings.TrimSpace(strings.TrimRight(content, "#"))
	if content == "" {
		return markdownHeading{}, false
	}
	return markdownHeading{level: level, text: content, line: strings.TrimSpace(text), start: start, end: end}, true
}

func normalizeSectionContent(content string) string {
	content = strings.Trim(content, "\n")
	if content == "" {
		return "\n"
	}
	return "\n" + content + "\n\n"
}

func appendContentBlock(existing, content string) string {
	if strings.TrimSpace(existing) == "" {
		return content
	}
	prefix := ""
	if !strings.HasSuffix(existing, "\n") {
		prefix = "\n"
	}
	if strings.HasSuffix(existing, "\n\n") {
		return prefix + strings.TrimPrefix(content, "\n")
	}
	return prefix + content
}

func formatSectionBlock(heading, content string) string {
	return strings.TrimSpace(heading) + "\n" + normalizeSectionContent(content)
}

func appendBlockToFile(markdown, block string) string {
	if strings.TrimSpace(markdown) == "" {
		return block
	}
	separator := "\n\n"
	if strings.HasSuffix(markdown, "\n\n") {
		separator = ""
	} else if strings.HasSuffix(markdown, "\n") {
		separator = "\n"
	}
	return markdown + separator + block
}

func insertionBlock(before, after, block string) string {
	prefix := "\n\n"
	if strings.HasSuffix(before, "\n\n") {
		prefix = ""
	} else if strings.HasSuffix(before, "\n") {
		prefix = "\n"
	}
	suffix := ""
	if after != "" && !strings.HasPrefix(after, "\n") && !strings.HasSuffix(block, "\n") {
		suffix = "\n"
	}
	return prefix + block + suffix
}
