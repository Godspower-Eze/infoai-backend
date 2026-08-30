package posts

import (
	"math/rand/v2"
	"time"
)

type Jitter interface {
	Apply(time.Duration) time.Duration
}

type RetryPolicy struct {
	MaxAttempts int
	Delays      []time.Duration
	Jitter      Jitter
}

type RandomJitter struct{}

func (RandomJitter) Apply(value time.Duration) time.Duration {
	return time.Duration(float64(value) * (0.8 + rand.Float64()*0.4))
}

func DefaultRetryPolicy(jitter Jitter) RetryPolicy {
	return RetryPolicy{MaxAttempts: 3, Delays: []time.Duration{30 * time.Second, 2 * time.Minute}, Jitter: jitter}
}

func (p RetryPolicy) Next(failedAttempts int, now, providerRetryAt time.Time) (time.Time, bool) {
	if failedAttempts >= p.MaxAttempts || failedAttempts < 1 || len(p.Delays) == 0 {
		return time.Time{}, false
	}
	index := failedAttempts - 1
	if index >= len(p.Delays) {
		index = len(p.Delays) - 1
	}
	delay := p.Delays[index]
	if p.Jitter != nil {
		delay = p.Jitter.Apply(delay)
	}
	next := now.Add(delay)
	if providerRetryAt.After(next) {
		next = providerRetryAt
	}
	return next, true
}
