package auth

import (
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/rafaribe/beagrid/internal/auth"
)

// ThemeHandler provides HTTP endpoints for theme management.
type ThemeHandler struct {
	themes auth.ThemeStore
	handler *Handler // reuse auth handler for token extraction
}

// NewThemeHandler creates a new theme HTTP handler.
func NewThemeHandler(themes auth.ThemeStore, authHandler *Handler) *ThemeHandler {
	return &ThemeHandler{themes: themes, handler: authHandler}
}

// RegisterRoutes registers theme endpoints.
func (h *ThemeHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/themes", h.handleList)
	mux.HandleFunc("GET /api/themes/{id}", h.handleGet)
	mux.HandleFunc("POST /api/themes", h.handleCreate)
	mux.HandleFunc("PUT /api/themes/{id}", h.handleUpdate)
	mux.HandleFunc("DELETE /api/themes/{id}", h.handleDelete)
}

// --- List all public themes ---

func (h *ThemeHandler) handleList(w http.ResponseWriter, r *http.Request) {
	themes, err := h.themes.List(r.Context())
	if err != nil {
		h.jsonError(w, 500, "failed to list themes")
		return
	}
	if themes == nil {
		themes = []*auth.Theme{}
	}
	h.writeJSON(w, themes, 200)
}

// --- Get single theme ---

func (h *ThemeHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	theme, err := h.themes.GetByID(r.Context(), id)
	if err != nil {
		h.jsonError(w, 404, "theme not found")
		return
	}
	h.writeJSON(w, theme, 200)
}

// --- Create theme (requires auth) ---

type createThemeRequest struct {
	Name     string `json:"name"`
	Bg       string `json:"bg"`
	Surface  string `json:"surface"`
	Surface2 string `json:"surface2"`
	Border   string `json:"border"`
	Text     string `json:"text"`
	TextDim  string `json:"text_dim"`
	Accent   string `json:"accent"`
	Green    string `json:"green"`
	Red      string `json:"red"`
	Yellow   string `json:"yellow"`
	Radius   string `json:"radius"`
	IsPublic *bool  `json:"is_public"`
}

var hexColorRegex = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (h *ThemeHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)
	if userID == "" {
		h.jsonError(w, 401, "authentication required to create themes")
		return
	}

	var req createThemeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, 400, "invalid JSON")
		return
	}

	if req.Name == "" {
		h.jsonError(w, 400, "name is required")
		return
	}
	if !isValidColor(req.Bg) || !isValidColor(req.Surface) || !isValidColor(req.Accent) || !isValidColor(req.Text) {
		h.jsonError(w, 400, "bg, surface, accent, and text must be valid hex colors (#RRGGBB)")
		return
	}

	// Check name uniqueness
	existing, _ := h.themes.GetByName(r.Context(), req.Name)
	if existing != nil {
		h.jsonError(w, 409, "a theme with this name already exists")
		return
	}

	now := time.Now()
	isPublic := true
	if req.IsPublic != nil {
		isPublic = *req.IsPublic
	}

	theme := &auth.Theme{
		ID:        uuid.New().String(),
		Name:      req.Name,
		AuthorID:  userID,
		Bg:        req.Bg,
		Surface:   req.Surface,
		Surface2:  defaultIfEmpty(req.Surface2, req.Surface),
		Border:    defaultIfEmpty(req.Border, "#2d3248"),
		Text:      req.Text,
		TextDim:   defaultIfEmpty(req.TextDim, "#8b92a5"),
		Accent:    req.Accent,
		Green:     defaultIfEmpty(req.Green, "#10b981"),
		Red:       defaultIfEmpty(req.Red, "#ef4444"),
		Yellow:    defaultIfEmpty(req.Yellow, "#f59e0b"),
		Radius:    defaultIfEmpty(req.Radius, "10px"),
		IsPublic:  isPublic,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := h.themes.Create(r.Context(), theme); err != nil {
		h.jsonError(w, 500, "failed to create theme")
		return
	}

	h.writeJSON(w, theme, 201)
}

// --- Update theme (author only) ---

func (h *ThemeHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)
	if userID == "" {
		h.jsonError(w, 401, "authentication required")
		return
	}

	id := r.PathValue("id")
	theme, err := h.themes.GetByID(r.Context(), id)
	if err != nil {
		h.jsonError(w, 404, "theme not found")
		return
	}
	if theme.AuthorID != userID {
		h.jsonError(w, 403, "only the theme author can update it")
		return
	}

	var req createThemeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, 400, "invalid JSON")
		return
	}

	if req.Name != "" {
		theme.Name = req.Name
	}
	if req.Bg != "" {
		theme.Bg = req.Bg
	}
	if req.Surface != "" {
		theme.Surface = req.Surface
	}
	if req.Surface2 != "" {
		theme.Surface2 = req.Surface2
	}
	if req.Border != "" {
		theme.Border = req.Border
	}
	if req.Text != "" {
		theme.Text = req.Text
	}
	if req.TextDim != "" {
		theme.TextDim = req.TextDim
	}
	if req.Accent != "" {
		theme.Accent = req.Accent
	}
	if req.Green != "" {
		theme.Green = req.Green
	}
	if req.Red != "" {
		theme.Red = req.Red
	}
	if req.Yellow != "" {
		theme.Yellow = req.Yellow
	}
	if req.Radius != "" {
		theme.Radius = req.Radius
	}
	if req.IsPublic != nil {
		theme.IsPublic = *req.IsPublic
	}
	theme.UpdatedAt = time.Now()

	if err := h.themes.Update(r.Context(), theme); err != nil {
		h.jsonError(w, 500, "failed to update theme")
		return
	}

	h.writeJSON(w, theme, 200)
}

// --- Delete theme (author only) ---

func (h *ThemeHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)
	if userID == "" {
		h.jsonError(w, 401, "authentication required")
		return
	}

	id := r.PathValue("id")
	theme, err := h.themes.GetByID(r.Context(), id)
	if err != nil {
		h.jsonError(w, 404, "theme not found")
		return
	}
	if theme.AuthorID != userID {
		h.jsonError(w, 403, "only the theme author can delete it")
		return
	}

	if err := h.themes.Delete(r.Context(), id); err != nil {
		h.jsonError(w, 500, "failed to delete theme")
		return
	}

	h.writeJSON(w, map[string]string{"status": "deleted"}, 200)
}

// --- Helpers ---

func (h *ThemeHandler) getUserID(r *http.Request) string {
	// Check X-User-ID (set by auth middleware) or extract from token
	if uid := r.Header.Get("X-User-ID"); uid != "" {
		return uid
	}
	token := extractToken(r)
	if token == "" {
		return ""
	}
	session, err := h.handler.sessions.GetByToken(r.Context(), token)
	if err != nil || session == nil {
		return ""
	}
	return session.UserID
}

func (h *ThemeHandler) writeJSON(w http.ResponseWriter, data any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *ThemeHandler) jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func isValidColor(c string) bool {
	return hexColorRegex.MatchString(c)
}

func defaultIfEmpty(val, fallback string) string {
	if val == "" {
		return fallback
	}
	return val
}
