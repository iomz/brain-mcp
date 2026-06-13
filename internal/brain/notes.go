package brain

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/iomz/brain-mcp/internal/diff"
)

func (v *Vault) ReadNote(path string) (string, string, error) {
	clean, abs, err := v.ResolveReadPath(path)
	if err != nil {
		return "", "", err
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return "", "", v.filePathError(path, clean, abs, err)
	}
	return clean, string(content), nil
}

func (v *Vault) WriteNote(path, content string) (string, error) {
	clean, _, err := v.ApplyPatch(path, content)
	return clean, err
}

func (v *Vault) CreateNote(path, content string) (string, int, error) {
	clean, abs, err := v.ResolveWritePath(path)
	if err != nil {
		return "", 0, err
	}
	if _, err := os.Stat(abs); err == nil {
		return "", 0, v.pathError(path, clean, abs, ReasonFileExists, ErrFileExists)
	} else if !os.IsNotExist(err) {
		return "", 0, v.filePathError(path, clean, abs, err)
	}
	parent := filepath.Dir(abs)
	if err := v.ensureNoSymlinkEscape(parent); err != nil {
		return "", 0, err
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", 0, err
	}
	content = sanitizeFilenameTitle(clean, content)
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	data := []byte(content)
	file, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return "", 0, v.pathError(path, clean, abs, ReasonFileExists, ErrFileExists)
		}
		return "", 0, err
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return "", 0, err
	}
	log.Printf("brain_create path=%s bytes=%d", clean, len(data))
	return clean, len(data), nil
}

func (v *Vault) ShowDiff(path, proposedContent string) (string, string, error) {
	clean, oldContent, err := v.ReadNote(path)
	if err != nil {
		if os.IsNotExist(err) {
			clean, _, resolveErr := v.ResolveWritePath(path)
			if resolveErr != nil {
				return "", "", resolveErr
			}
			proposedContent = sanitizeFilenameTitle(clean, proposedContent)
			return clean, diff.Unified(clean, "", proposedContent), nil
		}
		return "", "", err
	}
	proposedContent = sanitizeFilenameTitle(clean, proposedContent)
	return clean, diff.Unified(clean, oldContent, proposedContent), nil
}

func (v *Vault) ApplyPatch(path, proposedContent string) (string, string, error) {
	clean, abs, err := v.ResolveWritePath(path)
	if err != nil {
		return "", "", err
	}
	nextContent := sanitizeFilenameTitle(clean, proposedContent)
	patch, err := v.diffForResolvedPath(clean, abs, nextContent)
	if err != nil {
		return "", "", err
	}
	parent := filepath.Dir(abs)
	if err := v.ensureNoSymlinkEscape(parent); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(abs, []byte(nextContent), 0o644); err != nil {
		return "", "", err
	}
	log.Printf("brain_write path=%s diff_bytes=%d", clean, len(patch))
	return clean, patch, nil
}

func (v *Vault) ListNotes(dir string) ([]string, error) {
	clean, abs, err := v.ResolveReadPath(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, v.filePathError(dir, clean, abs, err)
	}
	if !info.IsDir() {
		return nil, fs.ErrInvalid
	}

	var notes []string
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
		notes = append(notes, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(notes)
	if clean == "." {
		return notes, nil
	}
	return notes, nil
}

func (v *Vault) filePathError(requested, normalized, resolved string, err error) error {
	code := ReasonPathValidationFailed
	if errors.Is(err, os.ErrNotExist) {
		code = ReasonFileMissing
		if _, parentErr := os.Stat(filepath.Dir(resolved)); errors.Is(parentErr, os.ErrNotExist) {
			code = ReasonParentMissing
		}
	}
	return v.pathError(requested, normalized, resolved, code, err)
}

func (v *Vault) diffForResolvedPath(clean, abs, proposedContent string) (string, error) {
	oldBytes, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return diff.Unified(clean, "", proposedContent), nil
		}
		return "", err
	}
	return diff.Unified(clean, string(oldBytes), proposedContent), nil
}
