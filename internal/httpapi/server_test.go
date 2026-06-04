package httpapi

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iomz/brain-mcp/internal/brain"
)

func TestOAuthProtectedResourceMetadata(t *testing.T) {
	server := testHTTPServer(t, AuthConfig{
		Mode:                 AuthModeBearer,
		BearerToken:          "secret",
		OAuthResource:        "https://brain.sazanka.io/mcp",
		AuthorizationServers: []string{"https://auth.sazanka.io/application/o/brain-mcp/"},
		Scopes:               []string{"brain:read", "brain:write", "brain:git", "brain:admin"},
	})

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
		ScopesSupported      []string `json:"scopes_supported"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Resource != "https://brain.sazanka.io/mcp" {
		t.Fatalf("got resource %q", got.Resource)
	}
	if len(got.AuthorizationServers) != 1 || got.AuthorizationServers[0] != "https://auth.sazanka.io/application/o/brain-mcp/" {
		t.Fatalf("got authorization servers %#v", got.AuthorizationServers)
	}
	if strings.Join(got.ScopesSupported, " ") != "brain:read brain:write brain:git brain:admin" {
		t.Fatalf("got scopes %#v", got.ScopesSupported)
	}
}

func TestOAuthProtectedResourceMetadataUsesCompatIssuer(t *testing.T) {
	server := testHTTPServer(t, AuthConfig{
		Mode:                     AuthModeBearer,
		BearerToken:              "secret",
		OAuthResource:            "https://brain.sazanka.io/mcp",
		AuthorizationServers:     []string{"https://brain.sazanka.io"},
		OAuthDCR:                 true,
		OAuthAuthorizationServer: "https://brain.sazanka.io",
	})

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Resource != "https://brain.sazanka.io/mcp" {
		t.Fatalf("got resource %q", got.Resource)
	}
	if len(got.AuthorizationServers) != 1 || got.AuthorizationServers[0] != "https://brain.sazanka.io" {
		t.Fatalf("got authorization servers %#v", got.AuthorizationServers)
	}
}

func TestOAuthAuthorizationServerMetadataIncludesRegistrationEndpoint(t *testing.T) {
	server := testHTTPServer(t, AuthConfig{
		Mode:                     AuthModeBearer,
		BearerToken:              "secret",
		OAuthResource:            "https://brain.sazanka.io/mcp",
		OAuthAuthorizationServer: "https://brain.sazanka.io",
		OAuthDCR:                 true,
		Scopes:                   []string{"openid", "email", "profile", "brain:read"},
	})

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Issuer                            string   `json:"issuer"`
		RegistrationEndpoint              string   `json:"registration_endpoint"`
		TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
		CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Issuer != "https://brain.sazanka.io" {
		t.Fatalf("got issuer %q", got.Issuer)
	}
	if got.RegistrationEndpoint != "https://brain.sazanka.io/oauth/register" {
		t.Fatalf("got registration endpoint %q", got.RegistrationEndpoint)
	}
	if strings.Join(got.TokenEndpointAuthMethodsSupported, " ") != "none" {
		t.Fatalf("got token endpoint auth methods %#v", got.TokenEndpointAuthMethodsSupported)
	}
	if strings.Join(got.CodeChallengeMethodsSupported, " ") != "S256" {
		t.Fatalf("got code challenge methods %#v", got.CodeChallengeMethodsSupported)
	}
}

func TestDCRRegistrationSucceedsForChatGPTRedirectURI(t *testing.T) {
	server := testHTTPServer(t, AuthConfig{
		Mode:        AuthModeBearer,
		BearerToken: "secret",
		OAuthDCR:    true,
	})
	body := `{"client_name":"ChatGPT brain-mcp","redirect_uris":["https://chatgpt.com/connector/oauth/callback"],"grant_types":["authorization_code"],"response_types":["code"],"token_endpoint_auth_method":"none"}`

	req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var got registeredClient
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.ClientID, "brain-mcp-") {
		t.Fatalf("got client id %q", got.ClientID)
	}
	if strings.Join(got.RedirectURIs, " ") != "https://chatgpt.com/connector/oauth/callback" {
		t.Fatalf("got redirect uris %#v", got.RedirectURIs)
	}
	if strings.Join(got.GrantTypes, " ") != "authorization_code refresh_token" || strings.Join(got.ResponseTypes, " ") != "code" {
		t.Fatalf("got grant/response types %#v %#v", got.GrantTypes, got.ResponseTypes)
	}
	if got.TokenEndpointAuthMethod != "none" {
		t.Fatalf("got token endpoint auth method %q", got.TokenEndpointAuthMethod)
	}
}

func TestDCRRegistrationRejectsNonChatGPTRedirectURI(t *testing.T) {
	server := testHTTPServer(t, AuthConfig{
		Mode:        AuthModeBearer,
		BearerToken: "secret",
		OAuthDCR:    true,
	})
	body := `{"client_name":"bad","redirect_uris":["https://evil.example/callback"],"grant_types":["authorization_code"],"response_types":["code"],"token_endpoint_auth_method":"none"}`

	req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "redirect_uris must use https://chatgpt.com/connector/oauth/") {
		t.Fatalf("got body %s", rec.Body.String())
	}
}

func TestOAuthCodeFlowRequiresPKCES256(t *testing.T) {
	server := testHTTPServer(t, AuthConfig{
		Mode:               AuthModeOAuth,
		OAuthDCR:           true,
		OAuthApprovalToken: "approve",
		OAuthResource:      "https://brain.sazanka.io/mcp",
	})
	clientID := registerTestOAuthClient(t, server)

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", "https://chatgpt.com/connector/oauth/callback")
	q.Set("resource", "https://brain.sazanka.io/mcp")
	q.Set("code_challenge", "abc")
	q.Set("code_challenge_method", "plain")
	q.Set("approval_token", "approve")
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "code_challenge_method must be S256") {
		t.Fatalf("got body %s", rec.Body.String())
	}
}

func TestOAuthCodeFlowIssuesUsableAccessToken(t *testing.T) {
	server := testHTTPServer(t, AuthConfig{
		Mode:                     AuthModeOAuth,
		OAuthDCR:                 true,
		OAuthApprovalToken:       "approve",
		OAuthResource:            "https://brain.sazanka.io/mcp",
		OAuthAuthorizationServer: "https://brain.sazanka.io",
		Scopes:                   []string{"brain:read", "brain:write", "brain:git", "brain:admin"},
		OAuthEmail:               "iori.mizutani@gmail.com",
	})
	clientID := registerTestOAuthClient(t, server)
	verifier := "correct horse battery staple"
	code := authorizeTestOAuthClient(t, server, clientID, pkceChallenge(verifier))

	form := tokenExchangeForm(clientID, code, verifier)
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tokenResp); err != nil {
		t.Fatal(err)
	}
	if tokenResp.AccessToken == "" || tokenResp.TokenType != "Bearer" {
		t.Fatalf("bad token response %#v", tokenResp)
	}
	read := postMCPWithBearer(server, tokenResp.AccessToken, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"brain_read_note","arguments":{"path":"Knowledge/test.md"}}}`)
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), "hello") {
		t.Fatalf("read with issued token failed status=%d body=%s", read.Code, read.Body.String())
	}
}

