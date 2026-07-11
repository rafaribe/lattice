// Package oidc provides a generic OIDC outbound adapter.
package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"

	"github.com/rafaribe/beagrid/internal/auth"
)

// Provider implements the auth.OIDCProvider port using a generic OIDC discovery flow.
type Provider struct {
	config      auth.OIDCConfig
	oauth2Cfg   *oauth2.Config
	userinfoURL string
}

// New creates a new OIDC provider adapter. It performs OIDC discovery at startup.
func New(cfg auth.OIDCConfig) (*Provider, error) {
	// Discover endpoints from the issuer's .well-known/openid-configuration
	disc, err := discover(cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery failed for %s: %w", cfg.Issuer, err)
	}

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
	}

	oauth2Cfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Scopes:       scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  disc.AuthorizationEndpoint,
			TokenURL: disc.TokenEndpoint,
		},
	}

	return &Provider{
		config:      cfg,
		oauth2Cfg:   oauth2Cfg,
		userinfoURL: disc.UserinfoEndpoint,
	}, nil
}

// AuthURL returns the URL to redirect the user to for OIDC login.
func (p *Provider) AuthURL(state string) string {
	return p.oauth2Cfg.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// Exchange trades an authorization code for user info.
func (p *Provider) Exchange(ctx context.Context, code string) (*auth.OIDCUserInfo, error) {
	token, err := p.oauth2Cfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}

	client := p.oauth2Cfg.Client(ctx, token)
	resp, err := client.Get(p.userinfoURL)
	if err != nil {
		return nil, fmt.Errorf("userinfo request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("userinfo returned %d: %s", resp.StatusCode, string(body))
	}

	var info auth.OIDCUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decoding userinfo: %w", err)
	}
	return &info, nil
}

// --- OIDC Discovery ---

type discoveryDoc struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
}

func discover(issuer string) (*discoveryDoc, error) {
	url := issuer + "/.well-known/openid-configuration"
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery returned %d", resp.StatusCode)
	}

	var doc discoveryDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}
	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" {
		return nil, fmt.Errorf("incomplete discovery document")
	}
	return &doc, nil
}
