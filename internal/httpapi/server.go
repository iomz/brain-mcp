package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/iomz/brain-mcp/internal/brain"
	braingit "github.com/iomz/brain-mcp/internal/git"
	"github.com/iomz/brain-mcp/internal/mcp"
)

type Server struct {
	auth        AuthConfig
	vault       *brain.Vault
	mcp         *mcp.Server
	jwtVerifier *jwtVerifier
	dcrStore    *dcrStore
	localOAuth  *localOAuthServer
}

func NewServer(vault *brain.Vault, token string) *Server {
	return NewServerWithAuth(vault, DefaultAuthConfig(token))
}

type AuthMode string

const (
	AuthModeBearer AuthMode = "bearer"
	AuthModeOAuth  AuthMode = "oauth"
	AuthModeMixed  AuthMode = "mixed"
	AuthModeNone   AuthMode = "none"
)

type AuthConfig struct {
	Mode                       AuthMode
	BearerToken                string
	OAuthIssuer                string
	OAuthJWKSURL               string
	OAuthResource              string
	OAuthClientID              string
	OAuthAcceptedAudiences     []string
	OAuthDefaultScopes         []string
	AuthorizationServers       []string
	Scopes                     []string
	BearerScopes               []string
	AllowedEmails              []string
	AllowedSubjects            []string
	AllowedGroups              []string
	AuthDebug                  bool
	OAuthDCR                   bool
	OAuthAuthorizationServer   string
	OAuthApprovalToken         string
	OAuthSubject               string
	OAuthEmail                 string
	OAuthStateFile             string
	OAuthAuthentikApproval     bool
	OAuthAuthentikClientID     string
	OAuthAuthentikClientSecret string
	OAuthAuthentikAuthorizeURL string
	OAuthAuthentikTokenURL     string
	OAuthAuthentikRedirectURI  string
}

func DefaultAuthConfig(token string) AuthConfig {
	return AuthConfig{
		Mode:                 AuthModeBearer,
		BearerToken:          token,
		OAuthResource:        "https://brain.sazanka.io/mcp",
		AuthorizationServers: []string{"https://auth.sazanka.io/application/o/brain-mcp/"},
		Scopes:               []string{"brain:read", "brain:write", "brain:git", "brain:admin"},
		BearerScopes:         []string{"brain:read", "brain:write", "brain:git", "brain:admin"},
		OAuthDefaultScopes:   []string{"brain:read"},
	}
}

func NewServerWithAuth(vault *brain.Vault, auth AuthConfig) *Server {
	if auth.Mode == "" {
		auth.Mode = AuthModeBearer
	}
	if auth.OAuthResource == "" {
		auth.OAuthResource = "https://brain.sazanka.io/mcp"
	}
	if len(auth.AuthorizationServers) == 0 {
		auth.AuthorizationServers = []string{"https://auth.sazanka.io/application/o/brain-mcp/"}
	}
	if len(auth.Scopes) == 0 {
		auth.Scopes = []string{"brain:read", "brain:write", "brain:git", "brain:admin"}
	}
	if len(auth.BearerScopes) == 0 {
		auth.BearerScopes = []string{"brain:read", "brain:write", "brain:git", "brain:admin"}
	}
	if len(auth.OAuthDefaultScopes) == 0 {
		auth.OAuthDefaultScopes = []string{"brain:read"}
	}
	if auth.OAuthAuthorizationServer == "" {
		auth.OAuthAuthorizationServer = authorizationServerFromResource(auth.OAuthResource)
	}
	oauthState, err := newOAuthState(auth)
	if err != nil {
		panic(err)
	}
	return &Server{
		auth:        auth,
		vault:       vault,
		mcp:         mcp.NewServer(vault),
		jwtVerifier: newJWTVerifier(auth),
		dcrStore:    oauthState.store,
		localOAuth:  newLocalOAuthServer(auth, oauthState.key),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.oauthProtectedResource)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", s.oauthProtectedResource)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.oauthAuthorizationServer)
	mux.HandleFunc("GET /.well-known/jwks.json", s.oauthJWKS)
	mux.HandleFunc("POST /oauth/register", s.oauthRegister)
	mux.HandleFunc("GET /oauth/clients", s.oauthClients)
	mux.HandleFunc("GET /oauth/authorize", s.oauthAuthorize)
	mux.HandleFunc("POST /oauth/authorize", s.oauthAuthorize)
	mux.HandleFunc("GET /oauth/authentik/callback", s.oauthAuthentikCallback)
	mux.HandleFunc("POST /oauth/token", s.oauthToken)
	mux.HandleFunc("GET /healthz", s.withAuth(s.healthz))
	mux.HandleFunc("GET /info", s.withAuth(s.info))
	mux.HandleFunc("POST /mcp", s.mcpPost)
	return mux
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authContext, logEntry, ok, status, message := s.authenticate(r)
		if !ok {
			s.logAuthDecision(r, logEntry)
			if status == http.StatusUnauthorized {
				w.Header().Set("WWW-Authenticate", bearerResourceChallenge(s.oauthMetadataURL()))
			}
			writeError(w, status, message)
			return
		}
		r = withAuthContext(r, authContext)
		s.logAuthDecision(r, logEntry)
		next(w, r)
	}
}

