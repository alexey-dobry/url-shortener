package sessions

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/example/url-shortener/internal/config"
	"github.com/redis/go-redis/v9"
)

type Manager struct {
	rdb *redis.Client
	ttl time.Duration
}

var ErrNotFound = errors.New("session not found")

const (
	sessionTTL    = 24 * time.Hour
	sessionPrefix = "session:"
)

func New(cfg config.Config) (*Manager, error) {
	if cfg.RedisAddr == "" {
		return nil, errors.New("REDIS_ADDR is not set")
	}
	dbNum, err := strconv.Atoi(cfg.RedisDB)
	if err != nil {
		return nil, fmt.Errorf("REDIS_DB must be integer: %w", err)
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       dbNum,
	})

	var pingErr error
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		pingErr = rdb.Ping(ctx).Err()
		cancel()
		if pingErr == nil {
			return &Manager{rdb: rdb, ttl: sessionTTL}, nil
		}
		time.Sleep(time.Second)
	}
	return nil, fmt.Errorf("ping redis: %w", pingErr)
}

func (m *Manager) Create(ctx context.Context, userID int64) (string, error) {
	sid, err := randomID()
	if err != nil {
		return "", err
	}
	if err := m.rdb.Set(ctx, sessionPrefix+sid, userID, m.ttl).Err(); err != nil {
		return "", err
	}
	return sid, nil
}

func (m *Manager) Get(ctx context.Context, sid string) (int64, error) {
	val, err := m.rdb.GetEx(ctx, sessionPrefix+sid, m.ttl).Result()
	if errors.Is(err, redis.Nil) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(val, 10, 64)
}

func (m *Manager) Delete(ctx context.Context, sid string) error {
	return m.rdb.Del(ctx, sessionPrefix+sid).Err()
}

func (m *Manager) Close() error { return m.rdb.Close() }

func (m *Manager) FlushAll(ctx context.Context) error {
	return m.rdb.FlushDB(ctx).Err()
}

func randomID() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