func TestOAuthApprovalCookieAvoidsRepeatedTokenEntry(t *testing.T) {
	server := testHTTPServer(t, AuthConfig{
		Mode:               AuthModeOAuth,
		OAuthDCR:           true,
		OAuthApprovalToken: "approve",
		OAuthResource:      "https://brain.sazanka.io/mcp",
	})
	clientID := registerTestOAuthClient(t, server)
	verifier := "correct horse battery staple"
	q := authorizeQuery(clientID, pkceChallenge(verifier))
	q.Set("approval_token", "approve")
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize status=%d body=%s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("approval cookie not set")
	}

	q = authorizeQuery(clientID, pkceChallenge(verifier+"2"))
	req = httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
	req.AddCookie(cookies[0])
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize with cookie status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOAuthStatePersistsClientsAndSigningKey(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "oauth-state.json")
	auth := AuthConfig{
		Mode:                     AuthModeOAuth,
		OAuthDCR:                 true,
		OAuthApprovalToken:       "approve",
		OAuthResource:            "https://brain.sazanka.io/mcp",
		OAuthAuthorizationServer: "https://brain.sazanka.io",
		OAuthStateFile:           stateFile,
	}
	server := testHTTPServer(t, auth)
	clientID := registerTestOAuthClient(t, server)
	firstJWK := server.localOAuth.jwk()

	restarted := testHTTPServer(t, auth)
	if restarted.registeredClient(clientID).ClientID != clientID {
		t.Fatalf("registered client did not persist")
	}
	secondJWK := restarted.localOAuth.jwk()
	if firstJWK.N != secondJWK.N || firstJWK.E != secondJWK.E {
		t.Fatalf("signing key did not persist")
	}
}

