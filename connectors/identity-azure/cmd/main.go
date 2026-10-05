package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/confidential"
)

var (
	_clientID     = os.Getenv("AZURE_CLIENT_ID")
	_clientSecret = os.Getenv("AZURE_CLIENT_SECRET")
	_tenantID     = os.Getenv("AZURE_TENANT_ID")
	_authority    = fmt.Sprintf("https://login.microsoftonline.com/%s", getEnvOr("AZURE_TENANT_ID", "common"))
	_redirectURL  = os.Getenv("AZURE_REDIRECT_URL")

	_msalClient *confidential.Client
	_userCache  sync.Map
	_config     = AzureConfig{
		ClientID:     _clientID,
		ClientSecret: _clientSecret,
		TenantID:     _tenantID,
		Authority:    _authority,
		RedirectURL:  _redirectURL,
	}
)

type AzureConfig struct {
	ClientID     string
	ClientSecret string
	TenantID     string
	Authority    string
	RedirectURL  string
	Scopes       []string
}

type AzureUser struct {
	OID         string
	Email       string
	Name        string
	Username    string
	Groups      []string
	Roles       []string
	TenantID    string
	IsLocal     bool
	LinkedAt    time.Time
	LastLoginAt time.Time
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
}

func main() {
	if _clientID == "" || _clientSecret == "" || _tenantID == "" || _redirectURL == "" {
		log.Fatal("AZURE_CLIENT_ID, AZURE_CLIENT_SECRET, AZURE_TENANT_ID and AZURE_REDIRECT_URL are required")
	}
	if err := initializeMSAL(); err != nil {
		log.Printf("Warning: MSAL initialization failed: %v (will use OIDC fallback)", err)
	}

	r := http.NewServeMux()

	r.HandleFunc("/.well-known/openid-configuration", handleOIDCDiscovery)
	r.HandleFunc("/.well-known/jwks", handleJWKS)
	r.HandleFunc("/authorize", handleAuthorize)
	r.HandleFunc("/token", handleToken)
	r.HandleFunc("/userinfo", handleUserInfo)
	r.HandleFunc("/health", handleHealth)

	port := getEnvOr("PORT", "8080")
	log.Printf("identity-azure connector starting on :%s", port)
	log.Printf("Azure AD: tenant=%s, client=%s", _tenantID, _clientID)

	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func initializeMSAL() error {
	if _clientID == "" || _clientSecret == "" || _tenantID == "" {
		return fmt.Errorf("Azure credentials not configured")
	}

	credential, err := confidential.NewCredFromSecret(_clientSecret)
	if err != nil {
		return fmt.Errorf("create Azure credential: %w", err)
	}
	client, err := confidential.New(_authority, _clientID, credential)
	if err != nil {
		return fmt.Errorf("create MSAL client: %w", err)
	}
	_msalClient = &client

	return nil
}

func handleOIDCDiscovery(w http.ResponseWriter, r *http.Request) {
	discovery := map[string]interface{}{
		"issuer":                                "https://login.microsoftonline.com/" + _tenantID + "/v2.0",
		"authorization_endpoint":                "https://login.microsoftonline.com/" + _tenantID + "/oauth2/v2.0/authorize",
		"token_endpoint":                        "https://login.microsoftonline.com/" + _tenantID + "/oauth2/v2.0/token",
		"userinfo_endpoint":                     "https://graph.microsoft.com/oidc/userinfo",
		"jwks_uri":                              "https://login.microsoftonline.com/" + _tenantID + "/discovery/v2.0/keys",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"pairwise"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email", "offline_access"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post", "private_key_jwt"},
		"claims_supported": []string{
			"sub", "iss", "aud", "exp", "iat", "name", "preferred_username",
			"email", "oid", "tid", "roles", "groups",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(discovery)
}

func handleJWKS(w http.ResponseWriter, r *http.Request) {
	jwksURI := fmt.Sprintf("https://login.microsoftonline.com/%s/discovery/v2.0/keys", _tenantID)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, jwksURI, nil)
	if err != nil {
		http.Error(w, "Failed to create JWKS request", http.StatusInternalServerError)
		return
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		http.Error(w, "Failed to fetch JWKS", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		http.Error(w, "Azure JWKS endpoint returned an error", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = io.Copy(w, resp.Body)
}

func handleAuthorize(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	responseType := query.Get("response_type")
	clientID := query.Get("client_id")
	redirectURI := query.Get("redirect_uri")
	state := query.Get("state")
	scope := query.Get("scope")
	prompt := query.Get("prompt")

	if clientID == "" || redirectURI == "" {
		http.Error(w, "client_id and redirect_uri are required", http.StatusBadRequest)
		return
	}

	if clientID != _clientID {
		http.Error(w, "invalid client_id", http.StatusBadRequest)
		return
	}
	if !isAllowedRedirect(redirectURI) {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}

	if responseType != "code" {
		http.Error(w, "unsupported response_type", http.StatusBadRequest)
		return
	}

	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("response_type", responseType)
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", scope)
	params.Set("state", state)
	params.Set("response_mode", "query")
	if prompt != "" {
		params.Set("prompt", prompt)
	}
	for _, key := range []string{"nonce", "code_challenge", "code_challenge_method"} {
		if value := query.Get(key); value != "" {
			params.Set(key, value)
		}
	}
	authURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/authorize?%s", _tenantID, params.Encode())

	http.Redirect(w, r, authURL, http.StatusFound)
}

func handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}
	if !authenticateClient(r) {
		http.Error(w, "invalid client credentials", http.StatusUnauthorized)
		return
	}

	grantType := r.Form.Get("grant_type")
	code := r.Form.Get("code")
	redirectURI := r.Form.Get("redirect_uri")
	if grantType == "authorization_code" && !isAllowedRedirect(redirectURI) {
		http.Error(w, "invalid client credentials", http.StatusUnauthorized)
		return
	}

	var tokenResp TokenResponse
	var err error

	if grantType == "authorization_code" {
		tokenResp, err = exchangeCodeForToken(r.Context(), code, redirectURI)
		if err != nil {
			http.Error(w, fmt.Sprintf("token exchange failed: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tokenResp)
		return
	}

	if grantType == "refresh_token" {
		refreshToken := r.Form.Get("refresh_token")
		tokenResp, err := refreshAccessToken(refreshToken)
		if err != nil {
			http.Error(w, fmt.Sprintf("refresh failed: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tokenResp)
		return
	}

	http.Error(w, "unsupported grant_type", http.StatusBadRequest)
}

func handleUserInfo(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "Authorization header required", http.StatusUnauthorized)
		return
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		http.Error(w, "Invalid authorization header format", http.StatusUnauthorized)
		return
	}

	accessToken := parts[1]

	user, err := getUserInfo(r.Context(), accessToken)
	if err != nil {
		http.Error(w, "invalid access token", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sub":    user.OID,
		"name":   user.Name,
		"email":  user.Email,
		"oid":    user.OID,
		"tid":    user.TenantID,
		"groups": user.Groups,
		"roles":  user.Roles,
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	status := "healthy"
	if _msalClient == nil {
		status = "degraded"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    status,
		"tenant_id": _tenantID,
		"client_id": _clientID,
	})
}

func exchangeCodeForToken(ctx context.Context, code, redirectURI string) (TokenResponse, error) {
	if _msalClient != nil {
		result, err := _msalClient.AcquireTokenByAuthCode(ctx, code, redirectURI,
			[]string{"openid", "profile", "email", "offline_access"},
		)
		if err != nil {
			return TokenResponse{}, err
		}

		return TokenResponse{
			AccessToken: result.AccessToken,
			TokenType:   "Bearer",
			ExpiresIn:   int(time.Until(result.ExpiresOn).Seconds()),
			IDToken:     result.IDToken.RawToken,
		}, nil
	}

	tokenURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", _tenantID)

	resp, err := http.PostForm(tokenURL, map[string][]string{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {_clientID},
		"client_secret": {_clientSecret},
		"scope":         {"openid profile email offline_access"},
	})
	if err != nil {
		return TokenResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return TokenResponse{}, fmt.Errorf("token endpoint returned HTTP %d", resp.StatusCode)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return TokenResponse{}, err
	}

	return tokenResp, nil
}

func refreshAccessToken(refreshToken string) (TokenResponse, error) {
	tokenURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", _tenantID)

	resp, err := http.PostForm(tokenURL, map[string][]string{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {_clientID},
		"client_secret": {_clientSecret},
		"scope":         {"openid profile email offline_access"},
	})
	if err != nil {
		return TokenResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return TokenResponse{}, fmt.Errorf("refresh token endpoint returned HTTP %d", resp.StatusCode)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return TokenResponse{}, err
	}

	return tokenResp, nil
}

func getUserInfo(ctx context.Context, accessToken string) (*AzureUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://graph.microsoft.com/oidc/userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("userinfo endpoint returned HTTP %d", resp.StatusCode)
	}

	var claims struct {
		Sub    string   `json:"sub"`
		Name   string   `json:"name"`
		Email  string   `json:"email"`
		OID    string   `json:"oid"`
		TID    string   `json:"tid"`
		Roles  []string `json:"roles"`
		Groups []string `json:"groups"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&claims); err != nil {
		return nil, err
	}

	user := &AzureUser{
		OID:      claims.OID,
		Email:    claims.Email,
		Name:     claims.Name,
		TenantID: claims.TID,
		Roles:    claims.Roles,
		Groups:   claims.Groups,
	}
	if user.OID == "" {
		user.OID = claims.Sub
	}

	if user.Email == "" {
		user.Email = claims.OID + "@" + claims.TID + ".onmicrosoft.com"
	}

	_userCache.Store(user.OID, user)

	return user, nil
}

func isAllowedRedirect(candidate string) bool {
	if _redirectURL == "" || candidate == "" {
		return false
	}
	for _, allowed := range strings.Split(_redirectURL, ",") {
		if strings.TrimSpace(allowed) == candidate {
			return true
		}
	}
	return false
}

func authenticateClient(r *http.Request) bool {
	clientID, clientSecret, ok := r.BasicAuth()
	if !ok {
		clientID = r.Form.Get("client_id")
		clientSecret = r.Form.Get("client_secret")
	}
	if clientID == "" || subtle.ConstantTimeCompare([]byte(clientID), []byte(_clientID)) != 1 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(clientSecret), []byte(_clientSecret)) == 1
}

func getEnvOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func urlEncode(s string) string {
	return fmt.Sprintf("%v", url.QueryEscape(s))
}
