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

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var (
	_keycloakURL  = os.Getenv("KEYCLOAK_URL")
	_realm        = os.Getenv("KEYCLOAK_REALM")
	_clientID     = os.Getenv("KEYCLOAK_CLIENT_ID")
	_clientSecret = os.Getenv("KEYCLOAK_CLIENT_SECRET")
	_redirectURL  = os.Getenv("KEYCLOAK_REDIRECT_URL")

	_oauth2Config *oauth2.Config
	_verifier     *oidc.IDTokenVerifier
	_provider     *oidc.Provider
	_adminMu      sync.Mutex
	_adminExpiry  time.Time
)

type KeycloakUser struct {
	Subject    string   `json:"sub"`
	Email      string   `json:"email"`
	Name       string   `json:"name"`
	GivenName  string   `json:"given_name"`
	FamilyName string   `json:"family_name"`
	Username   string   `json:"preferred_username"`
	Groups     []string `json:"groups"`
	Roles      []string `json:"roles,omitempty"`
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
}

func main() {
	if _keycloakURL == "" {
		log.Fatal("KEYCLOAK_URL is required")
	}
	if _realm == "" {
		log.Fatal("KEYCLOAK_REALM is required")
	}
	if _clientID == "" {
		log.Fatal("KEYCLOAK_CLIENT_ID is required")
	}
	if _redirectURL == "" {
		log.Fatal("KEYCLOAK_REDIRECT_URL is required")
	}

	ctx := context.Background()

	provider, err := oidc.NewProvider(ctx, fmt.Sprintf("%s/realms/%s", strings.TrimSuffix(_keycloakURL, "/"), _realm))
	if err != nil {
		log.Fatalf("Failed to create OIDC provider: %v", err)
	}
	_provider = provider

	_oauth2Config = &oauth2.Config{
		ClientID:     _clientID,
		ClientSecret: _clientSecret,
		RedirectURL:  _redirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email", "groups"},
	}

	_verifier = provider.Verifier(&oidc.Config{
		ClientID: _clientID,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", handleOIDCDiscovery)
	mux.HandleFunc("/.well-known/jwks", handleJWKS)
	mux.HandleFunc("/authorize", handleAuthorize)
	mux.HandleFunc("/token", handleToken)
	mux.HandleFunc("/userinfo", handleUserInfo)
	mux.HandleFunc("/users", handleUsers)
	mux.HandleFunc("/groups", handleGroups)
	mux.HandleFunc("/health", handleHealth)

	port := getEnvOr("PORT", "8080")
	log.Printf("identity-keycloak starting on :%s", port)
	log.Printf("Keycloak: %s/realms/%s", _keycloakURL, _realm)

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func handleOIDCDiscovery(w http.ResponseWriter, r *http.Request) {
	cfg := map[string]interface{}{
		"issuer":                                fmt.Sprintf("%s/realms/%s", _keycloakURL, _realm),
		"authorization_endpoint":                fmt.Sprintf("%s/realms/%s/protocol/openid-connect/auth", _keycloakURL, _realm),
		"token_endpoint":                        fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", _keycloakURL, _realm),
		"userinfo_endpoint":                     fmt.Sprintf("%s/realms/%s/protocol/openid-connect/userinfo", _keycloakURL, _realm),
		"jwks_uri":                              fmt.Sprintf("%s/realms/%s/protocol/openid-connect/certs", _keycloakURL, _realm),
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public", "pairwise"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email", "groups"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

func handleJWKS(w http.ResponseWriter, r *http.Request) {
	jwksURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/certs", _keycloakURL, _realm)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, jwksURL, nil)
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
		http.Error(w, "Keycloak JWKS endpoint returned an error", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = io.Copy(w, resp.Body)
}

func handleAuthorize(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	redirectURI := r.URL.Query().Get("redirect_uri")
	responseType := r.URL.Query().Get("response_type")
	clientID := r.URL.Query().Get("client_id")

	if clientID == "" || clientID != _clientID || redirectURI == "" {
		http.Error(w, "client_id and redirect_uri required", http.StatusBadRequest)
		return
	}
	if !isAllowedRedirect(redirectURI) {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	if responseType != "code" {
		http.Error(w, "only authorization code flow is supported", http.StatusBadRequest)
		return
	}

	authURL := _oauth2Config.AuthCodeURL(state, oauth2.AccessTypeOnline)

	if redirectURI != "" {
		params := url.Values{}
		params.Set("redirect_uri", redirectURI)
		params.Set("response_type", responseType)
		params.Set("client_id", _clientID)
		if state != "" {
			params.Set("state", state)
		}
		for _, key := range []string{"scope", "nonce", "code_challenge", "code_challenge_method"} {
			if value := r.URL.Query().Get(key); value != "" {
				params.Set(key, value)
			}
		}
		authURL = fmt.Sprintf("%s/realms/%s/protocol/openid-connect/auth?%s",
			_keycloakURL, _realm, params.Encode())
	}

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

	var tokenResp TokenResponse

	if grantType == "authorization_code" {
		if code == "" {
			http.Error(w, "code required", http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		options := make([]oauth2.AuthCodeOption, 0, 1)
		if verifier := r.Form.Get("code_verifier"); verifier != "" {
			options = append(options, oauth2.SetAuthURLParam("code_verifier", verifier))
		}
		token, err := _oauth2Config.Exchange(ctx, code, options...)
		if err != nil {
			http.Error(w, fmt.Sprintf("Token exchange failed: %v", err), http.StatusInternalServerError)
			return
		}

		rawIDToken, ok := token.Extra("id_token").(string)
		if ok {
			idToken, err := _verifier.Verify(ctx, rawIDToken)
			if err != nil {
				http.Error(w, fmt.Sprintf("ID token verification failed: %v", err), http.StatusInternalServerError)
				return
			}

			var claims struct {
				Subject string   `json:"sub"`
				Email   string   `json:"email"`
				Name    string   `json:"name"`
				Groups  []string `json:"groups"`
			}
			if err := idToken.Claims(&claims); err != nil {
				http.Error(w, fmt.Sprintf("Failed to parse claims: %v", err), http.StatusInternalServerError)
				return
			}

			_ = claims
		}

		idToken, _ := token.Extra("id_token").(string)
		tokenResp = TokenResponse{
			AccessToken:  token.AccessToken,
			TokenType:    token.TokenType,
			ExpiresIn:    int(token.Expiry.Sub(time.Now()).Seconds()),
			RefreshToken: token.RefreshToken,
			IDToken:      idToken,
		}
	} else if grantType == "refresh_token" {
		refreshToken := r.Form.Get("refresh_token")
		if refreshToken == "" {
			http.Error(w, "refresh_token required", http.StatusBadRequest)
			return
		}
		ctx := r.Context()

		token, err := _oauth2Config.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken}).Token()
		if err != nil {
			http.Error(w, fmt.Sprintf("Token refresh failed: %v", err), http.StatusInternalServerError)
			return
		}

		tokenResp = TokenResponse{
			AccessToken:  token.AccessToken,
			TokenType:    token.TokenType,
			ExpiresIn:    int(token.Expiry.Sub(time.Now()).Seconds()),
			RefreshToken: token.RefreshToken,
		}
	} else {
		http.Error(w, "Unsupported grant_type", http.StatusBadRequest)
		return
	}

	json.NewEncoder(w).Encode(tokenResp)
}

func handleUserInfo(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "Authorization header required", http.StatusUnauthorized)
		return
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		http.Error(w, "Invalid authorization header", http.StatusUnauthorized)
		return
	}

	accessToken := parts[1]
	ctx := r.Context()

	userInfo, err := _provider.UserInfo(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accessToken}))
	if err != nil {
		http.Error(w, "invalid access token", http.StatusUnauthorized)
		return
	}

	var claims struct {
		Subject    string   `json:"sub"`
		Email      string   `json:"email"`
		Name       string   `json:"name"`
		GivenName  string   `json:"given_name"`
		FamilyName string   `json:"family_name"`
		Username   string   `json:"preferred_username"`
		Groups     []string `json:"groups"`
	}

	if err := userInfo.Claims(&claims); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse claims: %v", err), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"sub":                claims.Subject,
		"email":              claims.Email,
		"name":               claims.Name,
		"given_name":         claims.GivenName,
		"family_name":        claims.FamilyName,
		"preferred_username": claims.Username,
		"groups":             claims.Groups,
	})
}

func handleUsers(w http.ResponseWriter, r *http.Request) {
	usersURL := fmt.Sprintf("%s/admin/realms/%s/users", _keycloakURL, _realm)

	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, usersURL, nil)
	if !copyBearerToken(w, r, req) {
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch users: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		http.Error(w, "Keycloak users endpoint returned an error", http.StatusBadGateway)
		return
	}

	var users []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		http.Error(w, fmt.Sprintf("Failed to decode response: %v", err), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"items": users,
		"total": len(users),
	})
}