func (s *Server) authenticate(r *http.Request) (mcp.AuthContext, authLog, bool, int, string) {
	if s.auth.Mode == AuthModeNone {
		authContext := mcp.AuthContext{Kind: mcp.AuthKindAnonymous}
		return authContext, authLog{Attempted: "none", Result: "accepted", Kind: string(authContext.Kind)}, true, 0, ""
	}
	if (s.auth.Mode == AuthModeBearer || s.auth.Mode == AuthModeMixed) && s.auth.BearerToken == "" {
		return mcp.AuthContext{}, authLog{Attempted: "none", Result: "rejected", RejectReason: "static_token_missing"}, false, http.StatusInternalServerError, "BRAIN_MCP_TOKEN is required"
	}
	header := r.Header.Get("Authorization")
	attempted := "none"
	rejectReason := "missing_authorization_header"
	if strings.HasPrefix(header, "Bearer ") {
		token := strings.TrimPrefix(header, "Bearer ")
		attempted = "static_bearer"
		if (s.auth.Mode == AuthModeBearer || s.auth.Mode == AuthModeMixed) && token == s.auth.BearerToken {
			authContext := mcp.AuthContext{
				Kind:   mcp.AuthKindBearerStatic,
				Scopes: s.auth.BearerScopes,
			}
			return authContext, authLog{
				Attempted: "static_bearer",
				Result:    "accepted",
				Kind:      string(authContext.Kind),
				Scopes:    authContext.Scopes,
			}, true, 0, ""
		}
		rejectReason = "static_bearer_mismatch"
		if s.auth.Mode == AuthModeOAuth || s.auth.Mode == AuthModeMixed {
			attempted = "oauth_jwt"
			if s.auth.OAuthDCR {
				authContext, err := s.localOAuth.Verify(token)
				if err == nil {
					return authContext, authLog{
						Attempted: "oauth_jwt",
						Result:    "accepted",
						Kind:      string(authContext.Kind),
						Email:     authContext.Email,
						Subject:   authContext.Subject,
						Groups:    authContext.Groups,
						Scopes:    authContext.Scopes,
						Issuer:    authContext.Issuer,
						Audience:  authContext.Audience,
						ExpiresAt: authContext.ExpiresAt,
					}, true, 0, ""
				}
			}
			authContext, err := s.jwtVerifier.Verify(token)
			if err == nil {
				return authContext, authLog{
					Attempted: "oauth_jwt",
					Result:    "accepted",
					Kind:      string(authContext.Kind),
					Email:     authContext.Email,
					Subject:   authContext.Subject,
					Groups:    authContext.Groups,
					Scopes:    authContext.Scopes,
					Issuer:    authContext.Issuer,
					Audience:  authContext.Audience,
					ExpiresAt: authContext.ExpiresAt,
				}, true, 0, ""
			}
			rejectReason = jwtRejectReason(err)
			return mcp.AuthContext{}, authLog{
				Attempted:    "oauth_jwt",
				Result:       "rejected",
				RejectReason: rejectReason,
				Claims:       jwtErrorClaims(err),
			}, false, http.StatusUnauthorized, "missing or invalid bearer token"
		}
	}
	if s.auth.Mode == AuthModeBearer {
		return mcp.AuthContext{}, authLog{
			Attempted:    attempted,
			Result:       "rejected",
			RejectReason: rejectReason,
		}, false, http.StatusUnauthorized, "missing or invalid bearer token"
	}
	return mcp.AuthContext{}, authLog{
		Attempted:    attempted,
		Result:       "rejected",
		RejectReason: rejectReason,
	}, false, http.StatusUnauthorized, "missing or invalid bearer token"
}

func (s *Server) anonymousMCPAuth(r *http.Request) mcp.AuthContext {
	authContext := mcp.AuthContext{Kind: mcp.AuthKindAnonymous}
	s.logAuthDecision(r, authLog{
		Attempted: "none",
		Result:    "accepted",
		Kind:      string(authContext.Kind),
	})
	return authContext
}

