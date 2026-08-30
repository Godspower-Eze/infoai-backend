package errorreporting

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
)

type FileConfig struct {
	Path          string
	MaxBytes      int64
	RetainedFiles int
	Fallback      io.Writer
	FlushInterval time.Duration
}

type FileReporter struct {
	mu     sync.Mutex
	config FileConfig
	file   *os.File
	writer *bufio.Writer
	closed bool
	size   int64
	stop   chan struct{}
	done   chan struct{}
}

type fileRecord struct {
	Time  time.Time `json:"time"`
	Error string    `json:"error"`
	Stack string    `json:"stack"`
	Event
}

func NewFileReporter(config FileConfig) (*FileReporter, error) {
	if config.Path == "" || config.MaxBytes <= 0 || config.RetainedFiles <= 0 {
		return nil, fmt.Errorf("invalid error reporter configuration")
	}
	if config.Fallback == nil {
		config.Fallback = os.Stderr
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = time.Second
	}
	if err := os.MkdirAll(filepath.Dir(config.Path), 0o700); err != nil {
		return nil, fmt.Errorf("create error log directory: %w", err)
	}
	reporter := &FileReporter{config: config, stop: make(chan struct{}), done: make(chan struct{})}
	if err := reporter.open(); err != nil {
		return nil, err
	}
	go reporter.flushPeriodically()
	return reporter, nil
}

func (r *FileReporter) Capture(_ context.Context, captured error, event Event) {
	record := fileRecord{Time: time.Now().UTC(), Error: captured.Error(), Stack: string(debug.Stack()), Event: event}
	data, err := json.Marshal(record)
	if err != nil {
		r.fallback(err)
		return
	}
	data = append(data, '\n')

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		r.fallback(fmt.Errorf("error reporter is closed"))
		return
	}
	if err := r.rotateIfNeeded(int64(len(data))); err != nil {
		r.fallback(err)
		return
	}
	if _, err := r.writer.Write(data); err != nil {
		r.fallback(err)
		return
	}
	r.size += int64(len(data))
}

func (r *FileReporter) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	close(r.stop)
	flushErr := r.writer.Flush()
	closeErr := r.file.Close()
	r.mu.Unlock()
	<-r.done
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}

func (r *FileReporter) open() error {
	file, err := os.OpenFile(r.config.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open error log: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("secure error log: %w", err)
	}
	r.file = file
	r.writer = bufio.NewWriter(file)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("stat error log: %w", err)
	}
	r.size = info.Size()
	return nil
}

func (r *FileReporter) rotateIfNeeded(incoming int64) error {
	if r.size+incoming <= r.config.MaxBytes || r.size == 0 {
		return nil
	}
	if err := r.writer.Flush(); err != nil {
		return err
	}
	if err := r.file.Close(); err != nil {
		return err
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", r.config.Path, r.config.RetainedFiles))
	for index := r.config.RetainedFiles - 1; index >= 1; index-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", r.config.Path, index), fmt.Sprintf("%s.%d", r.config.Path, index+1))
	}
	if err := os.Rename(r.config.Path, r.config.Path+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return r.open()
}

func (r *FileReporter) flushPeriodically() {
	ticker := time.NewTicker(r.config.FlushInterval)
	defer func() {
		ticker.Stop()
		close(r.done)
	}()
	for {
		select {
		case <-ticker.C:
			r.mu.Lock()
			if !r.closed {
				if err := r.writer.Flush(); err != nil {
					r.fallback(err)
				}
			}
			r.mu.Unlock()
		case <-r.stop:
			return
		}
	}
}

func (r *FileReporter) fallback(err error) {
	_, _ = fmt.Fprintf(r.config.Fallback, "error reporter failure: %v\n", err)
}

var _ Reporter = (*FileReporter)(nil)
