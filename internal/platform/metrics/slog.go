package metrics

import (
	"context"
	"log/slog"
	"time"
)

type SlogRecorder struct{ logger *slog.Logger }

func NewSlog(logger *slog.Logger) *SlogRecorder { return &SlogRecorder{logger: logger} }

func (r *SlogRecorder) PublicationFinished(ctx context.Context, value PublicationMeasurement) {
	r.logger.InfoContext(ctx, "metric", "metric", "publication_finished", "outcome", value.Outcome, "duration_ms", value.Duration.Milliseconds(), "retry", value.Retry, "ambiguous", value.Ambiguous, "partial", value.Partial)
}
func (r *SlogRecorder) QueueLatency(ctx context.Context, value time.Duration) {
	r.logger.InfoContext(ctx, "metric", "metric", "queue_latency", "duration_ms", value.Milliseconds())
}
func (r *SlogRecorder) LeaseExpired(ctx context.Context, worker string) {
	r.logger.InfoContext(ctx, "metric", "metric", "lease_expired", "worker", worker)
}
func (r *SlogRecorder) MediaProcessingFinished(ctx context.Context, value MediaMeasurement) {
	r.logger.InfoContext(ctx, "metric", "metric", "media_processing_finished", "category", value.Category, "outcome", value.Outcome, "duration_ms", value.Duration.Milliseconds())
}
func (r *SlogRecorder) CleanupBacklog(ctx context.Context, count int) {
	r.logger.InfoContext(ctx, "metric", "metric", "cleanup_backlog", "count", count)
}

var _ Recorder = (*SlogRecorder)(nil)
