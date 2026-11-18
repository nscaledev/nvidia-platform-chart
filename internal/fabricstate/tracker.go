package fabricstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Tracker holds the node's current state. Current is lock-free so the NRI hook
// can read it on the container-creation path; Observe is called by the poller.
//
// Once an Indexed state is held it is sticky: None never replaces it, and any
// other differing reading replaces it only after being seen on two consecutive
// Observe calls, so a single transient bad sysfs read cannot drop it.
type Tracker struct {
	current atomic.Pointer[State]
	path    string

	mu      sync.Mutex // serialises Observe; guards pending
	pending *State     // contradicting reading seen on the previous Observe
}

// renameFile is a seam so tests can make the final step of a save fail.
var renameFile = os.Rename

// stateFile is the on-disk form of an Indexed state.
type stateFile struct {
	Kind  Kind   `json:"kind"`
	PKey  string `json:"pkey"`
	Index int    `json:"index"`
}

// NewTracker returns a tracker that persists a known Indexed state to
// stateFile. A valid persisted Indexed state is loaded; anything else
// (missing, truncated, garbage, not indexed) is ignored.
func NewTracker(stateFile string) *Tracker {
	t := &Tracker{path: stateFile}

	initial := State{Kind: None}
	if s, ok := load(stateFile); ok {
		initial = s
	}

	t.current.Store(&initial)

	return t
}

// Current returns the current state without locking or I/O.
func (t *Tracker) Current() State {
	return *t.current.Load()
}

// Observe applies a reading to the current state and returns the resulting
// current state. A changed Indexed state is persisted (best effort).
func (t *Tracker) Observe(s State) State {
	t.mu.Lock()
	defer t.mu.Unlock()

	cur := *t.current.Load()

	if cur.Kind != Indexed {
		t.pending = nil
		t.set(cur, s)

		return s
	}

	if s.Kind == None {
		// Neither confirms nor contradicts: leave any pending reading alone.
		return cur
	}

	if s == cur {
		t.pending = nil

		return cur
	}

	if t.pending == nil || *t.pending != s {
		p := s
		t.pending = &p

		return cur
	}

	t.pending = nil
	t.set(cur, s)

	return s
}

// set publishes next and persists it when it is a new Indexed state.
func (t *Tracker) set(cur, next State) {
	t.current.Store(&next)

	if next == cur {
		return
	}

	// The file exists exactly when the tracker holds Indexed, so a restart
	// never reloads a layout that has since been replaced. Best effort:
	// in-memory state is authoritative.
	if next.Kind == Indexed {
		if t.save(next) != nil {
			t.remove()
		}

		return
	}

	if cur.Kind == Indexed {
		t.remove()
	}
}

func (t *Tracker) remove() {
	if t.path != "" {
		_ = os.Remove(t.path)
	}
}

func (t *Tracker) save(s State) error {
	if t.path == "" {
		return nil
	}

	b, err := json.Marshal(stateFile{Kind: Indexed, PKey: fmt.Sprintf("0x%04x", s.PKey), Index: s.Index})
	if err != nil {
		return err
	}

	dir := filepath.Dir(t.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	f, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}

	tmp := f.Name()

	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}

	if err == nil {
		err = renameFile(tmp, t.path)
	}

	if err != nil {
		_ = os.Remove(tmp)
	}

	return err
}

// load reads and validates a persisted Indexed state.
func load(path string) (State, bool) {
	if path == "" {
		return State{}, false
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return State{}, false
	}

	var f stateFile
	if err := json.Unmarshal(b, &f); err != nil || f.Kind != Indexed || f.Index < 1 {
		return State{}, false
	}

	hex, ok := strings.CutPrefix(f.PKey, "0x")
	if !ok {
		return State{}, false
	}

	v, err := strconv.ParseUint(hex, 16, 16)
	if err != nil || v == 0 || v >= 0x7fff {
		return State{}, false
	}

	return State{Kind: Indexed, PKey: uint16(v), Index: f.Index}, true
}
