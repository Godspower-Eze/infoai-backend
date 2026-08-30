package main

import (
	"testing"
	"time"
)

func TestWorkerRiverDefaultsKeepRescueBeyondJobTimeout(t *testing.T) {
	cfg := workerRiverConfig()
	if cfg.PublishWorkers != 4 || cfg.CleanupWorkers != 1 {
		t.Fatalf("worker counts = (%d, %d), want (4, 1)", cfg.PublishWorkers, cfg.CleanupWorkers)
	}
	if cfg.JobTimeout != 2*time.Minute {
		t.Fatalf("JobTimeout = %s, want 2m", cfg.JobTimeout)
	}
	if cfg.RescueStuckJobsAfter != 3*time.Minute {
		t.Fatalf("RescueStuckJobsAfter = %s, want 3m", cfg.RescueStuckJobsAfter)
	}
	if cfg.RescueStuckJobsAfter <= cfg.JobTimeout {
		t.Fatal("stuck-job rescue must occur after the job timeout")
	}
}
