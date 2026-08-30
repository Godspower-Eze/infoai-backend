package posts

import (
	"context"

	"github.com/riverqueue/river"
)

type PublishExecutor interface {
	Publish(context.Context, PublishCommand) error
}
type DeleteExecutor interface {
	Delete(context.Context, DeleteJobCommand) error
}
type CleanupExecutor interface {
	Run(context.Context, StorageCleanupArgs) error
}
type RecoveryExecutor interface {
	Run(context.Context, RecoverArgs) error
}

type PublishWorker struct {
	river.WorkerDefaults[PublishArgs]
	publisher PublishExecutor
	workerID  string
}

func NewPublishWorker(publisher PublishExecutor, workerID string) *PublishWorker {
	return &PublishWorker{publisher: publisher, workerID: workerID}
}

func (w *PublishWorker) Work(ctx context.Context, job *river.Job[PublishArgs]) error {
	return w.publisher.Publish(ctx, PublishCommand{PostID: job.Args.PostID, JobID: job.ID, StateVersion: job.Args.StateVersion, Trigger: job.Args.Trigger, Worker: w.workerID})
}

type DeleteWorker struct {
	river.WorkerDefaults[DeletePostArgs]
	deleter  DeleteExecutor
	workerID string
}

func (w *DeleteWorker) Work(ctx context.Context, job *river.Job[DeletePostArgs]) error {
	return w.deleter.Delete(ctx, DeleteJobCommand{PostID: job.Args.PostID, JobID: job.ID, StateVersion: job.Args.StateVersion, Worker: w.workerID})
}

type CleanupWorker struct {
	river.WorkerDefaults[StorageCleanupArgs]
	cleanup CleanupExecutor
}

func (w *CleanupWorker) Work(ctx context.Context, job *river.Job[StorageCleanupArgs]) error {
	return w.cleanup.Run(ctx, job.Args)
}

type RecoveryWorker struct {
	river.WorkerDefaults[RecoverArgs]
	recovery RecoveryExecutor
}

func (w *RecoveryWorker) Work(ctx context.Context, job *river.Job[RecoverArgs]) error {
	return w.recovery.Run(ctx, job.Args)
}

func RegisterWorkers(workers *river.Workers, publisher PublishExecutor, deleter DeleteExecutor, cleanup CleanupExecutor, recovery RecoveryExecutor, workerID string) {
	river.AddWorker(workers, NewPublishWorker(publisher, workerID))
	if deleter != nil {
		river.AddWorker(workers, &DeleteWorker{deleter: deleter, workerID: workerID})
	}
	if cleanup != nil {
		river.AddWorker(workers, &CleanupWorker{cleanup: cleanup})
	}
	if recovery != nil {
		river.AddWorker(workers, &RecoveryWorker{recovery: recovery})
	}
}
