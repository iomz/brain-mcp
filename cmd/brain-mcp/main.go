package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/iomz/brain-mcp/internal/brain"
	"github.com/iomz/brain-mcp/internal/config"
	"github.com/iomz/brain-mcp/internal/httpapi"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	root := cfg.Get("BRAIN_ROOT")
	if root == "" {
		fmt.Fprintf(os.Stderr, "BRAIN_ROOT is required; set it in environment or %s\n", cfg.File)
		os.Exit(1)
	}
	token := cfg.Get("BRAIN_MCP_TOKEN")
	if token == "" {
		fmt.Fprintf(os.Stderr, "BRAIN_MCP_TOKEN is required; set it in environment or %s\n", cfg.File)
		os.Exit(1)
	}
	addr := cfg.GetDefault("BRAIN_MCP_ADDR", "127.0.0.1:8787")

	vault, err := brain.NewVaultWithPolicy(root, brain.Policy{
		WritablePaths: configList(cfg, "BRAIN_MCP_WRITABLE_PATHS", brain.DefaultWritablePaths()),
		ReadonlyPaths: configList(cfg, "BRAIN_MCP_READONLY_PATHS", brain.DefaultReadonlyPaths()),
		RequireGit:    configBool(cfg, "BRAIN_MCP_REQUIRE_GIT", true),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid BRAIN_ROOT: %v\n", err)
		os.Exit(1)
	}

	log.Printf("brain-mcp listening addr=%s vault=%s", addr, "configured")
	if err := http.ListenAndServe(addr, httpapi.NewServer(vault, token).Handler()); err != nil {
		fmt.Fprintf(os.Stderr, "http server error: %v\n", err)
		os.Exit(1)
	}
}

func configList(cfg config.Config, key string, fallback []string) []string {
	raw := cfg.Get(key)
	if raw == "" {
		return fallback
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func configBool(cfg config.Config, key string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(cfg.Get(key)))
	if raw == "" {
		return fallback
	}
	return raw == "1" || raw == "true" || raw == "yes"
}