func TestOAuthTokenRejectsReusedCode(t *testing.T) {
	server := testHTTPServer(t, AuthConfig{
		Mode:               AuthModeOAuth,
		OAuthDCR:           true,
		OAuthApprovalToken: "approve",
		OAuthResource:      "https://brain.sazanka.io/mcp",
	})
	clientID := registerTestOAuthClient(t, server)
	verifier := "correct horse battery staple"
	code := authorizeTestOAuthClient(t, server, clientID, pkceChallenge(verifier))

	exchangeTestCode(t, server, clientID, code, verifier, http.StatusOK)
	exchangeTestCode(t, server, clientID, code, verifier, http.StatusBadRequest)
}

func TestOAuthTokenRejectsExpiredCode(t *testing.T) {
	server := testHTTPServer(t, AuthConfig{
		Mode:               AuthModeOAuth,
		OAuthDCR:           true,
		OAuthApprovalToken: "approve",
		OAuthResource:      "https://brain.sazanka.io/mcp",
	})
	clientID := registerTestOAuthClient(t, server)
	verifier := "correct horse battery staple"
	codeValue := authorizeTestOAuthClient(t, server, clientID, pkceChallenge(verifier))
	server.dcrStore.mu.Lock()
	code := server.dcrStore.codes[codeValue]
	code.ExpiresAt = time.Now().Add(-time.Minute)
	server.dcrStore.codes[codeValue] = code
	server.dcrStore.mu.Unlock()

	exchangeTestCode(t, server, clientID, codeValue, verifier, http.StatusBadRequest)
}

func TestOAuthRefreshTokenRotates(t *testing.T) {
	server := testHTTPServer(t, AuthConfig{
		Mode:               AuthModeOAuth,
		OAuthDCR:           true,
		OAuthApprovalToken: "approve",
		OAuthResource:      "https://brain.sazanka.io/mcp",
	})
	clientID := registerTestOAuthClient(t, server)
	verifier := "correct horse battery staple"
	code := authorizeTestOAuthClient(t, server, clientID, pkceChallenge(verifier))
	first := exchangeCodeForToken(t, server, clientID, code, verifier)
	if first.RefreshToken == "" {
		t.Fatalf("missing refresh token")
	}

	second := refreshAccessToken(t, server, clientID, first.RefreshToken, http.StatusOK)
	if second.RefreshToken == "" || second.RefreshToken == first.RefreshToken {
		t.Fatalf("refresh token was not rotated")
	}
	refreshAccessToken(t, server, clientID, first.RefreshToken, http.StatusBadRequest)
}

func TestOAuthClientsAdminRequiresStaticBearer(t *testing.T) {
	server := testHTTPServer(t, AuthConfig{
		Mode:        AuthModeOAuth,
		BearerToken: "secret",
		OAuthDCR:    true,
	})
	clientID := registerTestOAuthClient(t, server)

	req := httptest.NewRequest(http.MethodGet, "/oauth/clients", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/oauth/clients", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), clientID) {
		t.Fatalf("clients response missing client id: %s", rec.Body.String())
	}
}

func TestOAuthAuthorizeRedirectsToAuthentikWhenEnabled(t *testing.T) {
	server := testHTTPServer(t, AuthConfig{
		Mode:                       AuthModeOAuth,
		OAuthDCR:                   true,
		OAuthResource:              "https://brain.sazanka.io/mcp",
		OAuthAuthorizationServer:   "https://brain.sazanka.io",
		OAuthAuthentikApproval:     true,
		OAuthAuthentikClientID:     "authentik-client",
		OAuthAuthentikAuthorizeURL: "https://auth.sazanka.io/application/o/brain-mcp/authorize/",
	})
	clientID := registerTestOAuthClient(t, server)
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+authorizeQuery(clientID, pkceChallenge("verifier")).Encode(), nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("got status %d, want 302: %s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, "https://auth.sazanka.io/application/o/brain-mcp/authorize/") {
		t.Fatalf("unexpected redirect: %s", location)
	}
}

