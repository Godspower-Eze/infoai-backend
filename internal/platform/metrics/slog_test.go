package metrics

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestSlogRecorderEmitsAllowlistedPublicationMeasurement(t *testing.T) {
	var output bytes.Buffer
	recorder := NewSlog(slog.New(slog.NewJSONHandler(&output, nil)))
	recorder.PublicationFinished(context.Background(), PublicationMeasurement{
		Outcome: "succeeded", Duration: 2 * time.Second, Retry: true,
	})
	text := output.String()
	for _, expected := range []string{`"metric":"publication_finished"`, `"outcome":"succeeded"`, `"retry":true`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("measurement %s does not contain %s", text, expected)
		}
	}
}
