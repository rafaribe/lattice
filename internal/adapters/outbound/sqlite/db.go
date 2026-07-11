// Package sqlite provides the SQLite outbound adapter for user and session persistence.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/rafaribe/beagrid/internal/auth"
)

// DB is the SQLite database connection.
type DB struct {
	db *sql.DB
}

// New opens (or creates) a SQLite database at the given path and runs migrations.
func New(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening sqlite %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrating sqlite: %w", err)
	}
	return &DB{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id           TEXT PRIMARY KEY,
			email        TEXT UNIQUE NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL DEFAULT '',
			provider     TEXT NOT NULL DEFAULT 'local',
			provider_id  TEXT NOT NULL DEFAULT '',
			role         TEXT NOT NULL DEFAULT 'user',
			created_at   TEXT NOT NULL,
			updated_at   TEXT NOT NULL,
			last_login_at TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
		CREATE INDEX IF NOT EXISTS idx_users_provider ON users(provider, provider_id);

		CREATE TABLE IF NOT EXISTS sessions (
			id         TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token      TEXT UNIQUE NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token);
		CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
		CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);

		CREATE TABLE IF NOT EXISTS themes (
			id          TEXT PRIMARY KEY,
			name        TEXT UNIQUE NOT NULL,
			author_id   TEXT NOT NULL DEFAULT '',
			bg          TEXT NOT NULL,
			surface     TEXT NOT NULL,
			surface2    TEXT NOT NULL,
			border      TEXT NOT NULL,
			text_color  TEXT NOT NULL,
			text_dim    TEXT NOT NULL,
			accent      TEXT NOT NULL,
			green       TEXT NOT NULL DEFAULT '#10b981',
			red         TEXT NOT NULL DEFAULT '#ef4444',
			yellow      TEXT NOT NULL DEFAULT '#f59e0b',
			radius      TEXT NOT NULL DEFAULT '10px',
			is_public   INTEGER NOT NULL DEFAULT 1,
			created_at  TEXT NOT NULL,
			updated_at  TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_themes_name ON themes(name);
		CREATE INDEX IF NOT EXISTS idx_themes_author ON themes(author_id);
	`)
	return err
}

// Close closes the database connection.
func (d *DB) Close() error { return d.db.Close() }

// Users returns the UserStore adapter.
func (d *DB) Users() *UserStoreAdapter { return &UserStoreAdapter{db: d.db} }

// Sessions returns the SessionStore adapter.
func (d *DB) Sessions() *SessionStoreAdapter { return &SessionStoreAdapter{db: d.db} }

// Themes returns the ThemeStore adapter.
func (d *DB) Themes() *ThemeStoreAdapter { return &ThemeStoreAdapter{db: d.db} }

// --- UserStoreAdapter ---

// UserStoreAdapter implements auth.UserStore.
type UserStoreAdapter struct{ db *sql.DB }

func (a *UserStoreAdapter) Create(ctx context.Context, u *auth.User) error {
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO users (id, email, display_name, password_hash, provider, provider_id, role, created_at, updated_at, last_login_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Email, u.DisplayName, u.PasswordHash,
		string(u.Provider), u.ProviderID, string(u.Role),
		u.CreatedAt.Format(time.RFC3339), u.UpdatedAt.Format(time.RFC3339), u.LastLoginAt.Format(time.RFC3339),
	)
	return err
}

func (a *UserStoreAdapter) GetByID(ctx context.Context, id string) (*auth.User, error) {
	return scanUser(a.db.QueryRowContext(ctx, `SELECT * FROM users WHERE id = ?`, id))
}

func (a *UserStoreAdapter) GetByEmail(ctx context.Context, email string) (*auth.User, error) {
	return scanUser(a.db.QueryRowContext(ctx, `SELECT * FROM users WHERE email = ?`, email))
}

func (a *UserStoreAdapter) GetByProviderID(ctx context.Context, provider auth.Provider, providerID string) (*auth.User, error) {
	return scanUser(a.db.QueryRowContext(ctx, `SELECT * FROM users WHERE provider = ? AND provider_id = ?`, string(provider), providerID))
}

func (a *UserStoreAdapter) Update(ctx context.Context, u *auth.User) error {
	_, err := a.db.ExecContext(ctx, `
		UPDATE users SET email=?, display_name=?, password_hash=?, provider=?, provider_id=?, role=?, updated_at=?, last_login_at=?
		WHERE id=?`,
		u.Email, u.DisplayName, u.PasswordHash, string(u.Provider), u.ProviderID, string(u.Role),
		u.UpdatedAt.Format(time.RFC3339), u.LastLoginAt.Format(time.RFC3339), u.ID,
	)
	return err
}

func (a *UserStoreAdapter) Delete(ctx context.Context, id string) error {
	_, err := a.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

func (a *UserStoreAdapter) List(ctx context.Context, limit, offset int) ([]*auth.User, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT * FROM users ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []*auth.User
	for rows.Next() {
		u, err := scanUserRows(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (a *UserStoreAdapter) Count(ctx context.Context) (int, error) {
	var count int
	err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

// --- SessionStoreAdapter ---

// SessionStoreAdapter implements auth.SessionStore.
type SessionStoreAdapter struct{ db *sql.DB }

func (a *SessionStoreAdapter) Create(ctx context.Context, s *auth.Session) error {
	_, err := a.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, token, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		s.ID, s.UserID, s.Token, s.ExpiresAt.Format(time.RFC3339), s.CreatedAt.Format(time.RFC3339),
	)
	return err
}

func (a *SessionStoreAdapter) GetByToken(ctx context.Context, token string) (*auth.Session, error) {
	var s auth.Session
	var expiresAt, createdAt string
	err := a.db.QueryRowContext(ctx, `SELECT id, user_id, token, expires_at, created_at FROM sessions WHERE token = ?`, token).
		Scan(&s.ID, &s.UserID, &s.Token, &expiresAt, &createdAt)
	if err != nil {
		return nil, err
	}
	s.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	s.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &s, nil
}

func (a *SessionStoreAdapter) Delete(ctx context.Context, id string) error {
	_, err := a.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (a *SessionStoreAdapter) DeleteByUserID(ctx context.Context, userID string) error {
	_, err := a.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

func (a *SessionStoreAdapter) DeleteExpired(ctx context.Context) error {
	_, err := a.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, time.Now().Format(time.RFC3339))
	return err
}

// --- scan helpers ---

func scanUser(row *sql.Row) (*auth.User, error) {
	var u auth.User
	var provider, role, createdAt, updatedAt, lastLogin string
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash,
		&provider, &u.ProviderID, &role, &createdAt, &updatedAt, &lastLogin)
	if err != nil {
		return nil, err
	}
	u.Provider = auth.Provider(provider)
	u.Role = auth.Role(role)
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	u.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	u.LastLoginAt, _ = time.Parse(time.RFC3339, lastLogin)
	return &u, nil
}

func scanUserRows(rows *sql.Rows) (*auth.User, error) {
	var u auth.User
	var provider, role, createdAt, updatedAt, lastLogin string
	err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash,
		&provider, &u.ProviderID, &role, &createdAt, &updatedAt, &lastLogin)
	if err != nil {
		return nil, err
	}
	u.Provider = auth.Provider(provider)
	u.Role = auth.Role(role)
	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	u.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	u.LastLoginAt, _ = time.Parse(time.RFC3339, lastLogin)
	return &u, nil
}