func registerTestOAuthClient(t *testing.T, server *Server) string {
	t.Helper()
	body := `{"client_name":"ChatGPT brain-mcp","redirect_uris":["https://chatgpt.com/connector/oauth/callback"],"grant_types":["authorization_code"],"response_types":["code"],"token_endpoint_auth_method":"none"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got registeredClient
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got.ClientID
}

func authorizeTestOAuthClient(t *testing.T, server *Server, clientID, challenge string) string {
	t.Helper()
	q := authorizeQuery(clientID, challenge)
	q.Set("approval_token", "approve")
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize status=%d body=%s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	u, err := url.Parse(location)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("state") != "test-state" {
		t.Fatalf("state not echoed in %s", location)
	}
	code := u.Query().Get("code")
	if code == "" {
		t.Fatalf("location missing code: %s", location)
	}
	return code
}

func authorizeQuery(clientID, challenge string) url.Values {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", "https://chatgpt.com/connector/oauth/callback")
	q.Set("resource", "https://brain.sazanka.io/mcp")
	q.Set("scope", "brain:read")
	q.Set("state", "test-state")
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	return q
}

func exchangeTestCode(t *testing.T, server *Server, clientID, code, verifier string, want int) {
	t.Helper()
	rec := exchangeCode(t, server, clientID, code, verifier)
	if rec.Code != want {
		t.Fatalf("exchange status=%d want=%d body=%s", rec.Code, want, rec.Body.String())
	}
}

type testTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
}

func exchangeCodeForToken(t *testing.T, server *Server, clientID, code, verifier string) testTokenResponse {
	t.Helper()
	rec := exchangeCode(t, server, clientID, code, verifier)
	if rec.Code != http.StatusOK {
		t.Fatalf("exchange status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got testTokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func exchangeCode(t *testing.T, server *Server, clientID, code, verifier string) *httptest.ResponseRecorder {
	t.Helper()
	form := tokenExchangeForm(clientID, code, verifier)
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	return rec
}

func refreshAccessToken(t *testing.T, server *Server, clientID, refreshToken string, want int) testTokenResponse {
	t.Helper()
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", clientID)
	form.Set("refresh_token", refreshToken)
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != want {
		t.Fatalf("refresh status=%d want=%d body=%s", rec.Code, want, rec.Body.String())
	}
	var got testTokenResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
	}
	return got
}

func tokenExchangeForm(clientID, code, verifier string) url.Values {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", clientID)
	form.Set("code", code)
	form.Set("redirect_uri", "https://chatgpt.com/connector/oauth/callback")
	form.Set("resource", "https://brain.sazanka.io/mcp")
	form.Set("code_verifier", verifier)
	return form
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestUnauthenticatedMCPDiscoveryIsAllowed(t *testing.T) {
	server := testHTTPServer(t, AuthConfig{
		Mode:                 AuthModeBearer,
		BearerToken:          "secret",
		OAuthResource:        "https://brain.sazanka.io/mcp",
		AuthorizationServers: []string{"https://auth.sazanka.io/application/o/brain-mcp/"},
		Scopes:               []string{"brain:read", "brain:write", "brain:git", "brain:admin"},
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"serverInfo"`) {
		t.Fatalf("response missing initialize result: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`))
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"tools"`) {
		t.Fatalf("response missing tools list: %s", rec.Body.String())
	}
}

func TestUnauthenticatedMCPToolCallReturnsOAuthChallenge(t *testing.T) {
	server := testHTTPServer(t, AuthConfig{
		Mode:                 AuthModeBearer,
		BearerToken:          "secret",
		OAuthResource:        "https://brain.sazanka.io/mcp",
		AuthorizationServers: []string{"https://auth.sazanka.io/application/o/brain-mcp/"},
		Scopes:               []string{"openid", "email", "profile"},
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"brain_read_note","arguments":{"path":"Knowledge/test.md"}}}`))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401", rec.Code)
	}
	challenge := rec.Header().Get("WWW-Authenticate")
	if challenge != `Bearer resource_metadata="https://brain.sazanka.io/.well-known/oauth-protected-resource"` {
		t.Fatalf("got challenge %q", challenge)
	}
}

