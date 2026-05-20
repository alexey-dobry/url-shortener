package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/example/url-shortener/internal/logger"
)

const HeaderRequestID = "X-Request-ID"

func ReadinessGuard(shuttingDown *atomic.Bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shuttingDown.Load() && !isProbe(r.URL.Path) {
				http.Error(w, "shutting down", http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isProbe(p string) bool {
	return p == "/healthz" || p == "/ready" || strings.HasPrefix(p, "/healthz?")
}

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get(HeaderRequestID)
		if rid == "" {
			rid = newID()
		}
		w.Header().Set(HeaderRequestID, rid)

		log := logger.FromContext(r.Context()).With("request_id", rid)
		ctx := logger.IntoContext(r.Context(), log)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)

		log := logger.FromContext(r.Context())
		log.LogAttrs(r.Context(), levelForStatus(rw.status),
			"http_request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rw.status),
			slog.String("remote", r.RemoteAddr),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		)
	})
}

func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.FromContext(r.Context()).Error("panic recovered",
					slog.Any("panic", rec),
					slog.String("path", r.URL.Path),
				)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func levelForStatus(status int) slog.Level {
	if status >= 500 {
		return slog.LevelError
	}
	return slog.LevelInfo
}
