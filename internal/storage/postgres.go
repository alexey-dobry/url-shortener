package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/example/url-shortener/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgStore struct {
	pool *pgxpool.Pool
}

func NewPostgres(cfg config.Config) (Storage, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSSLMode,
	)

	var pool *pgxpool.Pool
	var err error
	for i := 0; i < 10; i++ {
		pool, err = pgxpool.New(context.Background(), dsn)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err = pool.Ping(ctx)
			cancel()
			if err == nil {
				return &pgStore{pool: pool}, nil
			}
			pool.Close()
		}
		time.Sleep(time.Second)
	}
	return nil, fmt.Errorf("connect postgres: %w", err)
}

func (s *pgStore) ExecRaw(ctx context.Context, sql string) error {
	_, err := s.pool.Exec(ctx, sql)
	return err
}

func (s *pgStore) QueryVersions(ctx context.Context, sql string) ([]int, error) {
	rows, err := s.pool.Query(ctx, sql)
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

func (s *pgStore) SaveLink(ctx context.Context, code, url string, ownerID int64) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO links (code, url, owner_id) VALUES ($1, $2, $3)`, code, url, ownerID)
	return err
}

func (s *pgStore) GetLink(ctx context.Context, code string) (Link, error) {
	var l Link
	var created time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT code, url, owner_id, clicks, created_at FROM links WHERE code = $1`, code,
	).Scan(&l.Code, &l.URL, &l.OwnerID, &l.Clicks, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		return Link{}, ErrNotFound
	}
	l.CreatedAt = created.Format(time.RFC3339)
	return l, err
}

func (s *pgStore) IncrementClicks(ctx context.Context, code string) (int64, error) {
	var clicks int64
	err := s.pool.QueryRow(ctx,
		`UPDATE links SET clicks = clicks + 1 WHERE code = $1 RETURNING clicks`, code,
	).Scan(&clicks)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return clicks, err
}

func (s *pgStore) ListLinksByOwner(ctx context.Context, ownerID int64) ([]Link, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT code, url, owner_id, clicks, created_at FROM links
		 WHERE owner_id = $1 ORDER BY created_at DESC LIMIT 100`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Link
	for rows.Next() {
		var l Link
		var created time.Time
		if err := rows.Scan(&l.Code, &l.URL, &l.OwnerID, &l.Clicks, &created); err != nil {
			return nil, err
		}
		l.CreatedAt = created.Format(time.RFC3339)
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *pgStore) CreateUser(ctx context.Context, username, passwordHash string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id`,
		username, passwordHash,
	).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return 0, ErrAlreadyExists
		}
		return 0, err
	}
	return id, nil
}

func (s *pgStore) GetUserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, password_hash FROM users WHERE username = $1`, username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *pgStore) Close() error {
	s.pool.Close()
	return nil
}
