package brain

import (
	"path/filepath"
	"regexp"
	"strings"
)

var setextH1Underline = regexp.MustCompile(`^=+$`)

func sanitizeFilenameTitle(path, content string) string {
	base := filepath.Base(filepath.ToSlash(path))
	if isFilenameTitleExempt(base) {
		return content
	}

	title := strings.TrimSuffix(base, filepath.Ext(base))
	if title == "" {
		return content
	}

	lines := strings.SplitAfter(content, "\n")
	first := firstNonblankLine(lines)
	if first < 0 {
		return content
	}

	line := trimLineEnding(lines[first])
	removeUntil := -1
	if line == title {
		next := first + 1
		if next < len(lines) && isSetextH1Underline(lines[next]) {
			removeUntil = next + 1
		} else {
			removeUntil = first + 1
		}
	} else if line == "# "+title {
		removeUntil = first + 1
	}

	if removeUntil < 0 {
		return content
	}
	if removeUntil < len(lines) && isBlankLine(lines[removeUntil]) {
		removeUntil++
	}

	var out strings.Builder
	for _, kept := range lines[:first] {
		out.WriteString(kept)
	}
	for _, kept := range lines[removeUntil:] {
		out.WriteString(kept)
	}
	return out.String()
}

func isFilenameTitleExempt(base string) bool {
	switch base {
	case "_README.md", "README.md", "Index.md", "index.md":
		return true
	default:
		return false
	}
}

func firstNonblankLine(lines []string) int {
	for i, line := range lines {
		if !isBlankLine(line) {
			return i
		}
	}
	return -1
}

func isBlankLine(line string) bool {
	return strings.TrimSpace(trimLineEnding(line)) == ""
}

func isSetextH1Underline(line string) bool {
	return setextH1Underline.MatchString(strings.TrimSpace(trimLineEnding(line)))
}

func trimLineEnding(line string) string {
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
}
