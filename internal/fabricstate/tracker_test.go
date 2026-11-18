package fabricstate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

var (
	idx1  = State{Kind: Indexed, PKey: 0x0600, Index: 1}
	idx2  = State{Kind: Indexed, PKey: 0x0700, Index: 1}
	slot0 = State{Kind: Slot0, PKey: 0x0030}
	none  = State{Kind: None}
	amb   = State{Kind: Ambiguous}
)

func newTracker(t *testing.T) (*Tracker, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "state")

	return NewTracker(path), path
}

func expect(t *testing.T, tr *Tracker, want State) {
	t.Helper()

	if got := tr.Current(); got != want {
		t.Fatalf("Current() = %+v, want %+v", got, want)
	}
}

func TestFreshTrackerFollowsObservations(t *testing.T) {
	tr, _ := newTracker(t)
	expect(t, tr, none)

	for _, s := range []State{slot0, amb, none, slot0} {
		if got := tr.Observe(s); got != s {
			t.Fatalf("Observe(%+v) = %+v", s, got)
		}

		expect(t, tr, s)
	}

	if got := tr.Observe(idx1); got != idx1 {
		t.Fatalf("Observe(indexed) = %+v", got)
	}

	expect(t, tr, idx1)
}

func TestNoneDoesNotResetPendingContradiction(t *testing.T) {
	tr, _ := newTracker(t)
	tr.Observe(idx1)
	tr.Observe(slot0)
	tr.Observe(none)
	expect(t, tr, idx1)
	tr.Observe(slot0)
	expect(t, tr, slot0)
}

func TestReplacingIndexedRemovesStateFile(t *testing.T) {
	for name, c := range map[string]State{"slot0": slot0, "ambiguous": amb} {
		t.Run(name, func(t *testing.T) {
			tr, path := newTracker(t)
			tr.Observe(idx1)
			tr.Observe(c)
			tr.Observe(c)

			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("stale state file remains: %v", err)
			}

			expect(t, NewTracker(path), none)
		})
	}
}

func TestFailedSaveOfReplacementLeavesNoStaleFile(t *testing.T) {
	tr, path := newTracker(t)
	tr.Observe(idx1)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("old state file missing before the failed save: %v", err)
	}

	orig := renameFile
	renameFile = func(string, string) error { return errors.New("injected") }

	defer func() { renameFile = orig }()

	tr.Observe(idx2)
	tr.Observe(idx2)
	expect(t, tr, idx2)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale state file remains: %v", err)
	}

	ents, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(ents) != 0 {
		t.Fatalf("leftover files %v, %v", ents, err)
	}
}

func TestNoneDoesNotDowngradeIndexed(t *testing.T) {
	tr, _ := newTracker(t)
	tr.Observe(idx1)

	for range 5 {
		if got := tr.Observe(none); got != idx1 {
			t.Fatalf("Observe(None) = %+v, want %+v", got, idx1)
		}
	}

	expect(t, tr, idx1)
}

func TestSingleContradictionIsIgnored(t *testing.T) {
	for name, c := range map[string]State{"slot0": slot0, "ambiguous": amb, "other indexed": idx2} {
		t.Run(name, func(t *testing.T) {
			tr, _ := newTracker(t)
			tr.Observe(idx1)

			if got := tr.Observe(c); got != idx1 {
				t.Fatalf("got %+v, want unchanged", got)
			}

			expect(t, tr, idx1)
		})
	}
}

func TestTwoConsecutiveContradictionsReplace(t *testing.T) {
	for name, c := range map[string]State{"slot0": slot0, "ambiguous": amb, "other indexed": idx2} {
		t.Run(name, func(t *testing.T) {
			tr, _ := newTracker(t)
			tr.Observe(idx1)
			tr.Observe(c)

			if got := tr.Observe(c); got != c {
				t.Fatalf("got %+v, want %+v", got, c)
			}

			expect(t, tr, c)
		})
	}
}

func TestContradictionReplacesIndexed(t *testing.T) {
	tr, _ := newTracker(t)
	tr.Observe(idx1)
	tr.Observe(slot0)
	tr.Observe(slot0)
	expect(t, tr, slot0)

	// A new Indexed state can be reached again from Slot0 (no stickiness).
	tr.Observe(idx2)
	expect(t, tr, idx2)
}

