package posts

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

type recordingPublisher struct{ command PublishCommand }

func (p *recordingPublisher) Publish(_ context.Context, command PublishCommand) error {
	p.command = command
	return nil
}

func TestPublishWorkerDelegatesStableJobMetadata(t *testing.T) {
	recorder := &recordingPublisher{}
	postID := uuid.New()
	worker := NewPublishWorker(recorder, "worker-a")
	if err := worker.Work(context.Background(), &river.Job[PublishArgs]{JobRow: &rivertype.JobRow{ID: 42}, Args: PublishArgs{PostID: postID, StateVersion: 3, Trigger: TriggerRecovery}}); err != nil {
		t.Fatal(err)
	}
	if recorder.command.PostID != postID || recorder.command.JobID != 42 || recorder.command.StateVersion != 3 || recorder.command.Worker != "worker-a" {
		t.Fatalf("command = %+v", recorder.command)
	}
}
