package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

type sqliteStore struct {
	db *sql.DB
}

func NewSQLite(path string) (Storage, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return &sqliteStore{db: db}, nil
}

func (s *sqliteStore) ExecRaw(ctx context.Context, sql string) error {
	_, err := s.db.ExecContext(ctx, sql)
	return err
}

func (s *sqliteStore) QueryVersions(ctx context.Context, query string) ([]int, error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *sqliteStore) SaveLink(ctx context.Context, code, url string, ownerID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO links (code, url, owner_id) VALUES (?, ?, ?)`, code, url, ownerID)
	return err
}

func (s *sqliteStore) GetLink(ctx context.Context, code string) (Link, error) {
	var l Link
	err := s.db.QueryRowContext(ctx,
		`SELECT code, url, owner_id, clicks, created_at FROM links WHERE code = ?`, code,
	).Scan(&l.Code, &l.URL, &l.OwnerID, &l.Clicks, &l.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Link{}, ErrNotFound
	}
	return l, err
}

func (s *sqliteStore) IncrementClicks(ctx context.Context, code string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `UPDATE links SET clicks = clicks + 1 WHERE code = ?`, code)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrNotFound
	}

	var clicks int64
	if err := tx.QueryRowContext(ctx,
		`SELECT clicks FROM links WHERE code = ?`, code).Scan(&clicks); err != nil {
		return 0, err
	}
	return clicks, tx.Commit()
}

func (s *sqliteStore) ListLinksByOwner(ctx context.Context, ownerID int64) ([]Link, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT code, url, owner_id, clicks, created_at FROM links
		 WHERE owner_id = ? ORDER BY created_at DESC LIMIT 100`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Link
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.Code, &l.URL, &l.OwnerID, &l.Clicks, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *sqliteStore) CreateUser(ctx context.Context, username, passwordHash string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash) VALUES (?, ?)`, username, passwordHash)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, ErrAlreadyExists
		}
		return 0, err
	}
	return res.LastInsertId()
}

func (s *sqliteStore) GetUserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash FROM users WHERE username = ?`, username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *sqliteStore) Close() error { return s.db.Close() }