func TestInterleavedContradictionsDoNotReplace(t *testing.T) {
	for _, seq := range [][]State{
		{slot0, idx1, slot0, idx1, slot0}, // matching reading resets
		{slot0, amb, slot0, amb},          // different contradictions
		{idx2, slot0, idx2, slot0},        // alternating
		{slot0, amb, idx2, slot0, amb, idx2},
	} {
		tr, _ := newTracker(t)
		tr.Observe(idx1)

		for _, s := range seq {
			tr.Observe(s)
			expect(t, tr, idx1)
		}
	}
}

func TestStateReloadedAfterRestart(t *testing.T) {
	tr, path := newTracker(t)
	tr.Observe(idx1)

	if b, err := os.ReadFile(path); err != nil || string(b) != "{\"kind\":\"indexed\",\"pkey\":\"0x0600\",\"index\":1}\n" {
		t.Fatalf("file = %q, %v", b, err)
	}

	expect(t, NewTracker(path), idx1)
}

func TestReplacementIsPersisted(t *testing.T) {
	tr, path := newTracker(t)
	tr.Observe(idx1)
	tr.Observe(idx2)
	tr.Observe(idx2)

	expect(t, NewTracker(path), idx2)
}

func TestReloadedStateIsSticky(t *testing.T) {
	_, path := newTracker(t)
	if err := os.WriteFile(path, []byte(`{"kind":"indexed","pkey":"0x0600","index":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	tr := NewTracker(path)
	tr.Observe(none)
	tr.Observe(slot0)
	expect(t, tr, idx1)
}

func TestCorruptStateFileIgnored(t *testing.T) {
	for _, content := range []string{
		`{"kind":`, "garbage", "", "{}",
		`{"kind":"slot0","pkey":"0x0600","index":1}`,
		`{"kind":"indexed","pkey":"0x0600","index":0}`,
		`{"kind":"indexed","pkey":"0x0600","index":-1}`,
		`{"kind":"indexed","pkey":"zzz","index":1}`,
		`{"kind":"indexed","pkey":"0x7fff","index":1}`,
		`{"kind":"indexed","pkey":"0x0000","index":1}`,
		`{"kind":"indexed","pkey":"0x10600","index":1}`,
	} {
		path := filepath.Join(t.TempDir(), "state")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		tr := NewTracker(path)
		expect(t, tr, none)
		tr.Observe(slot0)
		expect(t, tr, slot0)
	}
}

func TestMissingStateFileIsNone(t *testing.T) {
	expect(t, NewTracker(filepath.Join(t.TempDir(), "nope", "state")), none)
	expect(t, NewTracker(""), none)
}

func TestUnwritableStateFileKeepsWorking(t *testing.T) {
	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")

	if err := os.Mkdir(ro, 0o500); err != nil {
		t.Fatal(err)
	}

	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	if err := os.WriteFile(filepath.Join(dir, "file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{filepath.Join(ro, "sub", "state"), filepath.Join(dir, "file", "state"), ""} {
		tr := NewTracker(path)
		if got := tr.Observe(idx1); got != idx1 {
			t.Fatalf("path %q: got %+v", path, got)
		}

		expect(t, tr, idx1)
	}
}

func TestOnlyIndexedIsPersisted(t *testing.T) {
	tr, path := newTracker(t)
	tr.Observe(none)
	tr.Observe(slot0)
	tr.Observe(amb)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("state file exists after non-indexed observations: %v", err)
	}

	tr.Observe(idx1)
	tr.Observe(slot0) // single contradiction, not applied

	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}

	ents, _ := os.ReadDir(filepath.Dir(path))
	if len(ents) != 1 {
		t.Fatalf("stray temp files left behind: %v", ents)
	}
}

func TestIndexedNotRewrittenWhenUnchanged(t *testing.T) {
	tr, path := newTracker(t)
	tr.Observe(idx1)

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	tr.Observe(idx1)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("unchanged state was rewritten")
	}
}

func TestConcurrentCurrentAndObserve(t *testing.T) {
	tr, _ := newTracker(t)
	done := make(chan struct{})

	go func() {
		defer close(done)

		for range 500 {
			tr.Observe(idx1)
			tr.Observe(slot0)
			tr.Observe(slot0)
		}
	}()

	for range 2000 {
		tr.Current()
	}

	<-done
}
