package posts

import (
	"testing"
	"time"
)

type fixedJitter float64

func (j fixedJitter) Apply(value time.Duration) time.Duration {
	return time.Duration(float64(value) * float64(j))
}

func TestRetryPolicyStopsAfterThreeAttempts(t *testing.T) {
	policy := RetryPolicy{MaxAttempts: 3, Delays: []time.Duration{30 * time.Second, 2 * time.Minute}, Jitter: fixedJitter(1)}
	if _, ok := policy.Next(3, time.Time{}, time.Time{}); ok {
		t.Fatal("third failed attempt must stop")
	}
	next, ok := policy.Next(2, time.Unix(100, 0), time.Time{})
	if !ok || next != time.Unix(100, 0).Add(2*time.Minute) {
		t.Fatalf("next = %v, %v", next, ok)
	}
}

func TestRetryPolicyHonorsLaterProviderReset(t *testing.T) {
	now := time.Unix(100, 0)
	reset := now.Add(10 * time.Minute)
	next, ok := DefaultRetryPolicy(fixedJitter(1)).Next(1, now, reset)
	if !ok || next != reset {
		t.Fatalf("next = %v, %v", next, ok)
	}
}
