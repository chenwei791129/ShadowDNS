package logging

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
)

// inode returns the inode number for path. Test fails if stat fails.
func inode(t *testing.T, path string) uint64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("stat sys is not *syscall.Stat_t: %T", st.Sys())
	}
	return sys.Ino
}

// Requirement: Daemon SHALL reopen log file on SIGUSR1 — after rename, a new
// inode is created at the original path and writes land in the new file.
func TestReopenSink_ReopenAfterRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shadowdns.log")
	sink, err := OpenReopenSink(path)
	if err != nil {
		t.Fatalf("OpenReopenSink: %v", err)
	}
	t.Cleanup(func() { _ = sink.Close() })

	if _, err := sink.Write([]byte("first\n")); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	originalInode := inode(t, path)

	rotated := path + ".1"
	if err := os.Rename(path, rotated); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if err := sink.Reopen(); err != nil {
		t.Fatalf("Reopen: %v", err)
	}

	if _, err := sink.Write([]byte("second\n")); err != nil {
		t.Fatalf("write 2: %v", err)
	}

	newInode := inode(t, path)
	if newInode == originalInode {
		t.Fatalf("expected new inode after reopen, got same %d", newInode)
	}

	rotatedContent, err := os.ReadFile(rotated)
	if err != nil {
		t.Fatalf("read rotated: %v", err)
	}
	if string(rotatedContent) != "first\n" {
		t.Errorf("rotated file = %q, want %q", rotatedContent, "first\n")
	}

	newContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read new: %v", err)
	}
	if string(newContent) != "second\n" {
		t.Errorf("new file = %q, want %q", newContent, "second\n")
	}
}

// Requirement: Reopen failure preserves previous fd. After deleting the
// parent directory, Reopen returns an error, but the existing fd (already
// open before the directory vanished) still accepts writes — the kernel
// keeps it alive until close.
func TestReopenSink_ReopenFailurePreservesFd(t *testing.T) {
	parent := t.TempDir()
	subdir := filepath.Join(parent, "logs")
	if err := os.Mkdir(subdir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(subdir, "shadowdns.log")
	sink, err := OpenReopenSink(path)
	if err != nil {
		t.Fatalf("OpenReopenSink: %v", err)
	}
	t.Cleanup(func() { _ = sink.Close() })

	if _, err := sink.Write([]byte("before\n")); err != nil {
		t.Fatalf("write before: %v", err)
	}

	if err := os.RemoveAll(subdir); err != nil {
		t.Fatalf("removeall: %v", err)
	}

	if err := sink.Reopen(); err == nil {
		t.Fatal("Reopen on missing parent dir: expected error, got nil")
	}

	// Old fd must still accept writes — the kernel keeps it alive even
	// though no path resolves to it any more.
	if _, err := sink.Write([]byte("after\n")); err != nil {
		t.Fatalf("write after failed reopen: %v", err)
	}
}

// Requirement: Concurrent writers and Reopen do not lose bytes. With 100
// writers each emitting a unique tagged line and 50 Reopen calls
// interleaved, the union of (active log + all rotated copies) MUST contain
// every line exactly once.
func TestReopenSink_ConcurrentWriteAndReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shadowdns.log")
	sink, err := OpenReopenSink(path)
	if err != nil {
		t.Fatalf("OpenReopenSink: %v", err)
	}
	t.Cleanup(func() { _ = sink.Close() })

	const writers = 100
	const messagesPerWriter = 50
	const rotations = 50

	var rotatedPaths []string
	var rotatedMu sync.Mutex

	var wg sync.WaitGroup
	wg.Add(writers + 1)

	for w := 0; w < writers; w++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < messagesPerWriter; i++ {
				line := []byte("w=" + strconv.Itoa(id) + ",i=" + strconv.Itoa(i) + "\n")
				if _, err := sink.Write(line); err != nil {
					t.Errorf("write w=%d i=%d: %v", id, i, err)
					return
				}
			}
		}(w)
	}

	go func() {
		defer wg.Done()
		for r := 0; r < rotations; r++ {
			rotated := path + "." + strconv.Itoa(r)
			if err := os.Rename(path, rotated); err != nil {
				// Path may not exist if rotation happens too fast — try again next round.
				continue
			}
			rotatedMu.Lock()
			rotatedPaths = append(rotatedPaths, rotated)
			rotatedMu.Unlock()
			if err := sink.Reopen(); err != nil {
				t.Errorf("Reopen: %v", err)
				return
			}
		}
	}()

	wg.Wait()

	// Aggregate every emitted line across the active file plus every
	// rotated copy and confirm the count equals writers*messagesPerWriter.
	seen := map[string]int{}
	collect := func(p string) {
		t.Helper()
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		for _, line := range splitLines(b) {
			if line == "" {
				continue
			}
			seen[line]++
		}
	}
	collect(path)
	rotatedMu.Lock()
	for _, p := range rotatedPaths {
		collect(p)
	}
	rotatedMu.Unlock()

	want := writers * messagesPerWriter
	if len(seen) != want {
		t.Errorf("unique lines = %d, want %d", len(seen), want)
	}
	for line, count := range seen {
		if count != 1 {
			t.Errorf("line %q appeared %d times, want 1", line, count)
		}
	}
}

