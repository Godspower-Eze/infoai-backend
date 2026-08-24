package errorreporting

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFileReporterWritesStructuredRedactedEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "errors.jsonl")
	reporter, err := NewFileReporter(FileConfig{Path: path, MaxBytes: 1 << 20, RetainedFiles: 2})
	if err != nil {
		t.Fatal(err)
	}
	reporter.Capture(context.Background(), errors.New("database unavailable"), Event{
		Code: "publication_failed", PostID: "post-1", JobID: 42,
	})
	if err := reporter.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"code":"publication_failed"`, `"post_id":"post-1"`, `"job_id":42`, `"error":"database unavailable"`} {
		if !bytes.Contains(data, []byte(expected)) {
			t.Fatalf("event %s does not contain %s", data, expected)
		}
	}
	if bytes.Contains(data, []byte("token")) || bytes.Count(data, []byte("\n")) != 1 {
		t.Fatalf("unexpected event: %s", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestFileReporterRotatesBeforeExceedingLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "errors.jsonl")
	reporter, err := NewFileReporter(FileConfig{Path: path, MaxBytes: 180, RetainedFiles: 2})
	if err != nil {
		t.Fatal(err)
	}
	for range 4 {
		reporter.Capture(context.Background(), errors.New(strings.Repeat("x", 40)), Event{Code: "failed"})
	}
	if err := reporter.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("rotated file missing: %v", err)
	}
}

func TestFileReporterCloseIsIdempotent(t *testing.T) {
	reporter, err := NewFileReporter(FileConfig{Path: filepath.Join(t.TempDir(), "errors.jsonl"), MaxBytes: 1024, RetainedFiles: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := reporter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reporter.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestFileReporterFlushesPeriodicallyAndSerializesConcurrentCapture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "errors.jsonl")
	reporter, err := NewFileReporter(FileConfig{Path: path, MaxBytes: 1 << 20, RetainedFiles: 1, FlushInterval: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reporter.Close() }()

	var group sync.WaitGroup
	for index := range 10 {
		group.Add(1)
		go func() {
			defer group.Done()
			reporter.Capture(context.Background(), errors.New("failure"), Event{Code: "concurrent", RetryNumber: index})
		}()
	}
	group.Wait()

	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		data, readErr := os.ReadFile(path)
		if readErr == nil && bytes.Count(data, []byte("\n")) == 10 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("periodic flush did not persist ten events: %q, error %v", data, readErr)
		}
		time.Sleep(time.Millisecond)
	}
}
