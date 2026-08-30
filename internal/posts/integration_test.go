package posts

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Godspower-Eze/infoai-backend/internal/platform/database"
	"github.com/Godspower-Eze/infoai-backend/internal/platform/riverqueue"
	"github.com/riverqueue/river"
)

func TestIntegrationPublishAndCancelShareDurableStateWithRiver(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	connections, err := database.Open(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connections.Close() })
	owner := createPostTestOwner(t, connections.EntClient)
	account := createPostTestAccount(t, connections.EntClient, owner.ID, "None")
	repository := NewEntPostRepository(connections.EntClient)
	created, err := repository.Create(context.Background(), CreateCommand{OwnerID: owner.ID, XAccountID: account.ID, CreationMode: CreationModeUser, Items: []ItemInput{{Text: "durable"}}})
	if err != nil {
		t.Fatal(err)
	}
	workers := river.NewWorkers()
	client, err := riverqueue.New(connections.SQL, workers, riverqueue.Config{PublishWorkers: 1, CleanupWorkers: 1, JobTimeout: time.Minute, RescueStuckJobsAfter: 2 * time.Minute, SkipUnknownJobCheck: true})
	if err != nil {
		t.Fatal(err)
	}
	publication := NewEntPublicationRepository(connections.SQL, NewRiverQueue(client))
	service := NewServiceWithPublishing(PublishingDependencies{
		Posts:              repository,
		Publication:        publication,
		Outcomes:           publication,
		PublishedDeletions: publication,
		Retries:            publication,
	}, DefaultTextPolicy(), DefaultMediaPolicy(), nil)

	scheduledAt := time.Now().Add(time.Hour).UTC()
	scheduled, err := service.Schedule(context.Background(), owner.ID, created.ID, scheduledAt)
	if err != nil {
		t.Fatal(err)
	}
	if scheduled.Status != StatusScheduled || scheduled.ActiveJobID == nil || scheduled.StateVersion != 1 {
		t.Fatalf("scheduled = %+v", scheduled)
	}
	jobID := *scheduled.ActiveJobID
	t.Cleanup(func() {
		_, _ = connections.SQL.ExecContext(context.Background(), `DELETE FROM river_job WHERE id = $1`, jobID)
	})
	var storedKind string
	if err := connections.SQL.QueryRowContext(context.Background(), `SELECT kind FROM river_job WHERE id = $1`, jobID).Scan(&storedKind); err != nil {
		t.Fatal(err)
	}
	if storedKind != (PublishArgs{}).Kind() {
		t.Fatalf("job kind = %q", storedKind)
	}

	cancelled, err := service.CancelSchedule(context.Background(), owner.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != StatusDraft || cancelled.ActiveJobID != nil || cancelled.StateVersion != 2 {
		t.Fatalf("cancelled = %+v", cancelled)
	}
}