// Requirement: OpenReopenSink rejects empty path so callers cannot
// accidentally open the working directory or some other surprise.
func TestOpenReopenSink_EmptyPath(t *testing.T) {
	if _, err := OpenReopenSink(""); err == nil {
		t.Fatal("OpenReopenSink(\"\"): expected error, got nil")
	}
}

// Requirement: OpenReopenSink surfaces underlying os.OpenFile errors
// (e.g. parent directory missing) so daemon startup can fail loudly.
func TestOpenReopenSink_NonexistentParent(t *testing.T) {
	if _, err := OpenReopenSink("/nonexistent-dir-for-test/shadowdns.log"); err == nil {
		t.Fatal("OpenReopenSink with missing parent dir: expected error, got nil")
	}
}

func splitLines(b []byte) []string {
	var out []string
	start := 0
	for i, c := range b {
		if c == '\n' {
			out = append(out, string(b[start:i]))
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, string(b[start:]))
	}
	return out
}

// TestReopenSinkClosedTerminal verifies that Close is terminal: Reopen on a
// closed sink returns os.ErrClosed without opening a new fd (a closed sink is
// never resurrected), while Reopen on a live sink still swaps the fd.
func TestReopenSinkClosedTerminal(t *testing.T) {
	t.Run("reopen after close returns ErrClosed and opens nothing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "app.log")
		s, err := OpenReopenSink(path)
		if err != nil {
			t.Fatalf("OpenReopenSink: %v", err)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		err = s.Reopen()
		if !errors.Is(err, os.ErrClosed) {
			t.Fatalf("Reopen on closed sink: err = %v, want os.ErrClosed", err)
		}
		s.mu.Lock()
		f := s.f
		s.mu.Unlock()
		if f != nil {
			t.Fatal("Reopen on closed sink installed a new fd; close must be terminal")
		}
	})

	t.Run("reopen on live sink still swaps the fd", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "app.log")
		s, err := OpenReopenSink(path)
		if err != nil {
			t.Fatalf("OpenReopenSink: %v", err)
		}
		defer func() { _ = s.Close() }()

		// Rotate: rename the active file, then Reopen must create a fresh one.
		if err := os.Rename(path, path+".1"); err != nil {
			t.Fatalf("rename: %v", err)
		}
		if err := s.Reopen(); err != nil {
			t.Fatalf("Reopen on live sink: %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Reopen did not recreate %s: %v", path, err)
		}
		if _, err := s.Write([]byte("after reopen\n")); err != nil {
			t.Fatalf("Write after reopen: %v", err)
		}
	})
}
