package symbol

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// LiveIndex wraps an Index with an fsnotify watcher that re-extracts a
// file's symbols whenever it changes on disk. Solves the maintenance
// problem the agent loop creates: when Claude Code (or your IDE, or
// `git pull`) rewrites a file, the symbol graph stays accurate without
// any manual `keeba compile` re-run.
//
// All access to the underlying Index goes through the provided methods
// so the watcher's mutating goroutine can run safely alongside many
// concurrent MCP query reads.
type LiveIndex struct {
	repoRoot string

	mu     sync.RWMutex
	idx    *Index
	byName map[string][]Symbol

	watcher  *fsnotify.Watcher
	flushDur time.Duration

	// dirty signals that idx has changed since the last flush. Guarded
	// by mu (read under RLock for snapshot consumers, written under
	// Lock by event handlers).
	dirty bool
}

// NewLiveIndex loads .keeba/symbols.json under repoRoot, sets up an
// fsnotify watch on every distinct directory in the indexed file set,
// and returns a LiveIndex ready for Run. Missing or unreadable index
// is surfaced — call Compile first if you haven't.
func NewLiveIndex(repoRoot string) (*LiveIndex, error) {
	idx, err := Load(repoRoot)
	if err != nil {
		return nil, err
	}

	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	li := &LiveIndex{
		repoRoot: repoRoot,
		idx:      &idx,
		byName:   buildByName(idx.Symbols),
		watcher:  fw,
		flushDur: 30 * time.Second,
	}
	if err := li.addWatchesFromIndex(); err != nil {
		_ = fw.Close()
		return nil, err
	}
	return li, nil
}

// buildByName builds the O(1) name → []Symbol lookup the MCP find_def
// handler uses.
func buildByName(syms []Symbol) map[string][]Symbol {
	out := make(map[string][]Symbol, len(syms))
	for _, s := range syms {
		out[s.Name] = append(out[s.Name], s)
	}
	return out
}

// addWatchesFromIndex registers fsnotify watches on every directory
// containing an indexed file. fsnotify on Linux is per-directory, not
// recursive, so we walk the unique dirs once.
func (li *LiveIndex) addWatchesFromIndex() error {
	dirs := map[string]struct{}{}
	for _, s := range li.idx.Symbols {
		full := filepath.Join(li.repoRoot, s.File)
		dirs[filepath.Dir(full)] = struct{}{}
	}
	for d := range dirs {
		if err := li.watcher.Add(d); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Run pumps fsnotify events until ctx is canceled. On each Write/Create
// event, the affected file is re-extracted and the in-memory index is
// updated atomically. A periodic flush writes the dirty index back to
// .keeba/symbols.json so the next `keeba mcp serve` starts from a fresh
// snapshot. Always returns ctx.Err() on shutdown.
func (li *LiveIndex) Run(ctx context.Context) error {
	flushTimer := time.NewTicker(li.flushDur)
	defer flushTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = li.flush()
			return ctx.Err()

		case ev, ok := <-li.watcher.Events:
			if !ok {
				return nil
			}
			li.handleEvent(ev)

		case <-li.watcher.Errors:
			// fsnotify backpressure / permission errors are non-fatal —
			// the snapshot is still consistent; the watcher will try
			// again on subsequent events.
			continue

		case <-flushTimer.C:
			_ = li.flush()
		}
	}
}

// handleEvent re-extracts the touched file (or removes its symbols on
// Remove/Rename) and updates the in-memory snapshot.
func (li *LiveIndex) handleEvent(ev fsnotify.Event) {
	if ev.Name == "" {
		return
	}
	rel, err := filepath.Rel(li.repoRoot, ev.Name)
	if err != nil {
		return
	}
	rel = filepath.ToSlash(rel)

	switch {
	case ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0:
		li.replaceFileSymbols(rel, nil)

	case ev.Op&(fsnotify.Write|fsnotify.Create) != 0:
		// Skip non-regular files (dirs, sockets, etc.).
		info, err := os.Stat(ev.Name)
		if err != nil || info.IsDir() {
			return
		}
		src, err := os.ReadFile(ev.Name) //nolint:gosec
		if err != nil {
			return
		}
		syms, err := extractFile(rel, src)
		if err != nil {
			return
		}
		li.replaceFileSymbols(rel, syms)
	}
}

// replaceFileSymbols swaps every symbol whose File==rel for the new
// slice (which may be empty for deletes). Rebuilds the byName map.
// Single big lock — keeps the read path lock-free except for one
// RWMutex.RLock per query, which is cheap.
func (li *LiveIndex) replaceFileSymbols(rel string, fresh []Symbol) {
	li.mu.Lock()
	defer li.mu.Unlock()

	out := li.idx.Symbols[:0:0]
	for _, s := range li.idx.Symbols {
		if s.File != rel {
			out = append(out, s)
		}
	}
	out = append(out, fresh...)
	li.idx.Symbols = out

	files := map[string]struct{}{}
	for _, s := range out {
		files[s.File] = struct{}{}
	}
	li.idx.NumFiles = len(files)
	li.idx.NumSymbols = len(out)
	li.byName = buildByName(out)
	li.dirty = true
}

// flush writes the current index back to .keeba/symbols.json if it has
// changed since the last flush. Errors are logged via the watcher's
// error channel (best-effort), not surfaced — flush failures shouldn't
// block the live MCP layer.
func (li *LiveIndex) flush() error {
	li.mu.Lock()
	if !li.dirty {
		li.mu.Unlock()
		return nil
	}
	idx := *li.idx
	li.dirty = false
	li.mu.Unlock()
	idx.GeneratedAt = time.Now().UTC()
	return Save(li.repoRoot, idx)
}

// Snapshot returns a stable, copy-on-read view of the current index
// suitable for read-only callers (MCP `summary` tool). Only as expensive
// as iterating the symbol slice once.
func (li *LiveIndex) Snapshot() *Index {
	li.mu.RLock()
	defer li.mu.RUnlock()
	cp := *li.idx
	cp.Symbols = append([]Symbol(nil), li.idx.Symbols...)
	return &cp
}

// ByName returns the symbols matching name exactly, or nil. The returned
// slice is a copy — safe to mutate.
func (li *LiveIndex) ByName(name string) []Symbol {
	li.mu.RLock()
	defer li.mu.RUnlock()
	src := li.byName[name]
	if len(src) == 0 {
		return nil
	}
	out := make([]Symbol, len(src))
	copy(out, src)
	return out
}

// Names returns every (name, []Symbol) pair via the supplied callback.
// Used for the case-insensitive substring fallback in find_def. Holding
// the read lock for the whole walk; callbacks should be cheap.
func (li *LiveIndex) Names(fn func(name string, syms []Symbol)) {
	li.mu.RLock()
	defer li.mu.RUnlock()
	for n, syms := range li.byName {
		fn(n, syms)
	}
}

// Symbols returns a copy of every symbol. Cost: O(N), used by the
// summary tool which has to filter anyway.
func (li *LiveIndex) Symbols() []Symbol {
	li.mu.RLock()
	defer li.mu.RUnlock()
	out := make([]Symbol, len(li.idx.Symbols))
	copy(out, li.idx.Symbols)
	return out
}

// Close stops the watcher and flushes any pending changes to disk.
func (li *LiveIndex) Close() error {
	flushErr := li.flush()
	closeErr := li.watcher.Close()
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}

// withSlash normalizes a path separator for cross-platform stable keys.
// Kept private; tests reach it via path-shaped fixtures instead.
func withSlash(p string) string { return strings.ReplaceAll(p, "\\", "/") }
