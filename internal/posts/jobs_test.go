package posts

import (
	"testing"
	"time"

	"github.com/Godspower-Eze/infoai-backend/internal/platform/riverqueue"
	"github.com/google/uuid"
)

func TestPublishArgsRouteToPublishQueue(t *testing.T) {
	scheduled := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	args := PublishArgs{PostID: uuid.New(), Trigger: TriggerScheduled, StateVersion: 3, ScheduledAt: &scheduled}
	if args.Kind() != "publish_post" || args.InsertOpts().Queue != riverqueue.PublishQueue || !args.InsertOpts().ScheduledAt.Equal(scheduled) {
		t.Fatalf("args/options = %+v / %+v", args, args.InsertOpts())
	}
}

func TestMaintenanceArgsRouteToMaintenanceQueue(t *testing.T) {
	if (DeletePostArgs{}).InsertOpts().Queue != riverqueue.MaintenanceQueue || (RecoverArgs{}).InsertOpts().Queue != riverqueue.MaintenanceQueue || (StorageCleanupArgs{}).InsertOpts().Queue != riverqueue.MaintenanceQueue {
		t.Fatal("maintenance args use wrong queue")
	}
}
