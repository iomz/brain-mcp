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
	authMode := strings.ToLower(strings.TrimSpace(cfg.GetDefault("BRAIN_MCP_AUTH_MODE", "bearer")))
	if authMode != "bearer" && authMode != "oauth" && authMode != "mixed" && authMode != "none" {
		fmt.Fprintf(os.Stderr, "invalid BRAIN_MCP_AUTH_MODE: %s\n", authMode)
		os.Exit(1)
	}
	if token == "" && (authMode == "bearer" || authMode == "mixed") {
		fmt.Fprintf(os.Stderr, "BRAIN_MCP_TOKEN is required; set it in environment or %s\n", cfg.File)
		os.Exit(1)
	}
	addr := cfg.GetDefault("BRAIN_MCP_ADDR", "127.0.0.1:8787")
	oauthDCR := configBool(cfg, "BRAIN_MCP_OAUTH_DCR_ENABLED", false)

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
	if err := http.ListenAndServe(addr, httpapi.NewServerWithAuth(vault, httpapi.AuthConfig{
		Mode:                       httpapi.AuthMode(authMode),
		BearerToken:                token,
		OAuthIssuer:                cfg.Get("BRAIN_MCP_OAUTH_ISSUER"),
		OAuthJWKSURL:               cfg.Get("BRAIN_MCP_OAUTH_JWKS_URL"),
		OAuthResource:              cfg.GetDefault("BRAIN_MCP_OAUTH_RESOURCE", "https://brain.sazanka.io/mcp"),
		OAuthClientID:              cfg.Get("BRAIN_MCP_OAUTH_CLIENT_ID"),
		OAuthAcceptedAudiences:     acceptedAudiences(cfg),
		OAuthDefaultScopes:         configList(cfg, "BRAIN_MCP_OAUTH_DEFAULT_SCOPES", []string{"brain:read"}),
		AuthorizationServers:       authorizationServers(cfg, oauthDCR),
		Scopes:                     configList(cfg, "BRAIN_MCP_OAUTH_SCOPES", []string{"brain:read", "brain:write", "brain:git", "brain:admin"}),
		BearerScopes:               configList(cfg, "BRAIN_MCP_BEARER_SCOPES", []string{"brain:read", "brain:write", "brain:git", "brain:admin"}),
		AllowedEmails:              configList(cfg, "BRAIN_MCP_ALLOWED_EMAILS", nil),
		AllowedSubjects:            configList(cfg, "BRAIN_MCP_ALLOWED_SUBJECTS", nil),
		AllowedGroups:              configList(cfg, "BRAIN_MCP_ALLOWED_GROUPS", nil),
		AuthDebug:                  configBool(cfg, "BRAIN_MCP_AUTH_DEBUG", false),
		OAuthDCR:                   oauthDCR,
		OAuthAuthorizationServer:   cfg.Get("BRAIN_MCP_OAUTH_AUTHORIZATION_SERVER_ISSUER"),
		OAuthApprovalToken:         cfg.Get("BRAIN_MCP_OAUTH_APPROVAL_TOKEN"),
		OAuthSubject:               cfg.GetDefault("BRAIN_MCP_OAUTH_SUBJECT", "brain-mcp-user"),
		OAuthEmail:                 cfg.Get("BRAIN_MCP_OAUTH_EMAIL"),
		OAuthStateFile:             cfg.GetDefault("BRAIN_MCP_OAUTH_STATE_FILE", ".brain-mcp-oauth-state.json"),
		OAuthAuthentikApproval:     configBool(cfg, "BRAIN_MCP_OAUTH_AUTHENTIK_APPROVAL_ENABLED", false),
		OAuthAuthentikClientID:     cfg.GetDefault("BRAIN_MCP_OAUTH_AUTHENTIK_CLIENT_ID", cfg.Get("BRAIN_MCP_OAUTH_CLIENT_ID")),
		OAuthAuthentikClientSecret: cfg.Get("BRAIN_MCP_OAUTH_AUTHENTIK_CLIENT_SECRET"),
		OAuthAuthentikAuthorizeURL: cfg.Get("BRAIN_MCP_OAUTH_AUTHENTIK_AUTHORIZE_URL"),
		OAuthAuthentikTokenURL:     cfg.Get("BRAIN_MCP_OAUTH_AUTHENTIK_TOKEN_URL"),
		OAuthAuthentikRedirectURI:  cfg.Get("BRAIN_MCP_OAUTH_AUTHENTIK_REDIRECT_URI"),
	}).Handler()); err != nil {
		fmt.Fprintf(os.Stderr, "http server error: %v\n", err)
		os.Exit(1)
	}
}

func authorizationServers(cfg config.Config, dcr bool) []string {
	if raw := cfg.Get("BRAIN_MCP_OAUTH_AUTHORIZATION_SERVERS"); raw != "" {
		return configList(cfg, "BRAIN_MCP_OAUTH_AUTHORIZATION_SERVERS", nil)
	}
	if dcr {
		issuer := cfg.Get("BRAIN_MCP_OAUTH_AUTHORIZATION_SERVER_ISSUER")
		if issuer == "" {
			issuer = authorizationServerOrigin(cfg.GetDefault("BRAIN_MCP_OAUTH_RESOURCE", "https://brain.sazanka.io/mcp"))
		}
		return []string{issuer}
	}
	return []string{cfg.GetDefault("BRAIN_MCP_OAUTH_AUTHORIZATION_SERVER", "https://auth.sazanka.io/application/o/brain-mcp/")}
}

func authorizationServerOrigin(resource string) string {
	resource = strings.TrimRight(resource, "/")
	if strings.HasSuffix(resource, "/mcp") {
		return strings.TrimSuffix(resource, "/mcp")
	}
	return resource
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

func acceptedAudiences(cfg config.Config) []string {
	values := configList(cfg, "BRAIN_MCP_OAUTH_ACCEPTED_AUDIENCES", nil)
	if len(values) == 0 {
		values = append(values, cfg.GetDefault("BRAIN_MCP_OAUTH_RESOURCE", "https://brain.sazanka.io/mcp"))
	}
	if clientID := strings.TrimSpace(cfg.Get("BRAIN_MCP_OAUTH_CLIENT_ID")); clientID != "" {
		seen := false
		for _, value := range values {
			if value == clientID {
				seen = true
				break
			}
		}
		if !seen {
			values = append(values, clientID)
		}
	}
	return values
}

func configBool(cfg config.Config, key string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(cfg.Get(key)))
	if raw == "" {
		return fallback
	}
	return raw == "1" || raw == "true" || raw == "yes"
}
