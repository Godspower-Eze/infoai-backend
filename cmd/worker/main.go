package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Godspower-Eze/infoai-backend/internal/platform/config"
	"github.com/Godspower-Eze/infoai-backend/internal/platform/database"
	"github.com/Godspower-Eze/infoai-backend/internal/platform/errorreporting"
	"github.com/Godspower-Eze/infoai-backend/internal/platform/riverqueue"
	"github.com/riverqueue/river"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("worker stopped", "error", err)
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

	workers := river.NewWorkers()
	client, err := riverqueue.New(connections.SQL, workers, workerRiverConfig())
	if err != nil {
		return err
	}
	serviceContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	if err := client.Start(serviceContext); err != nil {
		return err
	}
	logger.Info("worker started")
	<-serviceContext.Done()

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := client.Stop(shutdownContext); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info("worker stopped gracefully")
	return nil
}

func workerRiverConfig() riverqueue.Config {
	return riverqueue.Config{
		PublishWorkers:       4,
		CleanupWorkers:       1,
		JobTimeout:           2 * time.Minute,
		RescueStuckJobsAfter: 3 * time.Minute,
	}
}
