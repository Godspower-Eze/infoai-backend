package riverqueue

import (
	"database/sql"
	"errors"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
)

const (
	PublishQueue     = "publish"
	MaintenanceQueue = "maintenance"
)

type Config struct {
	PublishWorkers       int
	CleanupWorkers       int
	JobTimeout           time.Duration
	RescueStuckJobsAfter time.Duration
}

func New(db *sql.DB, workers *river.Workers, cfg Config) (*river.Client[*sql.Tx], error) {
	if db == nil {
		return nil, errors.New("River database must not be nil")
	}
	riverConfig, err := buildRiverConfig(workers, cfg)
	if err != nil {
		return nil, err
	}
	return river.NewClient(riverdatabasesql.New(db), riverConfig)
}

func buildRiverConfig(workers *river.Workers, cfg Config) (*river.Config, error) {
	if workers == nil {
		return nil, errors.New("River workers must not be nil")
	}
	if cfg.PublishWorkers <= 0 {
		return nil, errors.New("River publish workers must be positive")
	}
	if cfg.CleanupWorkers <= 0 {
		return nil, errors.New("River cleanup workers must be positive")
	}
	if cfg.JobTimeout <= 0 {
		return nil, errors.New("River job timeout must be positive")
	}
	if cfg.RescueStuckJobsAfter <= cfg.JobTimeout {
		return nil, errors.New("River stuck-job rescue threshold must exceed job timeout")
	}
	return &river.Config{
		FetchPollInterval:    time.Second,
		JobTimeout:           cfg.JobTimeout,
		PollOnly:             true,
		RescueStuckJobsAfter: cfg.RescueStuckJobsAfter,
		Queues: map[string]river.QueueConfig{
			PublishQueue:     {MaxWorkers: cfg.PublishWorkers},
			MaintenanceQueue: {MaxWorkers: cfg.CleanupWorkers},
		},
		Workers: workers,
	}, nil
}
