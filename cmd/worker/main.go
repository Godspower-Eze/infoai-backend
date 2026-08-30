package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	xintegration "github.com/Godspower-Eze/infoai-backend/internal/integrations/x"
	"github.com/Godspower-Eze/infoai-backend/internal/platform/config"
	"github.com/Godspower-Eze/infoai-backend/internal/platform/database"
	"github.com/Godspower-Eze/infoai-backend/internal/platform/errorreporting"
	"github.com/Godspower-Eze/infoai-backend/internal/platform/mediastorage"
	"github.com/Godspower-Eze/infoai-backend/internal/platform/riverqueue"
	"github.com/Godspower-Eze/infoai-backend/internal/posts"
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
	mediaStorage, err := mediastorage.NewLocal(cfg.MediaStorageRoot)
	if err != nil {
		return err
	}
	encryptor, err := xintegration.NewAESGCMEncryptor(cfg.TokenEncryptionKey)
	if err != nil {
		return err
	}
	xClient := xintegration.NewXClient(cfg.XClientID, cfg.XClientSecret, cfg.XCallbackURL, nil, xintegration.DefaultXEndpoints())
	tokenService := xintegration.NewService(xClient, xintegration.NewEntAccountRepository(connections.EntClient), encryptor, nil, nil)
	postRepository := posts.NewEntPostRepository(connections.EntClient)
	publisher := posts.NewPublisher(postRepository, tokenService, xClient, mediaStorage, posts.DefaultRetryPolicy(posts.RandomJitter{}), reporter)
	deleter := posts.NewDeleter(postRepository, tokenService, xClient, reporter)
	cleanup := posts.NewCleanup(postRepository, mediaStorage)

	workers := river.NewWorkers()
	queue := posts.NewRiverQueue(nil)
	recovery := posts.NewRecovery(connections.SQL, queue)
	workerID, _ := os.Hostname()
	posts.RegisterWorkers(workers, publisher, deleter, cleanup, recovery, workerID)
	client, err := riverqueue.New(connections.SQL, workers, workerRiverConfig())
	if err != nil {
		return err
	}
	queue.SetClient(client)
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
	args := posts.RecoverArgs{}
	opts := args.InsertOpts()
	return riverqueue.Config{
		PublishWorkers:       4,
		CleanupWorkers:       1,
		JobTimeout:           2 * time.Minute,
		RescueStuckJobsAfter: 3 * time.Minute,
		PeriodicJobs:         []*river.PeriodicJob{river.NewPeriodicJob(river.PeriodicInterval(time.Minute), func() (river.JobArgs, *river.InsertOpts) { return args, &opts }, &river.PeriodicJobOpts{ID: "recover_posts", RunOnStart: true})},
	}
}
