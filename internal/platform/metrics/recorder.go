package metrics

import (
	"context"
	"time"
)

type PublicationMeasurement struct {
	Outcome   string
	Duration  time.Duration
	Retry     bool
	Ambiguous bool
	Partial   bool
}

type MediaMeasurement struct {
	Category string
	Outcome  string
	Duration time.Duration
}

type Recorder interface {
	PublicationFinished(context.Context, PublicationMeasurement)
	QueueLatency(context.Context, time.Duration)
	LeaseExpired(context.Context, string)
	MediaProcessingFinished(context.Context, MediaMeasurement)
	CleanupBacklog(context.Context, int)
}
