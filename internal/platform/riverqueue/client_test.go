package riverqueue

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/riverqueue/river"
)

func TestBuildRiverConfigUsesSeparatePollOnlyQueues(t *testing.T) {
	workers := river.NewWorkers()
	cfg, err := buildRiverConfig(workers, Config{
		PublishWorkers:       4,
		CleanupWorkers:       1,
		JobTimeout:           2 * time.Minute,
		RescueStuckJobsAfter: 3 * time.Minute,
	})
	if err != nil {
		t.Fatalf("buildRiverConfig() error = %v", err)
	}
	if !cfg.PollOnly {
		t.Fatal("River must use poll-only mode with riverdatabasesql")
	}
	if cfg.FetchPollInterval != time.Second {
		t.Fatalf("FetchPollInterval = %s, want 1s", cfg.FetchPollInterval)
	}
	if got := cfg.Queues[PublishQueue].MaxWorkers; got != 4 {
		t.Fatalf("publish workers = %d, want 4", got)
	}
	if got := cfg.Queues[MaintenanceQueue].MaxWorkers; got != 1 {
		t.Fatalf("maintenance workers = %d, want 1", got)
	}
	if cfg.JobTimeout != 2*time.Minute || cfg.RescueStuckJobsAfter != 3*time.Minute {
		t.Fatalf("timeouts = (%s, %s)", cfg.JobTimeout, cfg.RescueStuckJobsAfter)
	}
	if cfg.Workers != workers {
		t.Fatal("Workers bundle was not preserved")
	}
}

func TestBuildRiverConfigRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "no publish workers", cfg: Config{CleanupWorkers: 1, JobTimeout: time.Minute, RescueStuckJobsAfter: 2 * time.Minute}},
		{name: "no cleanup workers", cfg: Config{PublishWorkers: 1, JobTimeout: time.Minute, RescueStuckJobsAfter: 2 * time.Minute}},
		{name: "no job timeout", cfg: Config{PublishWorkers: 1, CleanupWorkers: 1, RescueStuckJobsAfter: 2 * time.Minute}},
		{name: "rescue equals timeout", cfg: Config{PublishWorkers: 1, CleanupWorkers: 1, JobTimeout: time.Minute, RescueStuckJobsAfter: time.Minute}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := buildRiverConfig(river.NewWorkers(), test.cfg); err == nil {
				t.Fatal("buildRiverConfig() error = nil")
			}
		})
	}
}

func TestNewReturnsConfiguredRiverClient(t *testing.T) {
	db, err := sql.Open("pgx", "postgres://unused")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	client, err := New(db, river.NewWorkers(), Config{
		PublishWorkers:       1,
		CleanupWorkers:       1,
		JobTimeout:           time.Minute,
		RescueStuckJobsAfter: 2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if client == nil {
		t.Fatal("New() client = nil")
	}
}
