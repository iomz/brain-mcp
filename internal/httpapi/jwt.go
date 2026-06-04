package httpapi

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iomz/brain-mcp/internal/mcp"
)

var (
	errInvalidToken = errors.New("invalid oauth token")
	errAccessDenied = errors.New("oauth principal not allowlisted")
)

const (
	authRejectTokenParseError   = "token_parse_error"
	authRejectJWKSFetchError    = "jwks_fetch_error"
	authRejectSignatureInvalid  = "signature_invalid"
	authRejectIssuerMismatch    = "issuer_mismatch"
	authRejectAudienceMismatch  = "audience_mismatch"
	authRejectExpired           = "expired"
	authRejectNotYetValid       = "not_yet_valid"
	authRejectMissingSubject    = "missing_subject"
	authRejectMissingEmail      = "missing_email"
	authRejectAllowlistMismatch = "allowlist_mismatch"
)

type jwtValidationError struct {
	reason string
	err    error
	claims jwtClaims
}

func (e jwtValidationError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return e.reason
}

func (e jwtValidationError) Unwrap() error {
	return e.err
}

type jwtVerifier struct {
	issuer            string
	jwksURL           string
	acceptedAudiences []string
	allowedEmails     []string
	allowedSubjects   []string
	allowedGroups     []string
	defaultScopes     []string
	client            *http.Client

	mu        sync.Mutex
	cachedAt  time.Time
	cachedJWK jwks
}

type jwks struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

type jwtClaims struct {
	Iss      string          `json:"iss"`
	Sub      string          `json:"sub"`
	Aud      audienceClaim   `json:"aud"`
	Resource audienceClaim   `json:"resource"`
	Exp      int64           `json:"exp"`
	Nbf      int64           `json:"nbf"`
	Email    string          `json:"email"`
	Groups   groupsClaim     `json:"groups"`
	Scope    string          `json:"scope"`
	Scopes   groupsClaim     `json:"scopes"`
	Scp      groupsClaim     `json:"scp"`
	Raw      json.RawMessage `json:"-"`
}

type audienceClaim []string

func (a *audienceClaim) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*a = []string{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

type groupsClaim []string

func (g *groupsClaim) UnmarshalJSON(data []byte) error {
	var many []string
	if err := json.Unmarshal(data, &many); err == nil {
		*g = many
		return nil
	}
	var one string
	if err := json.Unmarshal(data, &one); err != nil {
		return err
	}
	if strings.TrimSpace(one) == "" {
		*g = nil
		return nil
	}
	*g = strings.FieldsFunc(one, func(r rune) bool {
		return r == ',' || r == ' '
	})
	return nil
}

func newJWTVerifier(auth AuthConfig) *jwtVerifier {
	audiences := append([]string{}, auth.OAuthAcceptedAudiences...)
	if auth.OAuthClientID != "" && !contains(audiences, auth.OAuthClientID) {
		audiences = append(audiences, auth.OAuthClientID)
	}
	return &jwtVerifier{
		issuer:            auth.OAuthIssuer,
		jwksURL:           auth.OAuthJWKSURL,
		acceptedAudiences: audiences,
		allowedEmails:     auth.AllowedEmails,
		allowedSubjects:   auth.AllowedSubjects,
		allowedGroups:     auth.AllowedGroups,
		defaultScopes:     auth.OAuthDefaultScopes,
		client:            http.DefaultClient,
	}
}

func (v *jwtVerifier) Verify(token string) (mcp.AuthContext, error) {
	if v.issuer == "" || v.jwksURL == "" || len(v.acceptedAudiences) == 0 {
		return mcp.AuthContext{}, jwtValidationError{reason: authRejectTokenParseError, err: errInvalidToken}
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return mcp.AuthContext{}, jwtValidationError{reason: authRejectTokenParseError, err: errInvalidToken}
	}
	var header jwtHeader
	if err := decodeJWTPart(parts[0], &header); err != nil {
		return mcp.AuthContext{}, jwtValidationError{reason: authRejectTokenParseError, err: errInvalidToken}
	}
	if header.Alg != "RS256" {
		return mcp.AuthContext{}, jwtValidationError{reason: authRejectSignatureInvalid, err: errInvalidToken}
	}
	var claims jwtClaims
	if err := decodeJWTPart(parts[1], &claims); err != nil {
		return mcp.AuthContext{}, jwtValidationError{reason: authRejectTokenParseError, err: errInvalidToken}
	}
	if claims.Iss != v.issuer {
		return mcp.AuthContext{}, jwtValidationError{reason: authRejectIssuerMismatch, err: errInvalidToken, claims: claims}
	}
	now := time.Now().Unix()
	if claims.Exp <= now {
		return mcp.AuthContext{}, jwtValidationError{reason: authRejectExpired, err: errInvalidToken, claims: claims}
	}
	if claims.Nbf != 0 && claims.Nbf > now {
		return mcp.AuthContext{}, jwtValidationError{reason: authRejectNotYetValid, err: errInvalidToken, claims: claims}
	}
	tokenAudiences := append([]string(claims.Aud), []string(claims.Resource)...)
	if !anyOverlap(tokenAudiences, v.acceptedAudiences) {
		return mcp.AuthContext{}, jwtValidationError{reason: authRejectAudienceMismatch, err: errInvalidToken, claims: claims}
	}
	keys, err := v.keys()
	if err != nil {
		return mcp.AuthContext{}, jwtValidationError{reason: authRejectJWKSFetchError, err: err, claims: claims}
	}
	signed := []byte(parts[0] + "." + parts[1])
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return mcp.AuthContext{}, jwtValidationError{reason: authRejectTokenParseError, err: errInvalidToken, claims: claims}
	}
	if err := verifyRS256(keys.Keys, header.Kid, signed, signature); err != nil {
		return mcp.AuthContext{}, jwtValidationError{reason: authRejectSignatureInvalid, err: errInvalidToken, claims: claims}
	}
	if ok, reason := v.allowlisted(claims); !ok {
		return mcp.AuthContext{}, jwtValidationError{reason: reason, err: errAccessDenied, claims: claims}
	}
	scopes := brainScopes(claims)
	if len(scopes) == 0 {
		scopes = v.defaultScopes
	}
	return mcp.AuthContext{
		Kind:      mcp.AuthKindOAuth,
		Subject:   claims.Sub,
		Email:     claims.Email,
		Groups:    []string(claims.Groups),
		Scopes:    scopes,
		Issuer:    claims.Iss,
		Audience:  []string(claims.Aud),
		ExpiresAt: claims.Exp,
	}, nil
}