func handleGroups(w http.ResponseWriter, r *http.Request) {
	groupsURL := fmt.Sprintf("%s/admin/realms/%s/groups", _keycloakURL, _realm)

	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, groupsURL, nil)
	if !copyBearerToken(w, r, req) {
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch groups: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		http.Error(w, "Keycloak groups endpoint returned an error", http.StatusBadGateway)
		return
	}

	var groups []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&groups); err != nil {
		http.Error(w, fmt.Sprintf("Failed to decode response: %v", err), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"items": groups,
		"total": len(groups),
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"url":    _keycloakURL,
		"realm":  _realm,
	})
}

var _adminToken string

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
	if _clientSecret == "" {
		return clientSecret == ""
	}
	return subtle.ConstantTimeCompare([]byte(clientSecret), []byte(_clientSecret)) == 1
}

func copyBearerToken(w http.ResponseWriter, r *http.Request, req *http.Request) bool {
	value := r.Header.Get("Authorization")
	parts := strings.SplitN(value, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		http.Error(w, "Authorization bearer token required", http.StatusUnauthorized)
		return false
	}
	req.Header.Set("Authorization", value)
	return true
}

func getAdminToken() string {
	_adminMu.Lock()
	defer _adminMu.Unlock()
	if _adminToken != "" && time.Now().Before(_adminExpiry) {
		return _adminToken
	}

	tokenURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", _keycloakURL, _realm)

	resp, err := http.PostForm(tokenURL, map[string][]string{
		"grant_type":    {"client_credentials"},
		"client_id":     {_clientID},
		"client_secret": {_clientSecret},
	})
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return ""
	}

	_adminToken = token.AccessToken
	_adminExpiry = time.Now().Add(5 * time.Minute)
	return _adminToken
}

func getEnvOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
