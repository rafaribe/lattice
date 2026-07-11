// Package auth provides the authentication HTTP inbound adapter.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/rafaribe/beagrid/internal/auth"
)

// Handler provides HTTP endpoints for authentication.
type Handler struct {
	users    auth.UserStore
	sessions auth.SessionStore
	oidc     auth.OIDCProvider // nil if OIDC is not configured
	logger   *slog.Logger
}

// NewHandler creates a new auth HTTP handler.
func NewHandler(users auth.UserStore, sessions auth.SessionStore, oidc auth.OIDCProvider, logger *slog.Logger) *Handler {
	return &Handler{users: users, sessions: sessions, oidc: oidc, logger: logger}
}

// RegisterRoutes registers auth endpoints on the mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth/register", h.handleRegister)
	mux.HandleFunc("POST /auth/login", h.handleLogin)
	mux.HandleFunc("POST /auth/logout", h.handleLogout)
	mux.HandleFunc("GET /auth/me", h.handleMe)

	if h.oidc != nil {
		mux.HandleFunc("GET /auth/oidc/login", h.handleOIDCLogin)
		mux.HandleFunc("GET /auth/oidc/callback", h.handleOIDCCallback)
	}
}

// AuthMiddleware protects routes by requiring a valid session token.
// Returns an http.Handler that calls next only if authenticated.
func (h *Handler) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			h.jsonError(w, 401, "authentication required")
			return
		}

		session, err := h.sessions.GetByToken(r.Context(), token)
		if err != nil || session == nil {
			h.jsonError(w, 401, "invalid or expired session")
			return
		}

		if time.Now().After(session.ExpiresAt) {
			_ = h.sessions.Delete(r.Context(), session.ID)
			h.jsonError(w, 401, "session expired")
			return
		}

		// Inject user ID into request header for downstream handlers
		r.Header.Set("X-User-ID", session.UserID)
		next.ServeHTTP(w, r)
	})
}

// --- Register ---

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, 400, "invalid JSON")
		return
	}

	if req.Email == "" || req.Password == "" {
		h.jsonError(w, 400, "email and password are required")
		return
	}
	if len(req.Password) < 8 {
		h.jsonError(w, 400, "password must be at least 8 characters")
		return
	}

	// Check if user already exists
	existing, _ := h.users.GetByEmail(r.Context(), req.Email)
	if existing != nil {
		h.jsonError(w, 409, "user with this email already exists")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.jsonError(w, 500, "internal error")
		return
	}

	// First user gets admin role
	role := auth.RoleUser
	count, _ := h.users.Count(r.Context())
	if count == 0 {
		role = auth.RoleAdmin
	}

	now := time.Now()
	user := &auth.User{
		ID:           uuid.New().String(),
		Email:        req.Email,
		DisplayName:  req.DisplayName,
		PasswordHash: string(hash),
		Provider:     auth.ProviderLocal,
		Role:         role,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := h.users.Create(r.Context(), user); err != nil {
		h.logger.Error("user create failed", "err", err)
		h.jsonError(w, 500, "failed to create user")
		return
	}

	h.logger.Info("user registered", "id", user.ID, "email", user.Email, "role", role)
	h.writeJSON(w, map[string]any{"id": user.ID, "email": user.Email, "role": user.Role}, 201)
}

// --- Login ---

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, 400, "invalid JSON")
		return
	}

	user, err := h.users.GetByEmail(r.Context(), req.Email)
	if err != nil || user == nil {
		h.jsonError(w, 401, "invalid credentials")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		h.jsonError(w, 401, "invalid credentials")
		return
	}

	// Create session
	token := generateToken()
	session := &auth.Session{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour), // 7 days
		CreatedAt: time.Now(),
	}

	if err := h.sessions.Create(r.Context(), session); err != nil {
		h.jsonError(w, 500, "failed to create session")
		return
	}

	// Update last login
	user.LastLoginAt = time.Now()
	user.UpdatedAt = time.Now()
	_ = h.users.Update(r.Context(), user)

	h.logger.Info("user logged in", "id", user.ID, "email", user.Email)
	h.writeJSON(w, map[string]any{
		"token":      token,
		"expires_at": session.ExpiresAt,
		"user": map[string]any{
			"id":           user.ID,
			"email":        user.Email,
			"display_name": user.DisplayName,
			"role":         user.Role,
		},
	}, 200)
}

