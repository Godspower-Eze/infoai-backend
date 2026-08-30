package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Godspower-Eze/infoai-backend/internal/auth"
	"github.com/Godspower-Eze/infoai-backend/internal/httpapi"
	xintegration "github.com/Godspower-Eze/infoai-backend/internal/integrations/x"
	"github.com/Godspower-Eze/infoai-backend/internal/platform/config"
	"github.com/Godspower-Eze/infoai-backend/internal/platform/database"
	"github.com/Godspower-Eze/infoai-backend/internal/platform/errorreporting"
	"github.com/Godspower-Eze/infoai-backend/internal/platform/mediastorage"
	"github.com/Godspower-Eze/infoai-backend/internal/platform/riverqueue"
	"github.com/Godspower-Eze/infoai-backend/internal/posts"
	"github.com/alexedwards/scs/pgxstore"
	"github.com/riverqueue/river"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) (runErr error) {
	if err := config.LoadDotEnv(".env"); err != nil {
		return err
	}
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		return err
	}
	reporter, err := errorreporting.NewFileReporter(errorreporting.FileConfig{
		Path: cfg.ErrorLogPath, MaxBytes: cfg.ErrorLogMaxBytes, RetainedFiles: cfg.ErrorLogRetainedFiles,
	})
	if err != nil {
		return err
	}
	defer func() { runErr = errors.Join(runErr, reporter.Close()) }()

	startupContext, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()
	connections, err := database.Open(startupContext, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer connections.Close()

	sessionStore := pgxstore.New(connections.Pool)
	defer sessionStore.StopCleanup()
	sessions := auth.NewSessionManager(sessionStore, cfg.SessionLifetime, cfg.CookieSecure)
	authService := auth.NewService(auth.NewEntUserRepository(connections.EntClient), auth.NewArgon2Hasher())

	encryptor, err := xintegration.NewAESGCMEncryptor(cfg.TokenEncryptionKey)
	if err != nil {
		return err
	}
	xClient := xintegration.NewXClient(cfg.XClientID, cfg.XClientSecret, cfg.XCallbackURL, nil, xintegration.DefaultXEndpoints())
	xService := xintegration.NewService(
		xClient,
		xintegration.NewEntAccountRepository(connections.EntClient),
		encryptor,
		xintegration.NewOAuthSession(sessions, nil),
		nil,
	)
	mediaStorage, err := mediastorage.NewLocal(cfg.MediaStorageRoot)
	if err != nil {
		return err
	}
	riverClient, err := riverqueue.New(connections.SQL, river.NewWorkers(), apiRiverConfig())
	if err != nil {
		return err
	}
	postRepository := posts.NewEntPostRepository(connections.EntClient)
	publicationRepository := posts.NewEntPublicationRepository(connections.SQL, posts.NewRiverQueue(riverClient))
	postService := posts.NewServiceWithPublishing(posts.PublishingDependencies{
		Posts:              postRepository,
		Publication:        publicationRepository,
		Outcomes:           publicationRepository,
		PublishedDeletions: publicationRepository,
		Retries:            publicationRepository,
	}, posts.DefaultTextPolicy(), posts.DefaultMediaPolicy(), mediaStorage)

	handler, err := httpapi.NewRouter(httpapi.Dependencies{
		Auth:                 authService,
		X:                    xService,
		Posts:                postService,
		Sessions:             sessions,
		Readiness:            connections.Pool,
		FrontendOrigin:       cfg.FrontendOrigin,
		FrontendXRedirectURL: cfg.FrontendXRedirectURL,
		TrustedProxyCIDRs:    cfg.TrustedProxyCIDRs,
	})
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("api listening", "address", cfg.HTTPAddress)
		serverErrors <- server.ListenAndServe()
	}()

	shutdownSignal, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-shutdownSignal.Done():
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil {
		return err
	}
	logger.Info("api stopped gracefully")
	return nil
}

func apiRiverConfig() riverqueue.Config {
	return riverqueue.Config{PublishWorkers: 1, CleanupWorkers: 1, JobTimeout: 2 * time.Minute, RescueStuckJobsAfter: 3 * time.Minute, SkipUnknownJobCheck: true}
}
