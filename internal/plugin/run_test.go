package plugin

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/nscaledev/nvidia-platform-chart/internal/fabricstate"
)

// runPoll drives a Poller as main does: one Step, then Run until dur passes.
func runPoll(t *testing.T, read func() (fabricstate.State, int), m *Metrics, interval, dur time.Duration, onChange func(fabricstate.State, int)) {
	t.Helper()

	tr := fabricstate.NewTracker(filepath.Join(t.TempDir(), "state"))
	ctx, cancel := context.WithTimeout(context.Background(), dur)
	defer cancel()

	done := make(chan struct{})

	go func() {
		p := NewPoller(read, tr, m, onChange)
		p.Step()
		p.Run(ctx, interval)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(dur + 2*time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

func TestPollReadsImmediatelyAndOnInterval(t *testing.T) {
	var n atomic.Int32

	read := func() (fabricstate.State, int) {
		n.Add(1)
		return fabricstate.State{Kind: fabricstate.None}, 0
	}

	runPoll(t, read, NewMetrics(prometheus.NewRegistry()), 10*time.Millisecond, 100*time.Millisecond, func(fabricstate.State, int) {})

	if got := n.Load(); got < 3 {
		t.Fatalf("reads = %d, want >= 3", got)
	}
}

func TestPollLogsOnlyOnChange(t *testing.T) {
	var (
		mu    sync.Mutex
		lines []string
	)

	logf := func(s fabricstate.State, _ int) {
		mu.Lock()
		defer mu.Unlock()

		lines = append(lines, fmt.Sprint(s))
	}

	read := func() (fabricstate.State, int) { return indexed1, 2 }

	runPoll(t, read, NewMetrics(prometheus.NewRegistry()), 10*time.Millisecond, 100*time.Millisecond, logf)

	mu.Lock()
	defer mu.Unlock()

	if len(lines) != 1 {
		t.Fatalf("log lines = %q, want exactly 1", lines)
	}
}

func TestPollSetsNodeStateGauge(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())

	runPoll(t, func() (fabricstate.State, int) { return indexed1, 2 }, m, 10*time.Millisecond, 50*time.Millisecond, func(fabricstate.State, int) {})

	for _, k := range fabricstate.Kinds {
		want := 0.0
		if k == fabricstate.Indexed {
			want = 1
		}

		if got := testutil.ToFloat64(m.nodeState.WithLabelValues(string(k))); got != want {
			t.Errorf("gauge %s = %v, want %v", k, got, want)
		}
	}
}

func TestStepPopulatesCurrentBeforeAnyLoop(t *testing.T) {
	tr := fabricstate.NewTracker(filepath.Join(t.TempDir(), "state"))
	m := NewMetrics(prometheus.NewRegistry())
	logs := 0

	p := NewPoller(func() (fabricstate.State, int) { return indexed1, 2 }, tr, m, func(fabricstate.State, int) { logs++ })
	p.Step()

	if got := tr.Current(); got != indexed1 {
		t.Fatalf("Current = %+v, want %+v", got, indexed1)
	}

	if logs != 1 {
		t.Fatalf("logs = %d, want 1", logs)
	}

	// Run must not repeat the first log for an unchanged state.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	p.Run(ctx, 10*time.Millisecond)

	if logs != 1 {
		t.Fatalf("logs after Run = %d, want 1", logs)
	}
}

func TestStepLogsPortsWithoutComparingThem(t *testing.T) {
	tr := fabricstate.NewTracker(filepath.Join(t.TempDir(), "state"))
	m := NewMetrics(prometheus.NewRegistry())

	ports := 2

	var logged []int

	p := NewPoller(func() (fabricstate.State, int) { return indexed1, ports }, tr, m,
		func(_ fabricstate.State, n int) { logged = append(logged, n) })

	p.Step()

	ports = 1 // a port lost its tenant entry; the state is unchanged
	p.Step()

	if len(logged) != 1 || logged[0] != 2 {
		t.Fatalf("logged ports = %v, want [2]", logged)
	}
}
