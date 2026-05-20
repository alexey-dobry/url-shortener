package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/example/url-shortener/internal/config"
	"github.com/example/url-shortener/internal/handlers"
	"github.com/example/url-shortener/internal/logger"
	"github.com/example/url-shortener/internal/middleware"
	"github.com/example/url-shortener/internal/migrate"
	"github.com/example/url-shortener/internal/service"
	"github.com/example/url-shortener/internal/sessions"
	"github.com/example/url-shortener/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

var (
	buildVersion = "dev"
	buildCommit  = "none"
)

const shutdownTimeout = 25 * time.Second

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "server":
		runServer()
	case "migrate":
		runMigrate()
	case "create-admin":
		runCreateAdmin()
	case "clear-cache":
		runClearCache()
	case "healthcheck":
		runHealthcheck()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: app <command>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  server                        Run the HTTP server")
	fmt.Fprintln(os.Stderr, "  migrate                       Apply database migrations and exit")
	fmt.Fprintln(os.Stderr, "  create-admin --email=EMAIL     Create an admin user and exit")
	fmt.Fprintln(os.Stderr, "  clear-cache                   Flush Redis sessions/cache and exit")
	fmt.Fprintln(os.Stderr, "  healthcheck                   Docker health probe")
}

func runServer() {
	cfg := config.Load(buildVersion, buildCommit)
	log := logger.New(getenv("LOG_LEVEL", "info"), cfg.BuildVersion, cfg.Environment)

	hostname, _ := os.Hostname()
	log.Info("starting",
		slog.String("instance", hostname),
		slog.String("version", cfg.BuildVersion),
		slog.String("commit", cfg.BuildCommit),
		slog.String("env", cfg.Environment),
		slog.String("db_driver", cfg.DBDriver),
		slog.String("port", cfg.Port),
	)

	store, err := storage.New(cfg)
	if err != nil {
		log.Error("init storage failed", slog.Any("err", err))
		os.Exit(1)
	}

	sess, err := sessions.New(cfg)
	if err != nil {
		log.Error("init sessions failed", slog.Any("err", err))
		_ = store.Close()
		os.Exit(1)
	}

	svc := service.New(store, sess)
	rel := handlers.ReleaseInfo{
		Version:     cfg.BuildVersion,
		Commit:      cfg.BuildCommit,
		Environment: cfg.Environment,
	}
	h := handlers.New(svc, hostname, rel)

	var shuttingDown atomic.Bool
	mux := buildRoutes(h, &shuttingDown)

	var handler http.Handler = mux
	handler = h.AuthMiddleware(handler)
	handler = middleware.AccessLog(handler)
	handler = middleware.Recover(handler)
	handler = middleware.ReadinessGuard(&shuttingDown)(handler)
	handler = middleware.RequestID(handler)

	baseCtx := logger.IntoContext(context.Background(), log)
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
		BaseContext:  func(_ net.Listener) context.Context { return baseCtx },
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Info("listening", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		if err != nil {
			log.Error("server failed", slog.Any("err", err))
			gracefulClose(log, store, sess)
			os.Exit(1)
		}
	case sig := <-stop:
		log.Info("signal received, starting graceful shutdown",
			slog.String("signal", sig.String()),
		)
	}

	shuttingDown.Store(true)
	time.Sleep(1 * time.Second)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown error", slog.Any("err", err))
	} else {
		log.Info("http server stopped")
	}

	gracefulClose(log, store, sess)
	log.Info("bye")
}

func gracefulClose(log *slog.Logger, store storage.Storage, sess *sessions.Manager) {
	if err := sess.Close(); err != nil {
		log.Error("sessions close error", slog.Any("err", err))
	}
	if err := store.Close(); err != nil {
		log.Error("storage close error", slog.Any("err", err))
	}
}

func buildRoutes(h *handlers.Handlers, shuttingDown *atomic.Bool) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.Health)
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		if shuttingDown.Load() {
			http.Error(w, "shutting down", http.StatusServiceUnavailable)
			return
		}
		h.Health(w, r)
	})
	mux.HandleFunc("GET /info", h.Info)
	mux.HandleFunc("POST /auth/register", h.Register)
	mux.HandleFunc("POST /auth/login", h.Login)
	mux.HandleFunc("POST /auth/logout", h.Logout)
	mux.HandleFunc("GET /auth/me", h.Me)
	mux.HandleFunc("POST /shorten", h.RequireAuth(h.Shorten))
	mux.HandleFunc("GET /links", h.RequireAuth(h.MyLinks))
	mux.HandleFunc("GET /stats/{code}", h.Stats)
	mux.HandleFunc("GET /{code}", h.Redirect)
	return mux
}

func runMigrate() {
	cfg := config.Load(buildVersion, buildCommit)
	log := logger.New(getenv("LOG_LEVEL", "info"), cfg.BuildVersion, cfg.Environment)

	log.Info("running migrations",
		slog.String("version", cfg.BuildVersion),
		slog.String("env", cfg.Environment),
		slog.String("db_driver", cfg.DBDriver),
	)

	store, err := storage.New(cfg)
	if err != nil {
		log.Error("init storage failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer store.Close()

	if err := migrate.Up(context.Background(), store, log); err != nil {
		log.Error("migration failed", slog.Any("err", err))
		os.Exit(1)
	}

	log.Info("migrations applied successfully")
}

func runCreateAdmin() {
	email := ""
	for _, arg := range os.Args[2:] {
		if len(arg) > 8 && arg[:8] == "--email=" {
			email = arg[8:]
		}
	}
	if email == "" {
		fmt.Fprintln(os.Stderr, "Usage: app create-admin --email=admin@example.com")
		os.Exit(1)
	}

	cfg := config.Load(buildVersion, buildCommit)
	log := logger.New(getenv("LOG_LEVEL", "info"), cfg.BuildVersion, cfg.Environment)

	store, err := storage.New(cfg)
	if err != nil {
		log.Error("init storage failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer store.Close()

	password := "admin-" + fmt.Sprintf("%d", time.Now().UnixNano()%100000)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Error("hash password failed", slog.Any("err", err))
		os.Exit(1)
	}

	id, err := store.CreateUser(context.Background(), email, string(hash))
	if err != nil {
		log.Error("create admin failed", slog.Any("err", err), slog.String("email", email))
		os.Exit(1)
	}

	log.Info("admin created",
		slog.Int64("user_id", id),
		slog.String("email", email),
	)
	fmt.Printf("Admin created: id=%d email=%s password=%s\n", id, email, password)
}

func runClearCache() {
	cfg := config.Load(buildVersion, buildCommit)
	log := logger.New(getenv("LOG_LEVEL", "info"), cfg.BuildVersion, cfg.Environment)

	sess, err := sessions.New(cfg)
	if err != nil {
		log.Error("init sessions failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer sess.Close()

	if err := sess.FlushAll(context.Background()); err != nil {
		log.Error("clear cache failed", slog.Any("err", err))
		os.Exit(1)
	}

	log.Info("cache cleared")
}

func runHealthcheck() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/ready")
	if err != nil {
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
