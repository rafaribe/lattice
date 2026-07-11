package auth

import (
	"context"
	"time"
)

// Theme represents a user-created UI theme with CSS custom properties.
type Theme struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	AuthorID  string    `json:"author_id"`
	Bg        string    `json:"bg"`
	Surface   string    `json:"surface"`
	Surface2  string    `json:"surface2"`
	Border    string    `json:"border"`
	Text      string    `json:"text"`
	TextDim   string    `json:"text_dim"`
	Accent    string    `json:"accent"`
	Green     string    `json:"green"`
	Red       string    `json:"red"`
	Yellow    string    `json:"yellow"`
	Radius    string    `json:"radius"`
	IsPublic  bool      `json:"is_public"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ThemeStore is the outbound port for theme persistence.
type ThemeStore interface {
	Create(ctx context.Context, theme *Theme) error
	GetByID(ctx context.Context, id string) (*Theme, error)
	GetByName(ctx context.Context, name string) (*Theme, error)
	Update(ctx context.Context, theme *Theme) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]*Theme, error)           // all public themes
	ListByAuthor(ctx context.Context, authorID string) ([]*Theme, error)
}
