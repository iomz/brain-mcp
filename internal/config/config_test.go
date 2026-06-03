package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultDirUsesCurrentDirByDefault(t *testing.T) {
	t.Setenv("BRAIN_MCP_CONFIG_FILE", "")

	dir, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != "." {
		t.Fatalf("got %q, want .", dir)
	}
}

func TestLoadCreatesConfigDirAndFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("BRAIN_MCP_CONFIG_FILE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Dir != "." {
		t.Fatalf("got dir %q", cfg.Dir)
	}
	if cfg.File != ".env" {
		t.Fatalf("got file %q", cfg.File)
	}
	if _, err := os.Stat(filepath.Join(dir, cfg.File)); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReadsConfigAndEnvironmentOverrides(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "brain.env")
	if err := os.WriteFile(file, []byte("BRAIN_ROOT=/from/file\nBRAIN_MCP_ADDR='127.0.0.1:9000'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BRAIN_MCP_CONFIG_FILE", file)
	t.Setenv("BRAIN_ROOT", "/from/env")
	t.Setenv("BRAIN_MCP_ADDR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Get("BRAIN_ROOT"); got != "/from/env" {
		t.Fatalf("got %q, want env override", got)
	}
	if got := cfg.Get("BRAIN_MCP_ADDR"); got != "127.0.0.1:9000" {
		t.Fatalf("got %q, want file value", got)
	}
}
