package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const DefaultEnvFile = ".env"

type Config struct {
	Dir    string
	File   string
	Values map[string]string
}

func Load() (Config, error) {
	file := strings.TrimSpace(os.Getenv("BRAIN_MCP_CONFIG_FILE"))
	if file == "" {
		file = DefaultEnvFile
	} else {
		file = expandHome(file)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return Config{}, err
	}
	if err := ensureConfigFile(file); err != nil {
		return Config{}, err
	}

	values, err := readEnvFile(file)
	if err != nil {
		return Config{}, err
	}
	return Config{
		Dir:    filepath.Dir(file),
		File:   file,
		Values: values,
	}, nil
}

func DefaultDir() (string, error) {
	file := strings.TrimSpace(os.Getenv("BRAIN_MCP_CONFIG_FILE"))
	if file == "" {
		return ".", nil
	}
	return filepath.Dir(expandHome(file)), nil
}

func (c Config) Get(key string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return c.Values[key]
}

func (c Config) GetDefault(key, fallback string) string {
	if value := c.Get(key); value != "" {
		return value
	}
	return fallback
}

func ensureConfigFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(`# brain-mcp configuration
# Environment variables override values in this file.
#
# BRAIN_ROOT=/path/to/Brain
# BRAIN_ROOT_HOST=/path/to/Brain
# BRAIN_MCP_AUTH_MODE=mixed
# BRAIN_MCP_TOKEN=replace-with-long-random-token
# BRAIN_MCP_ADDR=127.0.0.1:8787
# BRAIN_MCP_OAUTH_RESOURCE=https://brain.sazanka.io/mcp
# BRAIN_MCP_OAUTH_ISSUER=https://auth.sazanka.io/application/o/brain-mcp/
# BRAIN_MCP_OAUTH_JWKS_URL=https://auth.sazanka.io/application/o/brain-mcp/jwks/
# BRAIN_MCP_OAUTH_CLIENT_ID=<client-id>
# BRAIN_MCP_OAUTH_ACCEPTED_AUDIENCES=https://brain.sazanka.io/mcp,<client-id>
# BRAIN_MCP_OAUTH_AUTHORIZATION_SERVER=https://auth.sazanka.io/application/o/brain-mcp/
# BRAIN_MCP_OAUTH_AUTHORIZATION_SERVERS=https://auth.sazanka.io/application/o/brain-mcp/
# BRAIN_MCP_OAUTH_DCR_ENABLED=false
# BRAIN_MCP_OAUTH_AUTHORIZATION_SERVER_ISSUER=https://brain.sazanka.io
# BRAIN_MCP_OAUTH_APPROVAL_TOKEN=replace-with-local-approval-token
# BRAIN_MCP_OAUTH_SUBJECT=brain-mcp-user
# BRAIN_MCP_OAUTH_EMAIL=replace-me@example.com
# BRAIN_MCP_OAUTH_STATE_FILE=.brain-mcp-oauth-state.json
# BRAIN_MCP_OAUTH_AUTHENTIK_APPROVAL_ENABLED=false
# BRAIN_MCP_OAUTH_AUTHENTIK_CLIENT_ID=<authentik-client-id>
# BRAIN_MCP_OAUTH_AUTHENTIK_CLIENT_SECRET=
# BRAIN_MCP_OAUTH_AUTHENTIK_AUTHORIZE_URL=https://auth.sazanka.io/application/o/brain-mcp/authorize/
# BRAIN_MCP_OAUTH_AUTHENTIK_TOKEN_URL=https://auth.sazanka.io/application/o/brain-mcp/token/
# BRAIN_MCP_OAUTH_AUTHENTIK_REDIRECT_URI=https://brain.sazanka.io/oauth/authentik/callback
# BRAIN_MCP_OAUTH_SCOPES=openid,email,profile,brain:read,brain:write,brain:git,brain:admin
# BRAIN_MCP_OAUTH_DEFAULT_SCOPES=brain:read
# BRAIN_MCP_BEARER_SCOPES=brain:read,brain:write,brain:git
# BRAIN_MCP_ALLOWED_SUBJECTS=
# BRAIN_MCP_ALLOWED_EMAILS=
# BRAIN_MCP_ALLOWED_GROUPS=
# BRAIN_MCP_AUTH_DEBUG=false
# BRAIN_MCP_WRITABLE_PATHS=Knowledge/,System/,Active/,Archive/
# BRAIN_MCP_READONLY_PATHS=Journal/
# BRAIN_MCP_REQUIRE_GIT=true
# CLOUDFLARED_TUNNEL_TOKEN=replace-with-cloudflare-token
`)
	return err
}

func readEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid config line: %s", line)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("invalid config line: %s", line)
		}
		values[key] = unquote(strings.TrimSpace(value))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func unquote(value string) string {
	if len(value) < 2 {
		return value
	}
	first := value[0]
	last := value[len(value)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
