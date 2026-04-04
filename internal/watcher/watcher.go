package watcher

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gobwas/glob"
)

// Event represents a file system change detected by the polling watcher.
type Event struct {
	Path string
	Type string // "created", "modified", "deleted"
}

// fileInfo records the mtime and size of a file for snapshot comparison.
type fileInfo struct {
	modTime time.Time
	size    int64
}

// Watcher polls a directory at a fixed interval and emits events when files
// are created, modified, or deleted. It works on any filesystem including
// NAS/network shares where inotify/FSEvents are unavailable.
type Watcher struct {
	dir      string
	interval time.Duration
	events   chan Event
	stop     chan struct{}
	done     chan struct{}
	snapshot map[string]fileInfo

	includes []glob.Glob
	excludes []glob.Glob

	mu                  sync.Mutex
	consecutiveFailures int
}

// New creates a Watcher that polls dir every interval. The includes and
// excludes slices are glob patterns applied to each file's path relative
// to dir (using forward slashes). If includes is non-empty, only files
// matching at least one include pattern are tracked. Files matching any
// exclude pattern are always ignored.
func New(dir string, interval time.Duration, includes, excludes []string) *Watcher {
	w := &Watcher{
		dir:      dir,
		interval: interval,
		events:   make(chan Event, 64),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		snapshot: make(map[string]fileInfo),
	}

	w.includes = compileGlobs(includes)
	w.excludes = compileGlobs(excludes)

	return w
}

// Start begins polling in a background goroutine. It takes an initial snapshot
// (no events emitted) and then diffs on each tick. Returns the event channel.
func (w *Watcher) Start() <-chan Event {
	// Take initial snapshot silently.
	initial, err := w.takeSnapshot()
	if err != nil {
		log.Printf("[watch] initial scan failed: %v\n", err)
	} else {
		w.snapshot = initial
	}

	go w.loop()
	return w.events
}

// Stop signals the polling goroutine to exit and waits for it to finish.
func (w *Watcher) Stop() {
	close(w.stop)
	<-w.done
	close(w.events)
}

// ConsecutiveFailures returns the current count of consecutive snapshot
// failures. Useful for testing disconnect resilience.
func (w *Watcher) ConsecutiveFailures() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.consecutiveFailures
}

func (w *Watcher) loop() {
	defer close(w.done)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			w.poll()
		}
	}
}

func (w *Watcher) poll() {
	newSnapshot, err := w.takeSnapshot()
	if err != nil {
		w.mu.Lock()
		w.consecutiveFailures++
		failures := w.consecutiveFailures
		w.mu.Unlock()
		if failures >= 3 {
			log.Printf("[watch] NAS appears disconnected (%d failures). Serving from cache.\n", failures)
		}
		return
	}
	w.mu.Lock()
	w.consecutiveFailures = 0
	w.mu.Unlock()

	// Detect created and modified files.
	for path, info := range newSnapshot {
		old, exists := w.snapshot[path]
		if !exists {
			w.emit(Event{Path: path, Type: "created"})
		} else if info.modTime != old.modTime || info.size != old.size {
			w.emit(Event{Path: path, Type: "modified"})
		}
	}

	// Detect deleted files.
	for path := range w.snapshot {
		if _, exists := newSnapshot[path]; !exists {
			w.emit(Event{Path: path, Type: "deleted"})
		}
	}

	w.snapshot = newSnapshot
}

func (w *Watcher) emit(e Event) {
	select {
	case w.events <- e:
	default:
		// Channel full; drop event to avoid blocking the poll loop.
	}
}

func (w *Watcher) takeSnapshot() (map[string]fileInfo, error) {
	snap := make(map[string]fileInfo)

	err := filepath.WalkDir(w.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		relPath, relErr := filepath.Rel(w.dir, path)
		if relErr != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)

		if !w.matchesFilter(relPath) {
			return nil
		}

		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}

		snap[path] = fileInfo{
			modTime: info.ModTime(),
			size:    info.Size(),
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", w.dir, err)
	}

	return snap, nil
}

// matchesFilter returns true if relPath passes the include/exclude filter.
func (w *Watcher) matchesFilter(relPath string) bool {
	if len(w.includes) > 0 {
		matched := false
		for _, g := range w.includes {
			if g.Match(relPath) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	for _, g := range w.excludes {
		if g.Match(relPath) {
			return false
		}
	}
	return true
}

// compileGlobs compiles pattern strings into glob matchers. For patterns
// starting with "**/" it also compiles the suffix so root-level files match.
// Invalid patterns are silently ignored.
func compileGlobs(patterns []string) []glob.Glob {
	var compiled []glob.Glob
	for _, p := range patterns {
		g, err := glob.Compile(p, '/')
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "WARNING: invalid glob %q: %v\n", p, err)
			continue
		}
		compiled = append(compiled, g)

		if strings.HasPrefix(p, "**/") {
			suffix := strings.TrimPrefix(p, "**/")
			sg, err := glob.Compile(suffix, '/')
			if err != nil {
				continue
			}
			compiled = append(compiled, sg)
		}
	}
	return compiled
}