func TestBearerModeStillAllowsValidToken(t *testing.T) {
	server := testHTTPServer(t, AuthConfig{
		Mode:                 AuthModeBearer,
		BearerToken:          "secret",
		OAuthResource:        "https://brain.sazanka.io/mcp",
		AuthorizationServers: []string{"https://auth.sazanka.io/application/o/brain-mcp/"},
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"serverInfo"`) {
		t.Fatalf("response missing initialize result: %s", rec.Body.String())
	}
}

func TestAuthFailureLogOmitsRawTokenAndIncludesRejectReason(t *testing.T) {
	var logs bytes.Buffer
	oldOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() {
		log.SetOutput(oldOutput)
	})

	server := testHTTPServer(t, AuthConfig{
		Mode:        AuthModeBearer,
		BearerToken: "secret",
	})
	rawToken := "raw-token-that-must-not-be-logged"
	rec := postMCPWithBearer(server, rawToken, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401", rec.Code)
	}
	out := logs.String()
	if strings.Contains(out, rawToken) {
		t.Fatalf("log leaked raw token: %s", out)
	}
	if !strings.Contains(out, "reject_reason=static_bearer_mismatch") {
		t.Fatalf("log missing reject reason: %s", out)
	}
}

func TestMixedModeStillAllowsStaticBearer(t *testing.T) {
	server := testHTTPServer(t, AuthConfig{
		Mode:        AuthModeMixed,
		BearerToken: "secret",
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestOAuthJWTAcceptedWithValidIssuerAudienceEmail(t *testing.T) {
	oauth := testOAuth(t)
	server := testHTTPServer(t, oauth.auth())
	token := oauth.token(t, map[string]any{
		"iss":   oauth.issuer,
		"sub":   "ak-user",
		"aud":   "https://brain.sazanka.io/mcp",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"email": "iori.mizutani@gmail.com",
		"scope": "openid profile",
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"brain_read_note","arguments":{"path":"Knowledge/test.md"}}}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "hello") || !strings.Contains(rec.Body.String(), "Knowledge/test.md") {
		t.Fatalf("read response missing note content: %s", rec.Body.String())
	}
}

func TestOAuthJWTRejectsWrongIssuer(t *testing.T) {
	oauth := testOAuth(t)
	server := testHTTPServer(t, oauth.auth())
	token := oauth.token(t, map[string]any{
		"iss":   "https://wrong.example/",
		"sub":   "ak-user",
		"aud":   "https://brain.sazanka.io/mcp",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"email": "iori.mizutani@gmail.com",
	})

	rec := postMCPWithBearer(server, token, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

func TestOAuthFailureLogIncludesRejectReasonWithoutRawToken(t *testing.T) {
	var logs bytes.Buffer
	oldOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() {
		log.SetOutput(oldOutput)
	})

	oauth := testOAuth(t)
	server := testHTTPServer(t, oauth.auth())
	token := oauth.token(t, map[string]any{
		"iss":   "https://wrong.example/",
		"sub":   "ak-user",
		"aud":   "https://brain.sazanka.io/mcp",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"email": "iori.mizutani@gmail.com",
	})

	rec := postMCPWithBearer(server, token, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401: %s", rec.Code, rec.Body.String())
	}
	out := logs.String()
	if strings.Contains(out, token) {
		t.Fatalf("log leaked raw token: %s", out)
	}
	if !strings.Contains(out, "auth_kind=oauth_jwt") || !strings.Contains(out, "reject_reason=issuer_mismatch") {
		t.Fatalf("log missing oauth reject reason: %s", out)
	}
}

func TestOAuthJWTRejectsWrongAudience(t *testing.T) {
	oauth := testOAuth(t)
	server := testHTTPServer(t, oauth.auth())
	token := oauth.token(t, map[string]any{
		"iss":   oauth.issuer,
		"sub":   "ak-user",
		"aud":   "https://other.example/mcp",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"email": "iori.mizutani@gmail.com",
	})

	rec := postMCPWithBearer(server, token, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

func TestOAuthJWTAcceptsConfiguredClientIDAudience(t *testing.T) {
	oauth := testOAuth(t)
	auth := oauth.auth()
	auth.OAuthClientID = "chatgpt-client"
	auth.OAuthAcceptedAudiences = []string{"https://brain.sazanka.io/mcp"}
	server := testHTTPServer(t, auth)
	token := oauth.token(t, map[string]any{
		"iss":   oauth.issuer,
		"sub":   "ak-user",
		"aud":   "chatgpt-client",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"email": "iori.mizutani@gmail.com",
	})

	rec := postMCPWithBearer(server, token, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestOAuthJWTAcceptsResourceClaim(t *testing.T) {
	oauth := testOAuth(t)
	auth := oauth.auth()
	auth.OAuthAcceptedAudiences = []string{"https://brain.sazanka.io/mcp"}
	server := testHTTPServer(t, auth)
	token := oauth.token(t, map[string]any{
		"iss":      oauth.issuer,
		"sub":      "ak-user",
		"aud":      "other-client",
		"resource": "https://brain.sazanka.io/mcp",
		"exp":      time.Now().Add(time.Hour).Unix(),
		"email":    "iori.mizutani@gmail.com",
	})

	rec := postMCPWithBearer(server, token, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestOAuthJWTRejectsExpiredToken(t *testing.T) {
	oauth := testOAuth(t)
	server := testHTTPServer(t, oauth.auth())
	token := oauth.token(t, map[string]any{
		"iss":   oauth.issuer,
		"sub":   "ak-user",
		"aud":   "https://brain.sazanka.io/mcp",
		"exp":   time.Now().Add(-time.Minute).Unix(),
		"email": "iori.mizutani@gmail.com",
	})

	rec := postMCPWithBearer(server, token, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

func TestOAuthJWTRejectsNonAllowlistedEmail(t *testing.T) {
	oauth := testOAuth(t)
	server := testHTTPServer(t, oauth.auth())
	token := oauth.token(t, map[string]any{
		"iss":   oauth.issuer,
		"sub":   "ak-user",
		"aud":   "https://brain.sazanka.io/mcp",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"email": "other@example.com",
	})

	rec := postMCPWithBearer(server, token, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

func TestOAuthReadScopeCanReadButCannotWrite(t *testing.T) {
	oauth := testOAuth(t)
	server := testHTTPServer(t, oauth.auth())
	token := oauth.token(t, map[string]any{
		"iss":   oauth.issuer,
		"sub":   "ak-user",
		"aud":   "chatgpt-client",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"email": "iori.mizutani@gmail.com",
		"scope": "brain:read",
	})

	read := postMCPWithBearer(server, token, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"brain_read_note","arguments":{"path":"Knowledge/test.md"}}}`)
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), "hello") || !strings.Contains(read.Body.String(), "Knowledge/test.md") {
		t.Fatalf("read failed status=%d body=%s", read.Code, read.Body.String())
	}

	write := postMCPWithBearer(server, token, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"brain_write_note","arguments":{"path":"Knowledge/test.md","content":"changed"}}}`)
	if write.Code != http.StatusForbidden {
		t.Fatalf("got status %d, want 403: %s", write.Code, write.Body.String())
	}
	if !strings.Contains(write.Body.String(), "insufficient scope: brain:write") {
		t.Fatalf("write response missing insufficient scope: %s", write.Body.String())
	}
	challenge := write.Header().Get("WWW-Authenticate")
	if !strings.Contains(challenge, `error="insufficient_scope"`) || !strings.Contains(challenge, `scope="brain:write"`) {
		t.Fatalf("write response missing insufficient scope challenge: %q", challenge)
	}
}

func testHTTPServer(t *testing.T, auth AuthConfig) *Server {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Knowledge", "test.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vault, err := brain.NewVaultWithPolicy(root, brain.Policy{
		WritablePaths: brain.DefaultWritablePaths(),
		ReadonlyPaths: brain.DefaultReadonlyPaths(),
		RequireGit:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewServerWithAuth(vault, auth)
}

type testOAuthFixture struct {
	key     *rsa.PrivateKey
	jwksURL string
	issuer  string
}

func testOAuth(t *testing.T) testOAuthFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwkKey := publicJWK(&key.PublicKey)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, jwks{Keys: []jwk{jwkKey}})
	}))
	t.Cleanup(server.Close)
	return testOAuthFixture{
		key:     key,
		jwksURL: server.URL,
		issuer:  "https://auth.sazanka.io/application/o/brain-mcp/",
	}
}

func (f testOAuthFixture) auth() AuthConfig {
	return AuthConfig{
		Mode:                   AuthModeOAuth,
		OAuthIssuer:            f.issuer,
		OAuthJWKSURL:           f.jwksURL,
		OAuthResource:          "https://brain.sazanka.io/mcp",
		OAuthAcceptedAudiences: []string{"https://brain.sazanka.io/mcp", "chatgpt-client"},
		OAuthDefaultScopes:     []string{"brain:read"},
		AuthorizationServers:   []string{f.issuer},
		AllowedEmails:          []string{"iori.mizutani@gmail.com"},
	}
}

func (f testOAuthFixture) token(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": "test-key"}
	head := encodeJWTJSON(t, header)
	body := encodeJWTJSON(t, claims)
	signed := head + "." + body
	sum := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func encodeJWTJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func publicJWK(pub *rsa.PublicKey) jwk {
	e := big.NewInt(int64(pub.E)).Bytes()
	return jwk{
		Kty: "RSA",
		Kid: "test-key",
		Alg: "RS256",
		Use: "sig",
		N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(e),
	}
}

func postMCPWithBearer(server *Server, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	return rec
}