func (s *Server) rejectMCPUnauthorized(w http.ResponseWriter, r *http.Request, logEntry authLog, status int, message string) {
	s.logAuthDecision(r, logEntry)
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", bearerResourceChallenge(s.oauthMetadataURL()))
	}
	if status == 0 {
		status = http.StatusUnauthorized
	}
	if message == "" {
		message = "missing or invalid bearer token"
	}
	writeError(w, status, message)
}

func isMCPDiscoveryRequest(body []byte) bool {
	var req struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}
	return req.Method == "initialize" || req.Method == "tools/list"
}

func mcpAuthChallengeResult(metadataURL, scope string) map[string]any {
	challenge := bearerChallenge("", "", metadataURL, scope)
	return map[string]any{
		"content": []map[string]string{
			{"type": "text", "text": "Authentication required."},
		},
		"_meta":   map[string]any{"mcp/www_authenticate": []string{challenge}},
		"isError": true,
	}
}

type authLog struct {
	Attempted    string
	Result       string
	RejectReason string
	Kind         string
	Email        string
	Subject      string
	Groups       []string
	Scopes       []string
	Issuer       string
	Audience     []string
	ExpiresAt    int64
	Claims       jwtClaims
}

func (s *Server) logAuthDecision(r *http.Request, entry authLog) {
	if r.URL.Path != "/mcp" {
		return
	}
	if entry.RejectReason == "" {
		entry.RejectReason = "none"
	}
	hasHeader := r.Header.Get("Authorization") != ""
	userAgent := r.UserAgent()
	if userAgent == "" {
		userAgent = "-"
	}
	remote := r.RemoteAddr
	if remote == "" {
		remote = "-"
	}
	base := "auth method=%s path=%s auth_mode=%s has_authorization_header=%t auth_kind=%s result=%s reject_reason=%s remote=%s user_agent=%q"
	if entry.Result == "accepted" {
		if s.auth.AuthDebug && entry.Attempted == "oauth_jwt" {
			log.Printf(base+" kind=%s email=%s scopes=%q iss=%s aud=%q sub=%s groups=%q exp=%d",
				r.Method, r.URL.Path, s.auth.Mode, hasHeader, entry.Attempted, entry.Result, entry.RejectReason, remote, userAgent,
				logDash(entry.Kind), logDash(entry.Email), strings.Join(entry.Scopes, " "), logDash(entry.Issuer), strings.Join(entry.Audience, " "), safeSubject(entry.Subject), strings.Join(entry.Groups, " "), entry.ExpiresAt)
			return
		}
		log.Printf(base+" kind=%s email=%s scopes=%q",
			r.Method, r.URL.Path, s.auth.Mode, hasHeader, entry.Attempted, entry.Result, entry.RejectReason, remote, userAgent,
			logDash(entry.Kind), logDash(entry.Email), strings.Join(entry.Scopes, " "))
		return
	}
	if s.auth.AuthDebug {
		claims := entry.Claims
		log.Printf(base+" iss=%s aud=%q sub=%s email=%s scopes=%q groups=%q exp=%d",
			r.Method, r.URL.Path, s.auth.Mode, hasHeader, entry.Attempted, entry.Result, entry.RejectReason, remote, userAgent,
			logDash(claims.Iss), strings.Join([]string(claims.Aud), " "), safeSubject(claims.Sub), logDash(claims.Email), strings.Join(brainScopes(claims), " "), strings.Join([]string(claims.Groups), " "), claims.Exp)
		return
	}
	log.Printf(base,
		r.Method, r.URL.Path, s.auth.Mode, hasHeader, entry.Attempted, entry.Result, entry.RejectReason, remote, userAgent)
}

func jwtRejectReason(err error) string {
	if jwtErr, ok := err.(jwtValidationError); ok && jwtErr.reason != "" {
		return jwtErr.reason
	}
	return "token_parse_error"
}

func jwtErrorClaims(err error) jwtClaims {
	if jwtErr, ok := err.(jwtValidationError); ok {
		return jwtErr.claims
	}
	return jwtClaims{}
}

func safeSubject(subject string) string {
	if subject == "" {
		return "-"
	}
	if len(subject) <= 8 {
		return subject
	}
	return subject[:8]
}

func logDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

type authContextKey struct{}

func withAuthContext(r *http.Request, auth mcp.AuthContext) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), authContextKey{}, auth))
}

func authContext(r *http.Request) mcp.AuthContext {
	auth, ok := r.Context().Value(authContextKey{}).(mcp.AuthContext)
	if ok {
		return auth
	}
	return mcp.AuthContext{
		Kind:   mcp.AuthKindBearerStatic,
		Scopes: []string{"brain:read", "brain:write", "brain:git", "brain:admin"},
	}
}

