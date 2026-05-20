package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/example/url-shortener/internal/logger"
	"github.com/example/url-shortener/internal/sessions"
	"github.com/example/url-shortener/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidURL         = errors.New("invalid url")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserExists         = errors.New("user already exists")
	ErrNotFound           = errors.New("not found")
	ErrValidation         = errors.New("validation failed")
)

type Link struct {
	Code      string `json:"code"`
	URL       string `json:"url"`
	Clicks    int64  `json:"clicks"`
	CreatedAt string `json:"created_at"`
}

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type Service struct {
	store storage.Storage
	sess  *sessions.Manager
}

func New(store storage.Storage, sess *sessions.Manager) *Service {
	return &Service{store: store, sess: sess}
}

func (s *Service) Register(ctx context.Context, username, password string) error {
	if len(username) < 3 || len(password) < 6 {
		return fmt.Errorf("%w: username >=3, password >=6", ErrValidation)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	_, err = s.store.CreateUser(ctx, username, string(hash))
	if errors.Is(err, storage.ErrAlreadyExists) {
		return ErrUserExists
	}
	if err != nil {
		logger.FromContext(ctx).Error("create user failed",
			slog.String("username", username),
			slog.Any("err", err),
		)
		return fmt.Errorf("create user: %w", err)
	}
	logger.FromContext(ctx).Info("user registered", slog.String("username", username))
	return nil
}

func (s *Service) Login(ctx context.Context, username, password string) (sid string, user User, err error) {
	u, err := s.store.GetUserByUsername(ctx, username)
	if errors.Is(err, storage.ErrNotFound) {
		return "", User{}, ErrInvalidCredentials
	}
	if err != nil {
		return "", User{}, fmt.Errorf("get user: %w", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return "", User{}, ErrInvalidCredentials
	}
	sid, err = s.sess.Create(ctx, u.ID)
	if err != nil {
		return "", User{}, fmt.Errorf("create session: %w", err)
	}
	logger.FromContext(ctx).Info("user logged in",
		slog.String("username", u.Username),
		slog.Int64("user_id", u.ID),
	)
	return sid, User{ID: u.ID, Username: u.Username}, nil
}

func (s *Service) Logout(ctx context.Context, sid string) error {
	if sid == "" {
		return nil
	}
	return s.sess.Delete(ctx, sid)
}

func (s *Service) ResolveSession(ctx context.Context, sid string) (int64, bool) {
	if sid == "" {
		return 0, false
	}
	uid, err := s.sess.Get(ctx, sid)
	if err != nil {
		return 0, false
	}
	return uid, true
}

func (s *Service) Shorten(ctx context.Context, ownerID int64, rawURL string) (Link, error) {
	if !isValidURL(rawURL) {
		return Link{}, ErrInvalidURL
	}

	var code string
	for i := 0; i < 5; i++ {
		code = randomCode(7)
		err := s.store.SaveLink(ctx, code, rawURL, ownerID)
		if err == nil {
			break
		}
		if i == 4 {
			logger.FromContext(ctx).Error("save link failed",
				slog.String("url", rawURL),
				slog.Any("err", err),
			)
			return Link{}, fmt.Errorf("save link: %w", err)
		}
	}

	logger.FromContext(ctx).Info("link created",
		slog.String("code", code),
		slog.Int64("owner_id", ownerID),
	)
	return Link{Code: code, URL: rawURL}, nil
}

func (s *Service) Resolve(ctx context.Context, code string) (string, error) {
	link, err := s.store.GetLink(ctx, code)
	if errors.Is(err, storage.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get link: %w", err)
	}
	if _, err := s.store.IncrementClicks(ctx, code); err != nil {
		logger.FromContext(ctx).Error("increment clicks failed",
			slog.String("code", code),
			slog.Any("err", err),
		)
	}
	return link.URL, nil
}

func (s *Service) Stats(ctx context.Context, code string) (Link, error) {
	link, err := s.store.GetLink(ctx, code)
	if errors.Is(err, storage.ErrNotFound) {
		return Link{}, ErrNotFound
	}
	if err != nil {
		return Link{}, fmt.Errorf("get link: %w", err)
	}
	return Link{
		Code:      link.Code,
		URL:       link.URL,
		Clicks:    link.Clicks,
		CreatedAt: link.CreatedAt,
	}, nil
}

func (s *Service) ListMyLinks(ctx context.Context, ownerID int64) ([]Link, error) {
	links, err := s.store.ListLinksByOwner(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}
	out := make([]Link, 0, len(links))
	for _, l := range links {
		out = append(out, Link{
			Code:      l.Code,
			URL:       l.URL,
			Clicks:    l.Clicks,
			CreatedAt: l.CreatedAt,
		})
	}
	return out, nil
}

func randomCode(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return strings.TrimRight(base64.URLEncoding.EncodeToString(b), "=")[:n]
}

func isValidURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
