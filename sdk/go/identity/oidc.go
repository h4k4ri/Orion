package identity

import (
	"context"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type OIDCConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

type OIDCVerifier struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	config   *oauth2.Config
	providerCtx context.Context
	issuer   string
}

type UserInfo struct {
	Subject string
	Email   string
	Name    string
	Groups  []string
}

func NewOIDCVerifier(ctx context.Context, cfg OIDCConfig) (*OIDCVerifier, error) {
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: cfg.ClientID,
	})

	scopes := cfg.Scopes
	if scopes == nil {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}

	config := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       scopes,
	}

	return &OIDCVerifier{
		provider:   provider,
		verifier:   verifier,
		config:     config,
		providerCtx: ctx,
		issuer:     cfg.IssuerURL,
	}, nil
}

func (v *OIDCVerifier) AuthCodeURL(state string) string {
	return v.config.AuthCodeURL(state)
}

func (v *OIDCVerifier) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return v.config.Exchange(ctx, code)
}

func (v *OIDCVerifier) VerifyIDToken(ctx context.Context, rawIDToken string) (*UserInfo, error) {
	idToken, err := v.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify ID token: %w", err)
	}

	var claims struct {
		Email   string   `json:"email"`
		Name    string   `json:"name"`
		Subject string   `json:"sub"`
		Groups  []string `json:"groups"`
	}

	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse claims: %w", err)
	}

	return &UserInfo{
		Subject: claims.Subject,
		Email:   claims.Email,
		Name:    claims.Name,
		Groups:  claims.Groups,
	}, nil
}

func (v *OIDCVerifier) GetUserInfo(ctx context.Context, token *oauth2.Token) (*UserInfo, error) {
	userInfo, err := v.provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	var claims struct {
		Email   string   `json:"email"`
		Name    string   `json:"name"`
		Groups  []string `json:"groups"`
	}

	if err := userInfo.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse user info: %w", err)
	}

	return &UserInfo{
		Subject: userInfo.Subject,
		Email:   claims.Email,
		Name:    claims.Name,
		Groups:  claims.Groups,
	}, nil
}

func (v *OIDCVerifier) DiscoverOIDCConfiguration(ctx context.Context) (*OIDCConfiguration, error) {
	if v.provider == nil {
		return nil, fmt.Errorf("OIDC provider is not initialized")
	}
	cfg := &OIDCConfiguration{
		Issuer:                v.issuer,
		AuthorizationEndpoint: v.provider.Endpoint().AuthURL,
		TokenEndpoint:         v.provider.Endpoint().TokenURL,
	}
	// go-oidc exposes the provider's verified key set internally, but not the
	// discovery JWKS URL. Keep discovery conservative instead of fabricating a
	// provider-specific URL that can point at the wrong endpoint.
	return cfg, nil
}

type OIDCConfiguration struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
}

type Claims struct {
	Subject       string   `json:"sub"`
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	Name          string   `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	Groups        []string `json:"groups"`
	TenantID      string   `json:"tenant_id"`
	Roles         []string `json:"roles"`
}

func ParseRolesFromClaims(claims *Claims, roleClaim string) []string {
	if roleClaim == "" {
		roleClaim = "roles"
	}

	switch roleClaim {
	case "groups":
		return claims.Groups
	case "roles":
		return claims.Roles
	default:
		var roles []string
		roles = append(roles, claims.Roles...)
		roles = append(roles, claims.Groups...)
		return roles
	}
}

func MapExternalUserToLocal(oidcUser *UserInfo, tenantID string) *LocalUser {
	username := oidcUser.Email
	if oidcUser.Name != "" {
		username = oidcUser.Name
	}

	localUser := &LocalUser{
		ExternalID: oidcUser.Subject,
		TenantID:   tenantID,
		Email:      oidcUser.Email,
		Name:       oidcUser.Name,
		Username:   username,
		Groups:     oidcUser.Groups,
	}

	if localUser.Username == "" {
		localUser.Username = strings.Split(localUser.Email, "@")[0]
	}

	return localUser
}

type LocalUser struct {
	ID           string
	ExternalID   string
	TenantID     string
	Email        string
	Name         string
	Username     string
	Groups       []string
	Roles        []string
	IsLocal      bool
	LinkedAt     string
	LastLoginAt  string
}

type TokenClaims struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
}
