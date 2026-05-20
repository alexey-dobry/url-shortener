package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/url-shortener/internal/logger"
	"github.com/example/url-shortener/internal/service"
)

const cookieName = "sid"

type Handlers struct {
	svc      *service.Service
	hostname string
	release  ReleaseInfo
}

type ReleaseInfo struct {
	Version     string `json:"version"`
	Commit      string `json:"commit"`
	Environment string `json:"environment"`
}

func New(svc *service.Service, hostname string, rel ReleaseInfo) *Handlers {
	return &Handlers{svc: svc, hostname: hostname, release: rel}
}

type ctxKey int

const userCtxKey ctxKey = 1

func (h *Handlers) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
			if uid, ok := h.svc.ResolveSession(r.Context(), c.Value); ok {
				ctx := context.WithValue(r.Context(), userCtxKey, uid)
				log := logger.FromContext(ctx).With(slog.Int64("user_id", uid))
				ctx = logger.IntoContext(ctx, log)
				r = r.WithContext(ctx)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handlers) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := currentUserID(r); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func currentUserID(r *http.Request) (int64, bool) {
	v := r.Context().Value(userCtxKey)
	id, ok := v.(int64)
	return id, ok
}

func (h *Handlers) Info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"release":  h.release,
		"instance": h.hostname,
		"time":     time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "ok",
		"instance": h.hostname,
	})
}

type authRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handlers) Register(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	err := h.svc.Register(r.Context(), req.Username, req.Password)
	switch {
	case errors.Is(err, service.ErrValidation):
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, service.ErrUserExists):
		http.Error(w, "username already exists", http.StatusConflict)
		return
	case err != nil:
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"status":"registered"}`))
}

func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	sid, user, err := h.svc.Login(r.Context(), req.Username, req.Password)
	switch {
	case errors.Is(err, service.ErrInvalidCredentials):
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	case err != nil:
		logger.FromContext(r.Context()).Error("login failed", slog.Any("err", err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   24 * 60 * 60,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"username":  user.Username,
		"served_by": h.hostname,
	})
}

func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		_ = h.svc.Logout(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	uid, _ := currentUserID(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":   uid,
		"served_by": h.hostname,
	})
}

type shortenRequest struct {
	URL string `json:"url"`
}

func (h *Handlers) Shorten(w http.ResponseWriter, r *http.Request) {
	uid, _ := currentUserID(r)
	var req shortenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	link, err := h.svc.Shorten(r.Context(), uid, req.URL)
	switch {
	case errors.Is(err, service.ErrInvalidURL):
		http.Error(w, "invalid url", http.StatusBadRequest)
		return
	case err != nil:
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"code":      link.Code,
		"short_url": scheme + "://" + r.Host + "/" + link.Code,
		"served_by": h.hostname,
	})
}

func (h *Handlers) MyLinks(w http.ResponseWriter, r *http.Request) {
	uid, _ := currentUserID(r)
	links, err := h.svc.ListMyLinks(r.Context(), uid)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"links":     links,
		"served_by": h.hostname,
	})
}

func (h *Handlers) Redirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	targetURL, err := h.svc.Resolve(r.Context(), code)
	switch {
	case errors.Is(err, service.ErrNotFound):
		http.NotFound(w, r)
		return
	case err != nil:
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("X-Served-By", h.hostname)
	http.Redirect(w, r, targetURL, http.StatusFound)
}

func (h *Handlers) Stats(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	link, err := h.svc.Stats(r.Context(), code)
	switch {
	case errors.Is(err, service.ErrNotFound):
		http.NotFound(w, r)
		return
	case err != nil:
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code":       link.Code,
		"url":        link.URL,
		"clicks":     link.Clicks,
		"created_at": link.CreatedAt,
		"served_by":  h.hostname,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
