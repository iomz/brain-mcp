package httpapi

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"html"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/iomz/brain-mcp/internal/mcp"
)

type dcrStore struct {
	mu            sync.Mutex
	path          string
	clients       map[string]registeredClient
	codes         map[string]authorizationCode
	refreshTokens map[string]refreshToken
	sessions      map[string]time.Time
	authentik     map[string]url.Values
}

type registeredClient struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

type dcrRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

type oauthState struct {
	store *dcrStore
	key   *rsa.PrivateKey
}

type persistedOAuthState struct {
	PrivateKeyPEM string             `json:"private_key_pem"`
	Clients       []registeredClient `json:"clients"`
	RefreshTokens []refreshToken     `json:"refresh_tokens"`
}

func newOAuthState(auth AuthConfig) (oauthState, error) {
	state := persistedOAuthState{}
	if auth.OAuthStateFile != "" {
		data, err := os.ReadFile(auth.OAuthStateFile)
		if err == nil {
			if err := json.Unmarshal(data, &state); err != nil {
				return oauthState{}, err
			}
		} else if !os.IsNotExist(err) {
			return oauthState{}, err
		}
	}
	key, err := privateKeyFromPEM(state.PrivateKeyPEM)
	if err != nil {
		return oauthState{}, err
	}
	if key == nil {
		key, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return oauthState{}, fmt.Errorf("generate oauth signing key: %w", err)
		}
		state.PrivateKeyPEM = privateKeyPEM(key)
	}
	store := &dcrStore{
		path:          auth.OAuthStateFile,
		clients:       map[string]registeredClient{},
		codes:         map[string]authorizationCode{},
		refreshTokens: map[string]refreshToken{},
		sessions:      map[string]time.Time{},
		authentik:     map[string]url.Values{},
	}
	for _, client := range state.Clients {
		if client.ClientID != "" {
			store.clients[client.ClientID] = client
		}
	}
	for _, token := range state.RefreshTokens {
		if token.Token != "" && time.Now().Before(token.ExpiresAt) {
			store.refreshTokens[token.Token] = token
		}
	}
	if auth.OAuthStateFile != "" && len(state.Clients) == 0 {
		if err := store.saveLocked(key); err != nil {
			return oauthState{}, err
		}
	}
	return oauthState{store: store, key: key}, nil
}

func (d *dcrStore) saveLocked(key *rsa.PrivateKey) error {
	if d.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(d.path), 0o700); err != nil {
		return err
	}
	state := persistedOAuthState{
		PrivateKeyPEM: privateKeyPEM(key),
	}
	for _, client := range d.clients {
		state.Clients = append(state.Clients, client)
	}
	for _, token := range d.refreshTokens {
		if time.Now().Before(token.ExpiresAt) {
			state.RefreshTokens = append(state.RefreshTokens, token)
		}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(d.path, data, 0o600)
}

func privateKeyPEM(key *rsa.PrivateKey) string {
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	return string(pem.EncodeToMemory(block))
}

