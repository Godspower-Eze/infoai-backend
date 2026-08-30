package posts

import (
	"time"

	"github.com/Godspower-Eze/infoai-backend/internal/platform/riverqueue"
	"github.com/google/uuid"
	"github.com/riverqueue/river"
)

type PublishTrigger string

const (
	TriggerImmediate PublishTrigger = "immediate"
	TriggerScheduled PublishTrigger = "scheduled"
	TriggerRetry     PublishTrigger = "retry"
	TriggerRecovery  PublishTrigger = "recovery"
)

type PublishArgs struct {
	PostID       uuid.UUID      `json:"post_id"`
	Trigger      PublishTrigger `json:"trigger"`
	StateVersion int64          `json:"state_version"`
	ScheduledAt  *time.Time     `json:"scheduled_at,omitempty"`
}

func (PublishArgs) Kind() string { return "publish_post" }
func (a PublishArgs) InsertOpts() river.InsertOpts {
	opts := river.InsertOpts{Queue: riverqueue.PublishQueue, MaxAttempts: 1}
	if a.ScheduledAt != nil {
		opts.ScheduledAt = *a.ScheduledAt
	}
	return opts
}

type DeletePostArgs struct {
	PostID       uuid.UUID `json:"post_id"`
	StateVersion int64     `json:"state_version"`
}

func (DeletePostArgs) Kind() string { return "delete_post" }
func (DeletePostArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: riverqueue.MaintenanceQueue, MaxAttempts: 1}
}

type RecoverArgs struct{}

func (RecoverArgs) Kind() string { return "recover_posts" }
func (RecoverArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: riverqueue.MaintenanceQueue, MaxAttempts: 1}
}

type StorageCleanupArgs struct {
	StorageKey string `json:"storage_key,omitempty"`
}

func (StorageCleanupArgs) Kind() string { return "cleanup_storage" }
func (StorageCleanupArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: riverqueue.MaintenanceQueue, MaxAttempts: 1}
}
