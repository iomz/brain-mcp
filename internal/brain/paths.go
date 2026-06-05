package brain

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrAbsolutePath  = errors.New("path must be relative")
	ErrPathTraversal = errors.New("path must not contain ..")
	ErrOutsideRoot   = errors.New("path escapes BRAIN_ROOT")
	ErrNotMarkdown   = errors.New("write path must end with .md")
	ErrHiddenPath    = errors.New("hidden path segments are not allowed")
	ErrPathForbidden = errors.New("path is not allowed by policy")
	ErrReadOnlyPath  = errors.New("path is read-only")
	ErrGitRequired   = errors.New("BRAIN_ROOT must be a git repository")
	ErrFileExists    = errors.New("file already exists")
)

const (
	ReasonMissingScope         = "missing_scope"
	ReasonPathOutsideVault     = "path_outside_vault"
	ReasonReadOnlyRoot         = "read_only_root"
	ReasonCreateDisabled       = "create_disabled"
	ReasonFileExists           = "file_exists"
	ReasonFileMissing          = "file_missing"
	ReasonParentMissing        = "parent_missing"
	ReasonPathValidationFailed = "path_validation_failed"
)

type PathError struct {
	Code           string
	RequestedPath  string
	NormalizedPath string
	ResolvedPath   string
	FileExists     bool
	ParentExists   bool
	Err            error
}

func (e PathError) Error() string {
	message := e.Code
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	fields := []string{}
	if e.RequestedPath != "" {
		fields = append(fields, "path="+e.RequestedPath)
	}
	if e.NormalizedPath != "" {
		fields = append(fields, "normalized="+e.NormalizedPath)
	}
	if e.ResolvedPath != "" {
		fields = append(fields, "resolved="+e.ResolvedPath)
		fields = append(fields, fmt.Sprintf("exists=%t", e.FileExists))
		fields = append(fields, fmt.Sprintf("parent_exists=%t", e.ParentExists))
	}
	if len(fields) > 0 {
		message += " (" + strings.Join(fields, " ") + ")"
	}
	return message
}

func (e PathError) Unwrap() error {
	return e.Err
}

func (e PathError) Is(target error) bool {
	return errors.Is(e.Err, target)
}

type Vault struct {
	root          string
	writablePaths []string
	readonlyPaths []string
	requireGit    bool
}

func NewVault(root string) (*Vault, error) {
	return NewVaultWithPolicy(root, Policy{
		WritablePaths: DefaultWritablePaths(),
		ReadonlyPaths: DefaultReadonlyPaths(),
		RequireGit:    true,
	})
}

type Policy struct {
	WritablePaths []string
	ReadonlyPaths []string
	RequireGit    bool
}

func DefaultWritablePaths() []string {
	return []string{"Knowledge/", "System/", "Active/", "Archive/", "Journal/"}
}

func DefaultReadonlyPaths() []string {
	return nil
}

func NewVaultWithPolicy(root string, policy Policy) (*Vault, error) {
	if root == "" {
		return nil, errors.New("root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root is not a directory: %s", abs)
	}
	eval, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	v := &Vault{
		root:          filepath.Clean(eval),
		writablePaths: normalizePrefixes(policy.WritablePaths),
		readonlyPaths: normalizePrefixes(policy.ReadonlyPaths),
		requireGit:    policy.RequireGit,
	}
	if len(v.writablePaths) == 0 && len(v.readonlyPaths) == 0 {
		return nil, errors.New("at least one allowed path is required")
	}
	if v.requireGit {
		if _, err := os.Stat(filepath.Join(v.root, ".git")); err != nil {
			return nil, ErrGitRequired
		}
	}
	return v, nil
}

func (v *Vault) Root() string {
	return v.root
}

func (v *Vault) WritablePaths() []string {
	return append([]string(nil), v.writablePaths...)
}

func (v *Vault) ReadonlyPaths() []string {
	return append([]string(nil), v.readonlyPaths...)
}

func (v *Vault) ResolveReadPath(rel string) (string, string, error) {
	return v.resolve(rel, false)
}

func (v *Vault) ResolveWritePath(rel string) (string, string, error) {
	return v.resolve(rel, true)
}