func privateKeyFromPEM(raw string) (*rsa.PrivateKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	block, _ := pem.Decode([]byte(raw))
	if block == nil {
		return nil, fmt.Errorf("oauth state private key is invalid PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return key, nil
}

func (d *dcrStore) createSession() (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	d.mu.Lock()
	d.sessions[token] = time.Now().Add(30 * 24 * time.Hour)
	d.mu.Unlock()
	return token, nil
}

func (d *dcrStore) validSession(token string) bool {
	if token == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	expiresAt, ok := d.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(expiresAt) {
		delete(d.sessions, token)
		return false
	}
	return true
}

type authorizationCode struct {
	ClientID      string
	RedirectURI   string
	Resource      string
	Scopes        []string
	CodeChallenge string
	ExpiresAt     time.Time
}

type refreshToken struct {
	Token     string    `json:"token"`
	ClientID  string    `json:"client_id"`
	Scopes    []string  `json:"scopes"`
	ExpiresAt time.Time `json:"expires_at"`
}

type localOAuthServer struct {
	issuer    string
	resource  string
	subject   string
	email     string
	scopes    []string
	key       *rsa.PrivateKey
	keyID     string
	expiresIn time.Duration
}

func newLocalOAuthServer(auth AuthConfig, key *rsa.PrivateKey) *localOAuthServer {
	subject := strings.TrimSpace(auth.OAuthSubject)
	if subject == "" {
		subject = "brain-mcp-user"
	}
	email := strings.TrimSpace(auth.OAuthEmail)
	if email == "" && len(auth.AllowedEmails) > 0 {
		email = auth.AllowedEmails[0]
	}
	return &localOAuthServer{
		issuer:    strings.TrimRight(auth.OAuthAuthorizationServer, "/"),
		resource:  auth.OAuthResource,
		subject:   subject,
		email:     email,
		scopes:    auth.Scopes,
		key:       key,
		keyID:     "brain-mcp-local",
		expiresIn: time.Hour,
	}
}

func (s *Server) oauthAuthorizationServer(w http.ResponseWriter, _ *http.Request) {
	if !s.auth.OAuthDCR {
		writeError(w, http.StatusNotFound, "oauth dcr is disabled")
		return
	}
	issuer := strings.TrimRight(s.auth.OAuthAuthorizationServer, "/")
	body := map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/oauth/authorize",
		"token_endpoint":                        issuer + "/oauth/token",
		"registration_endpoint":                 issuer + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"code_challenge_methods_supported":      []string{"S256"},
		"scopes_supported":                      s.auth.Scopes,
		"jwks_uri":                              issuer + "/.well-known/jwks.json",
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) oauthJWKS(w http.ResponseWriter, _ *http.Request) {
	if !s.auth.OAuthDCR {
		writeError(w, http.StatusNotFound, "oauth dcr is disabled")
		return
	}
	writeJSON(w, http.StatusOK, jwks{Keys: []jwk{s.localOAuth.jwk()}})
}

func (s *Server) oauthRegister(w http.ResponseWriter, r *http.Request) {
	if !s.auth.OAuthDCR {
		writeError(w, http.StatusNotFound, "oauth dcr is disabled")
		return
	}
	var req dcrRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid client metadata")
		return
	}
	if errMsg := validateDCRRequest(req); errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	clientID, err := randomClientID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate client_id")
		return
	}
	client := registeredClient{
		ClientID:                clientID,
		ClientName:              req.ClientName,
		RedirectURIs:            append([]string{}, req.RedirectURIs...),
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "none",
	}
	s.dcrStore.mu.Lock()
	s.dcrStore.clients[client.ClientID] = client
	err = s.dcrStore.saveLocked(s.localOAuth.key)
	s.dcrStore.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not persist registered client")
		return
	}
	writeJSON(w, http.StatusCreated, client)
}

