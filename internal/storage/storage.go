package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/example/url-shortener/internal/config"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
)

type Link struct {
	Code      string
	URL       string
	OwnerID   int64
	Clicks    int64
	CreatedAt string
}

type User struct {
	ID           int64
	Username     string
	PasswordHash string
}

type Storage interface {
	SaveLink(ctx context.Context, code, url string, ownerID int64) error
	GetLink(ctx context.Context, code string) (Link, error)
	IncrementClicks(ctx context.Context, code string) (int64, error)
	ListLinksByOwner(ctx context.Context, ownerID int64) ([]Link, error)

	CreateUser(ctx context.Context, username, passwordHash string) (int64, error)
	GetUserByUsername(ctx context.Context, username string) (User, error)

	ExecRaw(ctx context.Context, sql string) error
	QueryVersions(ctx context.Context, sql string) ([]int, error)

	Close() error
}

func New(cfg config.Config) (Storage, error) {
	switch cfg.DBDriver {
	case "sqlite":
		return NewSQLite(cfg.SQLitePath)
	case "postgres":
		return NewPostgres(cfg)
	default:
		return nil, fmt.Errorf("unknown DB_DRIVER: %q (expected 'sqlite' or 'postgres')", cfg.DBDriver)
	}
}