func decodeJWTPart(part string, out any) error {
	data, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func (v *jwtVerifier) keys() (jwks, error) {
	v.mu.Lock()
	if time.Since(v.cachedAt) < 5*time.Minute && len(v.cachedJWK.Keys) > 0 {
		keys := v.cachedJWK
		v.mu.Unlock()
		return keys, nil
	}
	v.mu.Unlock()

	resp, err := v.client.Get(v.jwksURL)
	if err != nil {
		return jwks{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return jwks{}, fmt.Errorf("jwks fetch failed: %d", resp.StatusCode)
	}
	var keys jwks
	if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
		return jwks{}, err
	}
	if len(keys.Keys) == 0 {
		return jwks{}, errInvalidToken
	}

	v.mu.Lock()
	v.cachedJWK = keys
	v.cachedAt = time.Now()
	v.mu.Unlock()
	return keys, nil
}

func verifyRS256(keys []jwk, kid string, signed, signature []byte) error {
	var last error = errInvalidToken
	for _, key := range keys {
		if key.Kty != "RSA" || (kid != "" && key.Kid != kid) {
			continue
		}
		pub, err := rsaPublicKey(key)
		if err != nil {
			last = err
			continue
		}
		sum := sha256.Sum256(signed)
		if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], signature); err == nil {
			return nil
		}
	}
	return last
}

func rsaPublicKey(key jwk) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
	if err != nil {
		return nil, err
	}
	e := 0
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}
	if e == 0 {
		return nil, errInvalidToken
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}, nil
}

func (v *jwtVerifier) allowlisted(claims jwtClaims) (bool, string) {
	if len(v.allowedEmails) == 0 && len(v.allowedSubjects) == 0 && len(v.allowedGroups) == 0 {
		return false, authRejectAllowlistMismatch
	}
	if contains(v.allowedEmails, claims.Email) || contains(v.allowedSubjects, claims.Sub) {
		return true, ""
	}
	if anyOverlap([]string(claims.Groups), v.allowedGroups) {
		return true, ""
	}
	if len(v.allowedEmails) > 0 && claims.Email == "" {
		return false, authRejectMissingEmail
	}
	if len(v.allowedSubjects) > 0 && claims.Sub == "" {
		return false, authRejectMissingSubject
	}
	return false, authRejectAllowlistMismatch
}

func brainScopes(claims jwtClaims) []string {
	seen := map[string]bool{}
	var out []string
	add := func(scope string) {
		scope = strings.TrimSpace(scope)
		if strings.HasPrefix(scope, "brain:") && !seen[scope] {
			seen[scope] = true
			out = append(out, scope)
		}
	}
	for _, scope := range strings.Fields(claims.Scope) {
		add(scope)
	}
	for _, scope := range claims.Scopes {
		add(scope)
	}
	for _, scope := range claims.Scp {
		add(scope)
	}
	return out
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func anyOverlap(left, right []string) bool {
	for _, l := range left {
		for _, r := range right {
			if l == r {
				return true
			}
		}
	}
	return false
}
