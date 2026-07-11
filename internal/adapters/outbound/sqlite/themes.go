package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/rafaribe/beagrid/internal/auth"
)

// ThemeStoreAdapter implements auth.ThemeStore.
type ThemeStoreAdapter struct{ db *sql.DB }

func (a *ThemeStoreAdapter) Create(ctx context.Context, t *auth.Theme) error {
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO themes (id, name, author_id, bg, surface, surface2, border, text_color, text_dim, accent, green, red, yellow, radius, is_public, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.AuthorID, t.Bg, t.Surface, t.Surface2, t.Border,
		t.Text, t.TextDim, t.Accent, t.Green, t.Red, t.Yellow, t.Radius,
		boolToInt(t.IsPublic), t.CreatedAt.Format(time.RFC3339), t.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (a *ThemeStoreAdapter) GetByID(ctx context.Context, id string) (*auth.Theme, error) {
	return a.scanTheme(a.db.QueryRowContext(ctx, `SELECT * FROM themes WHERE id = ?`, id))
}

func (a *ThemeStoreAdapter) GetByName(ctx context.Context, name string) (*auth.Theme, error) {
	return a.scanTheme(a.db.QueryRowContext(ctx, `SELECT * FROM themes WHERE name = ?`, name))
}

func (a *ThemeStoreAdapter) Update(ctx context.Context, t *auth.Theme) error {
	_, err := a.db.ExecContext(ctx, `
		UPDATE themes SET name=?, bg=?, surface=?, surface2=?, border=?, text_color=?, text_dim=?, accent=?, green=?, red=?, yellow=?, radius=?, is_public=?, updated_at=?
		WHERE id=?`,
		t.Name, t.Bg, t.Surface, t.Surface2, t.Border,
		t.Text, t.TextDim, t.Accent, t.Green, t.Red, t.Yellow, t.Radius,
		boolToInt(t.IsPublic), t.UpdatedAt.Format(time.RFC3339), t.ID,
	)
	return err
}

func (a *ThemeStoreAdapter) Delete(ctx context.Context, id string) error {
	_, err := a.db.ExecContext(ctx, `DELETE FROM themes WHERE id = ?`, id)
	return err
}

func (a *ThemeStoreAdapter) List(ctx context.Context) ([]*auth.Theme, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT * FROM themes WHERE is_public = 1 ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return a.scanThemes(rows)
}

func (a *ThemeStoreAdapter) ListByAuthor(ctx context.Context, authorID string) ([]*auth.Theme, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT * FROM themes WHERE author_id = ? ORDER BY name ASC`, authorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return a.scanThemes(rows)
}

func (a *ThemeStoreAdapter) scanTheme(row *sql.Row) (*auth.Theme, error) {
	var t auth.Theme
	var isPublic int
	var createdAt, updatedAt string
	err := row.Scan(&t.ID, &t.Name, &t.AuthorID, &t.Bg, &t.Surface, &t.Surface2,
		&t.Border, &t.Text, &t.TextDim, &t.Accent, &t.Green, &t.Red, &t.Yellow,
		&t.Radius, &isPublic, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	t.IsPublic = isPublic == 1
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &t, nil
}

func (a *ThemeStoreAdapter) scanThemes(rows *sql.Rows) ([]*auth.Theme, error) {
	var themes []*auth.Theme
	for rows.Next() {
		var t auth.Theme
		var isPublic int
		var createdAt, updatedAt string
		err := rows.Scan(&t.ID, &t.Name, &t.AuthorID, &t.Bg, &t.Surface, &t.Surface2,
			&t.Border, &t.Text, &t.TextDim, &t.Accent, &t.Green, &t.Red, &t.Yellow,
			&t.Radius, &isPublic, &createdAt, &updatedAt)
		if err != nil {
			return nil, err
		}
		t.IsPublic = isPublic == 1
		t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		themes = append(themes, &t)
	}
	return themes, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
