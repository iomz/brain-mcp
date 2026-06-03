package git

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var (
	ErrEmptyCommitMessage = errors.New("commit message is required")
	ErrCleanWorkingTree   = errors.New("git working tree is clean")
)

func Status(root string) (string, error) {
	cmd := exec.Command("git", "-C", root, "status", "--short")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return string(exitErr.Stderr), err
		}
		return "", err
	}
	return string(out), nil
}

func Commit(root, message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", ErrEmptyCommitMessage
	}
	status, err := Status(root)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(status) == "" {
		return "", ErrCleanWorkingTree
	}
	if err := run(root, "add", "--all", "--"); err != nil {
		return "", err
	}
	if err := run(root, "commit", "-m", message); err != nil {
		return "", err
	}
	out, err := output(root, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func run(root string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func output(root string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return string(exitErr.Stderr), err
		}
		return "", err
	}
	return string(out), nil
}

func Diff(root string) (string, error) {
	cmd := exec.Command("git", "-C", root, "diff", "--")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return string(exitErr.Stderr), err
		}
		return "", err
	}
	return string(out), nil
}
