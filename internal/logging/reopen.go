package logging

import (
	"fmt"
	"os"
	"sync"
)

// logFileFlags and logFileMode are the open() flags and permission bits
// used both at sink construction and on every reopen. Centralised so a
// future change can never let the two paths drift.
const (
	logFileFlags = os.O_APPEND | os.O_CREATE | os.O_WRONLY
	logFileMode  = os.FileMode(0o640)
)

// ReopenSink is a zapcore.WriteSyncer wrapping an *os.File that can be
// closed and reopened in place — driven by a SIGUSR1 handler after
// logrotate renames the active log file. The sync.Mutex makes the swap
// atomic from a writer's perspective, and a Reopen failure leaves the
// previous fd intact so log records continue to land.
type ReopenSink struct {
	mu   sync.Mutex
	f    *os.File
	path string
}

// OpenReopenSink opens path with O_APPEND|O_CREATE at mode 0640 and returns
// a ReopenSink wrapping the resulting file. Any open error is returned to
// the caller so daemon startup can abort loudly when the configured path
// is unwritable, instead of silently degrading to a half-broken sink.
func OpenReopenSink(path string) (*ReopenSink, error) {
	if path == "" {
		return nil, fmt.Errorf("logging: empty path")
	}
	f, err := os.OpenFile(path, logFileFlags, logFileMode)
	if err != nil {
		return nil, err
	}
	return &ReopenSink{f: f, path: path}, nil
}

// Write serializes against Reopen so a swap happening mid-write either
// completes with the old fd or the new fd — never a torn write across the
// boundary.
func (s *ReopenSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return 0, os.ErrClosed
	}
	return s.f.Write(p)
}

// Sync holds the lock so a concurrent Reopen cannot pull the fd out from
// under fsync.
func (s *ReopenSink) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	return s.f.Sync()
}

// Close shuts down the sink permanently. Safe to call multiple times; the
// underlying fd is only closed on the first call.
func (s *ReopenSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}

// Reopen closes the current file descriptor and opens the configured path
// again. On open failure the previous fd is preserved so subsequent Write
// calls still land on disk; the caller is expected to log the error
// through the still-active sink. On open success the new fd is installed
// before the old one is closed, and any close-time error (e.g. ENOSPC
// flushing kernel buffers on NFS) is returned so the caller can record it
// — the swap itself has already succeeded so subsequent writes land on
// the new fd.
func (s *ReopenSink) Reopen() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	newF, err := os.OpenFile(s.path, logFileFlags, logFileMode)
	if err != nil {
		return err
	}

	old := s.f
	s.f = newF
	if old != nil {
		if cerr := old.Close(); cerr != nil {
			return fmt.Errorf("logging: closing previous log fd: %w", cerr)
		}
	}
	return nil
}

// Path returns the file path that was passed to OpenReopenSink. Callers
// use this to log "reopening %s" without keeping a parallel copy of the
// path string.
func (s *ReopenSink) Path() string {
	return s.path
}