func (s *Server) oauthProtectedResource(w http.ResponseWriter, _ *http.Request) {
	body := map[string]any{
		"resource":                 s.auth.OAuthResource,
		"authorization_servers":    s.auth.AuthorizationServers,
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         s.auth.Scopes,
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) oauthMetadataURL() string {
	resource := strings.TrimRight(s.auth.OAuthResource, "/")
	if strings.HasSuffix(resource, "/mcp") {
		return strings.TrimSuffix(resource, "/mcp") + "/.well-known/oauth-protected-resource"
	}
	return resource + "/.well-known/oauth-protected-resource"
}

func bearerChallenge(errorCode, description, metadataURL, scope string) string {
	parts := []string{`Bearer realm="brain-mcp"`}
	if errorCode != "" {
		parts = append(parts, `error="`+headerQuote(errorCode)+`"`)
	}
	if description != "" {
		parts = append(parts, `error_description="`+headerQuote(description)+`"`)
	}
	if metadataURL != "" {
		parts = append(parts, `resource_metadata="`+headerQuote(metadataURL)+`"`)
	}
	if scope != "" {
		parts = append(parts, `scope="`+headerQuote(scope)+`"`)
	}
	return strings.Join(parts, ", ")
}

func bearerResourceChallenge(metadataURL string) string {
	if metadataURL == "" {
		return "Bearer"
	}
	return `Bearer resource_metadata="` + headerQuote(metadataURL) + `"`
}

func headerQuote(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) info(w http.ResponseWriter, _ *http.Request) {
	status, err := braingit.Status(s.vault.Root())
	summary := map[string]any{"available": err == nil}
	if err == nil {
		lines := strings.Split(strings.TrimSpace(status), "\n")
		entries := 0
		if strings.TrimSpace(status) != "" {
			entries = len(lines)
		}
		summary["clean"] = entries == 0
		summary["entries"] = entries
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"vault":          filepath.Base(s.vault.Root()),
		"writable_paths": s.vault.WritablePaths(),
		"readonly_paths": s.vault.ReadonlyPaths(),
		"git_status":     summary,
	})
}

func (s *Server) mcpPost(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil {
		log.Printf("http_mcp_error remote=%s bytes=unknown status=%d error=%q", r.RemoteAddr, http.StatusBadRequest, err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	log.Printf("http_mcp_request remote=%s bytes=%d", r.RemoteAddr, len(body))
	auth, logEntry, ok, status, message := s.authenticate(r)
	if !ok {
		if r.Header.Get("Authorization") == "" && isMCPDiscoveryRequest(body) {
			auth = s.anonymousMCPAuth(r)
		} else {
			s.rejectMCPUnauthorized(w, r, logEntry, status, message)
			log.Printf("http_mcp_error remote=%s bytes=%d status=%d duration_ms=%d error=%q", r.RemoteAddr, len(body), status, time.Since(start).Milliseconds(), message)
			return
		}
	} else {
		s.logAuthDecision(r, logEntry)
	}
	if scope := requiredScopeForMCPRequest(body); scope != "" && !mcp.HasScope(auth.Scopes, scope) {
		if auth.Kind == mcp.AuthKindAnonymous {
			w.Header().Set("WWW-Authenticate", bearerResourceChallenge(s.oauthMetadataURL()))
			writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")
			log.Printf("http_mcp_error remote=%s bytes=%d status=%d duration_ms=%d error=%q", r.RemoteAddr, len(body), http.StatusUnauthorized, time.Since(start).Milliseconds(), "missing authorization")
			return
		}
		w.Header().Set("WWW-Authenticate", bearerChallenge("insufficient_scope", "insufficient scope: "+scope, s.oauthMetadataURL(), scope))
		writeError(w, http.StatusForbidden, "insufficient scope: "+scope)
		log.Printf("http_mcp_error remote=%s bytes=%d status=%d duration_ms=%d error=%q", r.RemoteAddr, len(body), http.StatusForbidden, time.Since(start).Milliseconds(), "insufficient scope")
		return
	}
	resp, err := s.mcp.HandleBytesWithAuth(body, auth)
	if err != nil {
		log.Printf("http_mcp_error remote=%s bytes=%d status=%d duration_ms=%d error=%q", r.RemoteAddr, len(body), http.StatusInternalServerError, time.Since(start).Milliseconds(), err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
	log.Printf("http_mcp_response remote=%s request_bytes=%d response_bytes=%d status=%d duration_ms=%d", r.RemoteAddr, len(body), len(resp), http.StatusOK, time.Since(start).Milliseconds())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func requiredScopeForMCPRequest(body []byte) string {
	var req struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	if req.Method != "tools/call" {
		return ""
	}
	return mcp.RequiredScope(req.Params.Name)
}
