package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcher_DetectsNewFile(t *testing.T) {
	dir := t.TempDir()

	// Create an initial file so the directory isn't empty.
	if err := os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := New(dir, 100*time.Millisecond, nil, nil)
	events := w.Start()

	// Give the initial snapshot time to settle.
	time.Sleep(150 * time.Millisecond)

	// Create a new file.
	newFile := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(newFile, []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Wait for the event.
	ev := waitForEvent(t, events, 2*time.Second)
	if ev.Type != "created" {
		t.Errorf("expected event type 'created', got %q", ev.Type)
	}
	if ev.Path != newFile {
		t.Errorf("expected path %q, got %q", newFile, ev.Path)
	}

	w.Stop()
}

func TestWatcher_DetectsModification(t *testing.T) {
	dir := t.TempDir()

	target := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(target, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := New(dir, 100*time.Millisecond, nil, nil)
	events := w.Start()

	// Wait for initial snapshot.
	time.Sleep(150 * time.Millisecond)

	// Modify the file: change content and ensure mtime moves forward.
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(target, []byte("v2 longer"), 0o644); err != nil {
		t.Fatal(err)
	}

	ev := waitForEvent(t, events, 2*time.Second)
	if ev.Type != "modified" {
		t.Errorf("expected event type 'modified', got %q", ev.Type)
	}
	if ev.Path != target {
		t.Errorf("expected path %q, got %q", target, ev.Path)
	}

	w.Stop()
}

func TestWatcher_DetectsDeletion(t *testing.T) {
	dir := t.TempDir()

	target := filepath.Join(dir, "doomed.txt")
	if err := os.WriteFile(target, []byte("bye"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := New(dir, 100*time.Millisecond, nil, nil)
	events := w.Start()

	time.Sleep(150 * time.Millisecond)

	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}

	ev := waitForEvent(t, events, 2*time.Second)
	if ev.Type != "deleted" {
		t.Errorf("expected event type 'deleted', got %q", ev.Type)
	}
	if ev.Path != target {
		t.Errorf("expected path %q, got %q", target, ev.Path)
	}

	w.Stop()
}

func TestWatcher_RespectsExcludes(t *testing.T) {
	dir := t.TempDir()

	// Start with one file.
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Exclude *.log files.
	w := New(dir, 100*time.Millisecond, nil, []string{"**/*.log"})
	events := w.Start()

	time.Sleep(150 * time.Millisecond)

	// Create an excluded file.
	if err := os.WriteFile(filepath.Join(dir, "debug.log"), []byte("noise"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a non-excluded file to prove the watcher is working.
	tracked := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("signal"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The first event should be the tracked file, not the log.
	ev := waitForEvent(t, events, 2*time.Second)
	if ev.Path == filepath.Join(dir, "debug.log") {
		t.Error("watcher should have excluded *.log files")
	}
	if ev.Path != tracked {
		t.Errorf("expected path %q, got %q", tracked, ev.Path)
	}

	w.Stop()
}

func TestWatcher_DisconnectResilience(t *testing.T) {
	// Use a non-existent directory to simulate NAS disconnect after start.
	dir := t.TempDir()

	w := New(dir, 100*time.Millisecond, nil, nil)
	_ = w.Start()

	time.Sleep(50 * time.Millisecond)

	// Remove the directory to simulate NAS disconnection.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	// Wait long enough for at least 3 failed polls.
	time.Sleep(500 * time.Millisecond)

	failures := w.ConsecutiveFailures()
	if failures < 3 {
		t.Errorf("expected at least 3 consecutive failures, got %d", failures)
	}

	w.Stop()
}

// waitForEvent blocks until an event arrives or the timeout elapses.
func waitForEvent(t *testing.T, ch <-chan Event, timeout time.Duration) Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(timeout):
		t.Fatal("timed out waiting for watcher event")
		return Event{}
	}
}