// --- Logout ---

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if token == "" {
		h.jsonError(w, 400, "no token provided")
		return
	}

	session, err := h.sessions.GetByToken(r.Context(), token)
	if err != nil || session == nil {
		h.writeJSON(w, map[string]string{"status": "ok"}, 200)
		return
	}

	_ = h.sessions.Delete(r.Context(), session.ID)
	h.writeJSON(w, map[string]string{"status": "logged_out"}, 200)
}

// --- Me ---

func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if token == "" {
		h.jsonError(w, 401, "authentication required")
		return
	}

	session, err := h.sessions.GetByToken(r.Context(), token)
	if err != nil || session == nil || time.Now().After(session.ExpiresAt) {
		h.jsonError(w, 401, "invalid or expired session")
		return
	}

	user, err := h.users.GetByID(r.Context(), session.UserID)
	if err != nil || user == nil {
		h.jsonError(w, 401, "user not found")
		return
	}

	h.writeJSON(w, map[string]any{
		"id":           user.ID,
		"email":        user.Email,
		"display_name": user.DisplayName,
		"role":         user.Role,
		"provider":     user.Provider,
		"created_at":   user.CreatedAt,
	}, 200)
}

// --- OIDC ---

func (h *Handler) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	state := generateToken()
	// In production, store state in a short-lived cookie for CSRF validation
	http.SetCookie(w, &http.Cookie{
		Name:     "oidc_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		MaxAge:   300, // 5 minutes
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, h.oidc.AuthURL(state), http.StatusFound)
}

func (h *Handler) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	// Validate state
	stateCookie, err := r.Cookie("oidc_state")
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		h.jsonError(w, 400, "invalid OIDC state")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		h.jsonError(w, 400, "missing authorization code")
		return
	}

	// Exchange code for user info
	info, err := h.oidc.Exchange(r.Context(), code)
	if err != nil {
		h.logger.Error("OIDC exchange failed", "err", err)
		h.jsonError(w, 502, "OIDC authentication failed")
		return
	}

	// Find or create user
	user, _ := h.users.GetByProviderID(r.Context(), auth.ProviderOIDC, info.Subject)
	if user == nil {
		// Check by email (link accounts)
		user, _ = h.users.GetByEmail(r.Context(), info.Email)
	}

	now := time.Now()
	if user == nil {
		// First user gets admin
		role := auth.RoleUser
		count, _ := h.users.Count(r.Context())
		if count == 0 {
			role = auth.RoleAdmin
		}

		user = &auth.User{
			ID:          uuid.New().String(),
			Email:       info.Email,
			DisplayName: info.Name,
			Provider:    auth.ProviderOIDC,
			ProviderID:  info.Subject,
			Role:        role,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := h.users.Create(r.Context(), user); err != nil {
			h.jsonError(w, 500, "failed to create user")
			return
		}
		h.logger.Info("OIDC user created", "id", user.ID, "email", user.Email)
	} else {
		// Update provider info
		user.ProviderID = info.Subject
		user.Provider = auth.ProviderOIDC
		user.LastLoginAt = now
		user.UpdatedAt = now
		_ = h.users.Update(r.Context(), user)
	}

	// Create session
	token := generateToken()
	session := &auth.Session{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: now.Add(7 * 24 * time.Hour),
		CreatedAt: now,
	}
	if err := h.sessions.Create(r.Context(), session); err != nil {
		h.jsonError(w, 500, "failed to create session")
		return
	}

	// Redirect to UI with token as query param (UI stores it)
	http.Redirect(w, r, "/?token="+token, http.StatusFound)
}

// --- Helpers ---

func extractToken(r *http.Request) string {
	// Check Authorization header first
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	// Fallback to query param
	return r.URL.Query().Get("token")
}

func generateToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *Handler) writeJSON(w http.ResponseWriter, data any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *Handler) jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