func (v *Vault) resolve(rel string, write bool) (string, string, error) {
	clean, err := cleanRelative(rel)
	if err != nil {
		return "", "", v.pathError(rel, "", "", reasonForPathError(err), err)
	}
	if write && strings.ToLower(filepath.Ext(clean)) != ".md" {
		return "", "", v.pathError(rel, clean, filepath.Join(v.root, clean), ReasonPathValidationFailed, ErrNotMarkdown)
	}
	if write {
		if !matchesPrefix(clean, v.writablePaths) {
			if matchesPrefix(clean, v.readonlyPaths) {
				return "", "", v.pathError(rel, clean, filepath.Join(v.root, clean), ReasonReadOnlyRoot, ErrReadOnlyPath)
			}
			return "", "", v.pathError(rel, clean, filepath.Join(v.root, clean), ReasonPathOutsideVault, ErrPathForbidden)
		}
	} else if !matchesPrefix(clean, append(v.writablePaths, v.readonlyPaths...)) {
		return "", "", v.pathError(rel, clean, filepath.Join(v.root, clean), ReasonPathOutsideVault, ErrPathForbidden)
	}

	abs := filepath.Join(v.root, clean)
	if err := ensureInside(v.root, abs); err != nil {
		return "", "", v.pathError(rel, clean, abs, reasonForPathError(err), err)
	}
	if err := v.ensureNoSymlinkEscape(abs); err != nil {
		return "", "", v.pathError(rel, clean, abs, reasonForPathError(err), err)
	}
	return clean, abs, nil
}

func (v *Vault) pathError(requested, normalized, resolved, code string, err error) PathError {
	pe := PathError{
		Code:           code,
		RequestedPath:  requested,
		NormalizedPath: normalized,
		ResolvedPath:   resolved,
		Err:            err,
	}
	if resolved != "" {
		if _, statErr := os.Stat(resolved); statErr == nil {
			pe.FileExists = true
		}
		if _, statErr := os.Stat(filepath.Dir(resolved)); statErr == nil {
			pe.ParentExists = true
		}
	}
	return pe
}

func reasonForPathError(err error) string {
	switch {
	case errors.Is(err, ErrOutsideRoot):
		return ReasonPathOutsideVault
	case errors.Is(err, ErrReadOnlyPath):
		return ReasonReadOnlyRoot
	case errors.Is(err, ErrPathForbidden):
		return ReasonPathOutsideVault
	default:
		return ReasonPathValidationFailed
	}
}

func normalizePrefixes(prefixes []string) []string {
	out := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		prefix = filepath.ToSlash(filepath.Clean(prefix))
		if prefix == "." || strings.HasPrefix(prefix, ".") || strings.Contains(prefix, "..") || strings.HasPrefix(prefix, "/") {
			continue
		}
		out = append(out, strings.TrimSuffix(prefix, "/")+"/")
	}
	return out
}

func matchesPrefix(path string, prefixes []string) bool {
	slashPath := filepath.ToSlash(path)
	if slashPath == "." {
		return true
	}
	for _, prefix := range prefixes {
		dir := strings.TrimSuffix(prefix, "/")
		if slashPath == dir || strings.HasPrefix(slashPath, prefix) {
			return true
		}
	}
	return false
}

func cleanRelative(rel string) (string, error) {
	if rel == "" {
		rel = "."
	}
	if filepath.IsAbs(rel) {
		return "", ErrAbsolutePath
	}
	parts := strings.FieldsFunc(rel, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	for _, part := range parts {
		if part == ".." {
			return "", ErrPathTraversal
		}
		if strings.HasPrefix(part, ".") && part != "." {
			return "", ErrHiddenPath
		}
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", ErrPathTraversal
	}
	return clean, nil
}

func ensureInside(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return ErrOutsideRoot
	}
	return nil
}

func (v *Vault) ensureNoSymlinkEscape(abs string) error {
	current := v.root
	rel, err := filepath.Rel(v.root, abs)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		eval, err := filepath.EvalSymlinks(current)
		if err != nil {
			if os.IsNotExist(err) {
				parent := filepath.Dir(current)
				parentEval, parentErr := filepath.EvalSymlinks(parent)
				if parentErr != nil {
					return parentErr
				}
				return ensureInside(v.root, parentEval)
			}
			return err
		}
		if err := ensureInside(v.root, eval); err != nil {
			return err
		}
	}
	return nil
}
