package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

const Service = "url-shortener"

func New(level string, version, env string) *slog.Logger {
	lvl := parseLevel(level)

	stdoutHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: lvl,
	})
	stderrHandler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	})
	h := &splitHandler{
		nonError: stdoutHandler,
		errOnly:  stderrHandler,
	}

	logger := slog.New(h).With(
		slog.String("service", Service),
		slog.String("version", version),
		slog.String("env", env),
	)

	slog.SetDefault(logger)
	return logger
}

func Discard() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

type splitHandler struct {
	nonError slog.Handler
	errOnly  slog.Handler
}

func (s *splitHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return s.nonError.Enabled(ctx, l) || s.errOnly.Enabled(ctx, l)
}

func (s *splitHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		return s.errOnly.Handle(ctx, r)
	}
	return s.nonError.Handle(ctx, r)
}

func (s *splitHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &splitHandler{
		nonError: s.nonError.WithAttrs(attrs),
		errOnly:  s.errOnly.WithAttrs(attrs),
	}
}

func (s *splitHandler) WithGroup(name string) slog.Handler {
	return &splitHandler{
		nonError: s.nonError.WithGroup(name),
		errOnly:  s.errOnly.WithGroup(name),
	}
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type ctxKey int

const loggerKey ctxKey = 1

func IntoContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}
