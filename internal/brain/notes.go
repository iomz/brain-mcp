package brain

import (
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
		return "", "", err
	}
	return clean, string(content), nil
}

func (v *Vault) WriteNote(path, content string) (string, error) {
	clean, _, err := v.ApplyPatch(path, content)
	return clean, err
}

func (v *Vault) ShowDiff(path, proposedContent string) (string, string, error) {
	clean, oldContent, err := v.ReadNote(path)
	if err != nil {
		if os.IsNotExist(err) {
			clean, _, resolveErr := v.ResolveWritePath(path)
			if resolveErr != nil {
				return "", "", resolveErr
			}
			return clean, diff.Unified(clean, "", proposedContent), nil
		}
		return "", "", err
	}
	return clean, diff.Unified(clean, oldContent, proposedContent), nil
}

func (v *Vault) ApplyPatch(path, proposedContent string) (string, string, error) {
	clean, abs, err := v.ResolveWritePath(path)
	if err != nil {
		return "", "", err
	}
	patch, err := v.diffForResolvedPath(clean, abs, proposedContent)
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
	if err := os.WriteFile(abs, []byte(proposedContent), 0o644); err != nil {
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
		return nil, err
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