func (s *Server) oauthClients(w http.ResponseWriter, r *http.Request) {
	if s.auth.BearerToken == "" {
		writeError(w, http.StatusNotFound, "admin endpoint is disabled")
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+s.auth.BearerToken {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")
		return
	}
	type clientSummary struct {
		ClientID                string   `json:"client_id"`
		ClientName              string   `json:"client_name,omitempty"`
		RedirectURIs            []string `json:"redirect_uris"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	s.dcrStore.mu.Lock()
	clients := make([]clientSummary, 0, len(s.dcrStore.clients))
	for _, client := range s.dcrStore.clients {
		clients = append(clients, clientSummary{
			ClientID:                client.ClientID,
			ClientName:              client.ClientName,
			RedirectURIs:            append([]string{}, client.RedirectURIs...),
			GrantTypes:              append([]string{}, client.GrantTypes...),
			ResponseTypes:           append([]string{}, client.ResponseTypes...),
			TokenEndpointAuthMethod: client.TokenEndpointAuthMethod,
		})
	}
	refreshTokenCount := len(s.dcrStore.refreshTokens)
	codeCount := len(s.dcrStore.codes)
	sessionCount := len(s.dcrStore.sessions)
	s.dcrStore.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"clients":             clients,
		"refresh_token_count": refreshTokenCount,
		"code_count":          codeCount,
		"session_count":       sessionCount,
	})
}

func (s *Server) oauthAuthorize(w http.ResponseWriter, r *http.Request) {
	if !s.auth.OAuthDCR {
		writeError(w, http.StatusNotFound, "oauth dcr is disabled")
		return
	}
	values := r.URL.Query()
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid authorization request")
			return
		}
		values = r.PostForm
	}
	if errMsg := s.validateAuthorizeRequest(values); errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	if !s.approvalSatisfied(w, r, values) {
		return
	}
	code, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not generate authorization code")
		return
	}
	scopes := requestedScopes(values.Get("scope"), s.auth.Scopes)
	s.dcrStore.mu.Lock()
	s.dcrStore.codes[code] = authorizationCode{
		ClientID:      values.Get("client_id"),
		RedirectURI:   values.Get("redirect_uri"),
		Resource:      values.Get("resource"),
		Scopes:        scopes,
		CodeChallenge: values.Get("code_challenge"),
		ExpiresAt:     time.Now().Add(5 * time.Minute),
	}
	s.dcrStore.mu.Unlock()
	redirectURL, _ := url.Parse(values.Get("redirect_uri"))
	q := redirectURL.Query()
	q.Set("code", code)
	if state := values.Get("state"); state != "" {
		q.Set("state", state)
	}
	redirectURL.RawQuery = q.Encode()
	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

func (s *Server) approvalSatisfied(w http.ResponseWriter, r *http.Request, values url.Values) bool {
	if cookie, err := r.Cookie("brain_mcp_oauth_approval"); err == nil && s.dcrStore.validSession(cookie.Value) {
		return true
	}
	if s.auth.OAuthApprovalToken == "" {
		if s.auth.OAuthAuthentikApproval {
			s.redirectToAuthentikApproval(w, r, values)
			return false
		}
		writeError(w, http.StatusServiceUnavailable, "BRAIN_MCP_OAUTH_APPROVAL_TOKEN is required")
		return false
	}
	approvalToken := values.Get("approval_token")
	if approvalToken == "" || subtle.ConstantTimeCompare([]byte(approvalToken), []byte(s.auth.OAuthApprovalToken)) != 1 {
		if approvalToken == "" && s.auth.OAuthAuthentikApproval {
			s.redirectToAuthentikApproval(w, r, values)
			return false
		}
		writeApprovalForm(w, values, approvalToken != "")
		return false
	}
	session, err := s.dcrStore.createSession()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create approval session")
		return false
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "brain_mcp_oauth_approval",
		Value:    session,
		Path:     "/oauth/",
		MaxAge:   30 * 24 * 60 * 60,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return true
}

func (s *Server) redirectToAuthentikApproval(w http.ResponseWriter, r *http.Request, values url.Values) {
	state, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create authentik state")
		return
	}
	s.dcrStore.mu.Lock()
	s.dcrStore.authentik[state] = cloneValues(values)
	s.dcrStore.mu.Unlock()
	authURL := s.authentikAuthorizeURL()
	if authURL == "" {
		writeError(w, http.StatusServiceUnavailable, "authentik authorization URL is not configured")
		return
	}
	clientID := s.auth.OAuthAuthentikClientID
	if clientID == "" {
		clientID = s.auth.OAuthClientID
	}
	if clientID == "" {
		writeError(w, http.StatusServiceUnavailable, "authentik client_id is not configured")
		return
	}
	redirectURI := s.authentikRedirectURI()
	u, err := url.Parse(authURL)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "authentik authorization URL is invalid")
		return
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", "openid email profile")
	q.Set("state", state)
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (s *Server) oauthAuthentikCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" {
		writeError(w, http.StatusBadRequest, "missing authentik callback state or code")
		return
	}
	s.dcrStore.mu.Lock()
	original, ok := s.dcrStore.authentik[state]
	delete(s.dcrStore.authentik, state)
	s.dcrStore.mu.Unlock()
	if !ok {
		writeError(w, http.StatusBadRequest, "authentik state is invalid or expired")
		return
	}
	authContext, err := s.exchangeAuthentikCode(code)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentik approval failed: "+err.Error())
		return
	}
	session, err := s.dcrStore.createSession()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create approval session")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "brain_mcp_oauth_approval",
		Value:    session,
		Path:     "/oauth/",
		MaxAge:   30 * 24 * 60 * 60,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	log.Printf("oauth_authentik_approval result=accepted email=%s subject=%s", logDash(authContext.Email), safeSubject(authContext.Subject))
	http.Redirect(w, r, "/oauth/authorize?"+original.Encode(), http.StatusFound)
}

func (s *Server) exchangeAuthentikCode(code string) (mcp.AuthContext, error) {
	tokenURL := s.authentikTokenURL()
	if tokenURL == "" {
		return mcp.AuthContext{}, fmt.Errorf("token URL is not configured")
	}
	clientID := s.auth.OAuthAuthentikClientID
	if clientID == "" {
		clientID = s.auth.OAuthClientID
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", clientID)
	form.Set("code", code)
	form.Set("redirect_uri", s.authentikRedirectURI())
	if s.auth.OAuthAuthentikClientSecret != "" {
		form.Set("client_secret", s.auth.OAuthAuthentikClientSecret)
	}
	resp, err := http.DefaultClient.PostForm(tokenURL, form)
	if err != nil {
		return mcp.AuthContext{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return mcp.AuthContext{}, fmt.Errorf("token exchange failed: %d", resp.StatusCode)
	}
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return mcp.AuthContext{}, err
	}
	verifyToken := tokenResp.IDToken
	if verifyToken == "" {
		verifyToken = tokenResp.AccessToken
	}
	if verifyToken == "" {
		return mcp.AuthContext{}, fmt.Errorf("token response missing id_token/access_token")
	}
	return s.jwtVerifier.Verify(verifyToken)
}

func (s *Server) authentikAuthorizeURL() string {
	if s.auth.OAuthAuthentikAuthorizeURL != "" {
		return s.auth.OAuthAuthentikAuthorizeURL
	}
	if s.auth.OAuthIssuer == "" {
		return ""
	}
	return strings.TrimRight(s.auth.OAuthIssuer, "/") + "/authorize/"
}

func (s *Server) authentikTokenURL() string {
	if s.auth.OAuthAuthentikTokenURL != "" {
		return s.auth.OAuthAuthentikTokenURL
	}
	if s.auth.OAuthIssuer == "" {
		return ""
	}
	return strings.TrimRight(s.auth.OAuthIssuer, "/") + "/token/"
}

func (s *Server) authentikRedirectURI() string {
	if s.auth.OAuthAuthentikRedirectURI != "" {
		return s.auth.OAuthAuthentikRedirectURI
	}
	return strings.TrimRight(s.auth.OAuthAuthorizationServer, "/") + "/oauth/authentik/callback"
}

func cloneValues(values url.Values) url.Values {
	out := url.Values{}
	for key, value := range values {
		out[key] = append([]string{}, value...)
	}
	return out
}

func (s *Server) oauthToken(w http.ResponseWriter, r *http.Request) {
	if !s.auth.OAuthDCR {
		writeError(w, http.StatusNotFound, "oauth dcr is disabled")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid token request")
		return
	}
	if r.Form.Get("grant_type") == "refresh_token" {
		s.oauthRefreshToken(w, r)
		return
	}
	if r.Form.Get("grant_type") != "authorization_code" {
		writeOAuthError(w, "unsupported_grant_type", "only authorization_code and refresh_token are supported")
		return
	}
	codeValue := r.Form.Get("code")
	s.dcrStore.mu.Lock()
	code, ok := s.dcrStore.codes[codeValue]
	delete(s.dcrStore.codes, codeValue)
	s.dcrStore.mu.Unlock()
	if !ok {
		writeOAuthError(w, "invalid_grant", "authorization code is invalid or already used")
		return
	}
	if time.Now().After(code.ExpiresAt) {
		writeOAuthError(w, "invalid_grant", "authorization code is expired")
		return
	}
	if r.Form.Get("client_id") != code.ClientID {
		writeOAuthError(w, "invalid_client", "client_id does not match authorization code")
		return
	}
	if r.Form.Get("redirect_uri") != code.RedirectURI {
		writeOAuthError(w, "invalid_grant", "redirect_uri does not match authorization code")
		return
	}
	if resource := r.Form.Get("resource"); resource != "" && resource != code.Resource {
		writeOAuthError(w, "invalid_target", "resource does not match authorization code")
		return
	}
	if !verifyPKCES256(r.Form.Get("code_verifier"), code.CodeChallenge) {
		writeOAuthError(w, "invalid_grant", "code_verifier does not match code_challenge")
		return
	}
	s.writeTokenResponse(w, code.ClientID, code.Scopes)
}

func (s *Server) oauthRefreshToken(w http.ResponseWriter, r *http.Request) {
	raw := r.Form.Get("refresh_token")
	clientID := r.Form.Get("client_id")
	s.dcrStore.mu.Lock()
	refresh, ok := s.dcrStore.refreshTokens[raw]
	delete(s.dcrStore.refreshTokens, raw)
	if ok && (clientID == "" || clientID == refresh.ClientID) && time.Now().Before(refresh.ExpiresAt) {
		_ = s.dcrStore.saveLocked(s.localOAuth.key)
	}
	s.dcrStore.mu.Unlock()
	if !ok {
		writeOAuthError(w, "invalid_grant", "refresh token is invalid or already used")
		return
	}
	if clientID != "" && clientID != refresh.ClientID {
		writeOAuthError(w, "invalid_client", "client_id does not match refresh token")
		return
	}
	if time.Now().After(refresh.ExpiresAt) {
		writeOAuthError(w, "invalid_grant", "refresh token is expired")
		return
	}
	s.writeTokenResponse(w, refresh.ClientID, refresh.Scopes)
}

func (s *Server) writeTokenResponse(w http.ResponseWriter, clientID string, scopes []string) {
	token, err := s.localOAuth.accessToken(clientID, scopes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue access token")
		return
	}
	refreshValue, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue refresh token")
		return
	}
	s.dcrStore.mu.Lock()
	s.dcrStore.refreshTokens[refreshValue] = refreshToken{
		Token:     refreshValue,
		ClientID:  clientID,
		Scopes:    append([]string{}, scopes...),
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	}
	err = s.dcrStore.saveLocked(s.localOAuth.key)
	s.dcrStore.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not persist refresh token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  token,
		"token_type":    "Bearer",
		"expires_in":    int(s.localOAuth.expiresIn.Seconds()),
		"refresh_token": refreshValue,
		"scope":         strings.Join(scopes, " "),
	})
}

func validateDCRRequest(req dcrRequest) string {
	if len(req.RedirectURIs) == 0 {
		return "redirect_uris is required"
	}
	for _, raw := range req.RedirectURIs {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return "redirect_uris must be absolute HTTPS URLs"
		}
		if !strings.HasPrefix(raw, "https://chatgpt.com/connector/oauth/") {
			return "redirect_uris must use https://chatgpt.com/connector/oauth/"
		}
	}
	if !contains(req.GrantTypes, "authorization_code") {
		return "grant_types must include authorization_code"
	}
	if !contains(req.ResponseTypes, "code") {
		return "response_types must include code"
	}
	method := req.TokenEndpointAuthMethod
	if method == "" {
		method = "none"
	}
	if method != "none" {
		return "token_endpoint_auth_method must be none"
	}
	return ""
}

func (s *Server) validateAuthorizeRequest(values url.Values) string {
	if values.Get("response_type") != "code" {
		return "response_type must be code"
	}
	client := s.registeredClient(values.Get("client_id"))
	if client.ClientID == "" {
		return "client_id is not registered"
	}
	if !contains(client.RedirectURIs, values.Get("redirect_uri")) {
		return "redirect_uri is not registered"
	}
	if values.Get("resource") != s.auth.OAuthResource {
		return "resource must equal " + s.auth.OAuthResource
	}
	if values.Get("code_challenge_method") != "S256" {
		return "code_challenge_method must be S256"
	}
	if values.Get("code_challenge") == "" {
		return "code_challenge is required"
	}
	for _, scope := range strings.Fields(values.Get("scope")) {
		if !contains(s.auth.Scopes, scope) {
			return "unsupported scope: " + scope
		}
	}
	return ""
}

func (s *Server) registeredClient(clientID string) registeredClient {
	s.dcrStore.mu.Lock()
	defer s.dcrStore.mu.Unlock()
	return s.dcrStore.clients[clientID]
}

func requestedScopes(raw string, supported []string) []string {
	if strings.TrimSpace(raw) == "" {
		if contains(supported, "brain:read") {
			return []string{"brain:read"}
		}
		return append([]string{}, supported...)
	}
	return strings.Fields(raw)
}

func verifyPKCES256(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
}

func randomClientID() (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	return "brain-mcp-" + token, nil
}

func randomToken() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func authorizationServerFromResource(resource string) string {
	u, err := url.Parse(resource)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "https://brain.sazanka.io"
	}
	return u.Scheme + "://" + u.Host
}

func (l *localOAuthServer) accessToken(clientID string, scopes []string) (string, error) {
	now := time.Now()
	claims := map[string]any{
		"iss":       l.issuer,
		"sub":       l.subject,
		"email":     l.email,
		"aud":       l.resource,
		"resource":  l.resource,
		"scope":     strings.Join(scopes, " "),
		"iat":       now.Unix(),
		"exp":       now.Add(l.expiresIn).Unix(),
		"client_id": clientID,
	}
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": l.keyID}
	head, err := encodeJWT(header)
	if err != nil {
		return "", err
	}
	body, err := encodeJWT(claims)
	if err != nil {
		return "", err
	}
	signed := head + "." + body
	sum := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, l.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (l *localOAuthServer) Verify(token string) (mcp.AuthContext, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return mcp.AuthContext{}, errInvalidToken
	}
	var header jwtHeader
	if err := decodeJWTPart(parts[0], &header); err != nil {
		return mcp.AuthContext{}, err
	}
	if header.Alg != "RS256" || header.Kid != l.keyID {
		return mcp.AuthContext{}, errInvalidToken
	}
	var claims jwtClaims
	if err := decodeJWTPart(parts[1], &claims); err != nil {
		return mcp.AuthContext{}, err
	}
	if claims.Iss != l.issuer {
		return mcp.AuthContext{}, errInvalidToken
	}
	now := time.Now().Unix()
	if claims.Exp <= now {
		return mcp.AuthContext{}, errInvalidToken
	}
	tokenAudiences := append([]string(claims.Aud), []string(claims.Resource)...)
	if !contains(tokenAudiences, l.resource) {
		return mcp.AuthContext{}, errInvalidToken
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return mcp.AuthContext{}, err
	}
	if err := verifyRS256([]jwk{l.jwk()}, header.Kid, []byte(parts[0]+"."+parts[1]), signature); err != nil {
		return mcp.AuthContext{}, err
	}
	scopes := brainScopes(claims)
	if len(scopes) == 0 {
		scopes = []string{"brain:read"}
	}
	return mcp.AuthContext{
		Kind:      mcp.AuthKindOAuth,
		Subject:   claims.Sub,
		Email:     claims.Email,
		Scopes:    scopes,
		Issuer:    claims.Iss,
		Audience:  []string(claims.Aud),
		ExpiresAt: claims.Exp,
	}, nil
}

func (l *localOAuthServer) jwk() jwk {
	pub := &l.key.PublicKey
	return jwk{
		Kty: "RSA",
		Kid: l.keyID,
		Alg: "RS256",
		Use: "sig",
		N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

func encodeJWT(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func writeApprovalForm(w http.ResponseWriter, values url.Values, invalid bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	errorHTML := ""
	if invalid {
		errorHTML = `<p class="error">Approval token did not match.</p>`
	}
	clientName := html.EscapeString(values.Get("client_id"))
	scope := html.EscapeString(values.Get("scope"))
	if scope == "" {
		scope = "brain:read"
	}
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Authorize brain-mcp</title>
<style>
:root { color-scheme: light dark; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #f6f7f9; color: #17181c; }
main { width: min(420px, calc(100vw - 32px)); background: #fff; border: 1px solid #d7dbe2; border-radius: 8px; box-shadow: 0 16px 40px rgba(20, 23, 31, .10); padding: 28px; }
h1 { font-size: 22px; margin: 0 0 8px; letter-spacing: 0; }
p { line-height: 1.45; margin: 0 0 18px; color: #4d5563; }
dl { display: grid; grid-template-columns: 88px 1fr; gap: 8px 12px; margin: 18px 0; font-size: 14px; }
dt { color: #687180; }
dd { margin: 0; overflow-wrap: anywhere; }
label { display: block; font-size: 13px; font-weight: 600; margin: 18px 0 8px; }
input[type=password] { box-sizing: border-box; width: 100%%; height: 42px; border: 1px solid #b8c0cc; border-radius: 6px; padding: 0 12px; font-size: 15px; }
button { width: 100%%; height: 42px; border: 0; border-radius: 6px; background: #1769e0; color: white; font-weight: 700; font-size: 15px; cursor: pointer; }
.error { color: #b42318; background: #fff1f0; border: 1px solid #ffcdc9; padding: 10px 12px; border-radius: 6px; }
.note { margin-top: 14px; font-size: 13px; color: #687180; }
@media (prefers-color-scheme: dark) {
  body { background: #101114; color: #f0f2f6; }
  main { background: #181a20; border-color: #303540; box-shadow: none; }
  p, dt, .note { color: #aab2c0; }
  input[type=password] { background: #101114; color: #f0f2f6; border-color: #4a5362; }
}
</style>
</head>
<body>
<main>
<h1>Authorize brain-mcp</h1>
<p>ChatGPT is requesting access to your Brain MCP tools.</p>
%s
<dl>
<dt>Client</dt><dd>%s</dd>
<dt>Scopes</dt><dd>%s</dd>
</dl>
<form method="post" action="/oauth/authorize">
%s
<label for="approval_token">Approval token</label>
<input id="approval_token" type="password" name="approval_token" autocomplete="current-password" autofocus>
<button type="submit">Authorize</button>
</form>
<p class="note">This browser will stay approved for 30 days.</p>
</main>
</body>
</html>`, errorHTML, clientName, scope, hiddenAuthorizeInputs(values))
}

func hiddenAuthorizeInputs(values url.Values) string {
	keys := []string{"response_type", "client_id", "redirect_uri", "scope", "state", "resource", "code_challenge", "code_challenge_method"}
	var out strings.Builder
	for _, key := range keys {
		if value := values.Get(key); value != "" {
			fmt.Fprintf(&out, `<input type="hidden" name="%s" value="%s">`+"\n", html.EscapeString(key), html.EscapeString(value))
		}
	}
	return out.String()
}

func writeOAuthError(w http.ResponseWriter, code, description string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"error":             code,
		"error_description": description,
	})
}
