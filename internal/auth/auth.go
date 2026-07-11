// Package auth defines the authentication domain types and port interfaces.
package auth

import (
	"context"
	"time"
)

// User represents an authenticated user account.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"display_name"`
	PasswordHash string    `json:"-"` // never serialized
	Provider     Provider  `json:"provider"`
	ProviderID   string    `json:"provider_id,omitempty"` // external ID from OIDC
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastLoginAt  time.Time `json:"last_login_at,omitempty"`
}

// Session represents an active login session.
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Token     string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// Provider identifies how the user authenticated.
type Provider string

const (
	ProviderLocal Provider = "local"
	ProviderOIDC  Provider = "oidc"
)

// Role defines user permission levels.
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleUser   Role = "user"
	RoleViewer Role = "viewer"
)

// OIDCConfig holds the OIDC provider configuration.
type OIDCConfig struct {
	Issuer       string `json:"issuer"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURL  string `json:"redirect_url"`
	Scopes       []string `json:"scopes"`
}

// OIDCUserInfo is the user info returned from the OIDC provider.
type OIDCUserInfo struct {
	Subject     string `json:"sub"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	Picture     string `json:"picture,omitempty"`
}

// --- Port Interfaces (outbound) ---

// UserStore is the outbound port for user persistence.
type UserStore interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetByProviderID(ctx context.Context, provider Provider, providerID string) (*User, error)
	Update(ctx context.Context, user *User) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, limit, offset int) ([]*User, error)
	Count(ctx context.Context) (int, error)
}

// SessionStore is the outbound port for session persistence.
type SessionStore interface {
	Create(ctx context.Context, session *Session) error
	GetByToken(ctx context.Context, token string) (*Session, error)
	Delete(ctx context.Context, id string) error
	DeleteByUserID(ctx context.Context, userID string) error
	DeleteExpired(ctx context.Context) error
}

// OIDCProvider is the outbound port for OIDC authentication flows.
type OIDCProvider interface {
	AuthURL(state string) string
	Exchange(ctx context.Context, code string) (*OIDCUserInfo, error)
}
