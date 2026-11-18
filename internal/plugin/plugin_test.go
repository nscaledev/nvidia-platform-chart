package plugin

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/containerd/nri/pkg/api"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/nscaledev/nvidia-platform-chart/internal/fabricstate"
)

var indexed1 = fabricstate.State{Kind: fabricstate.Indexed, PKey: 0x0600, Index: 1}

func newPlugin(s fabricstate.State) (*Plugin, *Metrics) {
	m := NewMetrics(prometheus.NewRegistry())

	return &Plugin{State: func() fabricstate.State { return s }, Metrics: m}, m
}

func create(t *testing.T, p *Plugin, env ...string) *api.ContainerAdjustment {
	t.Helper()

	adj, upd, err := p.CreateContainer(context.Background(), &api.PodSandbox{}, &api.Container{Env: env})
	if err != nil || upd != nil {
		t.Fatalf("err=%v updates=%v, want nil, nil", err, upd)
	}

	return adj
}

func skipped(m *Metrics, reason string) float64 {
	return testutil.ToFloat64(m.skipped.WithLabelValues(reason))
}

func TestIndexedAddsPKeyIndex(t *testing.T) {
	p, m := newPlugin(indexed1)

	adj := create(t, p, "PATH=/bin")
	if adj == nil || len(adj.Env) != 1 || adj.Env[0].Key != "NCCL_IB_PKEY" || adj.Env[0].Value != "1" {
		t.Fatalf("adjustment = %+v", adj)
	}

	if got := testutil.ToFloat64(m.adjusted); got != 1 {
		t.Fatalf("adjusted_total = %v", got)
	}
}

func TestUserSetIsRespected(t *testing.T) {
	p, m := newPlugin(indexed1)

	if adj := create(t, p, "NCCL_IB_PKEY=3"); adj != nil {
		t.Fatalf("adjustment = %+v", adj)
	}

	if skipped(m, "user_set") != 1 {
		t.Fatal("user_set not counted")
	}
}

func TestUserSetValueIsRespected(t *testing.T) {
	p, m := newPlugin(indexed1)

	if adj := create(t, p, "NCCL_IB_PKEY_VALUE=0x0600"); adj != nil {
		t.Fatalf("adjustment = %+v", adj)
	}

	if skipped(m, "user_set") != 1 {
		t.Fatal("user_set not counted")
	}
}

func TestUserSetWithoutValueIsRespected(t *testing.T) {
	for _, env := range []string{"NCCL_IB_PKEY", "NCCL_IB_PKEY=", "NCCL_IB_PKEY_VALUE", "NCCL_IB_PKEY_VALUE="} {
		p, m := newPlugin(indexed1)

		if adj := create(t, p, "PATH=/bin", env); adj != nil {
			t.Fatalf("%q: adjustment = %+v", env, adj)
		}

		if skipped(m, "user_set") != 1 {
			t.Fatalf("%q: user_set not counted", env)
		}
	}
}

func TestSimilarKeyIsNotUserSet(t *testing.T) {
	p, _ := newPlugin(indexed1)

	if adj := create(t, p, "NCCL_IB_PKEY_X=1", "XNCCL_IB_PKEY=1"); adj == nil {
		t.Fatal("unrelated keys must not count as user-set")
	}
}

func TestNotIndexedAddsNothing(t *testing.T) {
	for _, k := range []fabricstate.Kind{fabricstate.Slot0, fabricstate.None, fabricstate.Ambiguous} {
		p, m := newPlugin(fabricstate.State{Kind: k})

		if adj := create(t, p, "PATH=/bin"); adj != nil {
			t.Fatalf("%s: adjustment = %+v", k, adj)
		}

		if skipped(m, "not_indexed") != 1 {
			t.Fatalf("%s: not_indexed not counted", k)
		}
	}
}

func TestNeverReturnsAnError(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())
	p := &Plugin{State: func() fabricstate.State { panic("boom") }, Metrics: m}

	adj, upd, err := p.CreateContainer(context.Background(), &api.PodSandbox{}, &api.Container{})
	if adj != nil || upd != nil || err != nil {
		t.Fatalf("got %v %v %v", adj, upd, err)
	}

	if skipped(m, "error") != 1 {
		t.Fatal("error not counted")
	}
}

func TestNilArgumentsDoNotPanicOut(t *testing.T) {
	p, _ := newPlugin(indexed1)

	adj, upd, err := p.CreateContainer(context.Background(), nil, nil)
	if err != nil || upd != nil {
		t.Fatalf("err=%v upd=%v", err, upd)
	}

	if adj == nil || len(adj.Env) != 1 || adj.Env[0].Key != "NCCL_IB_PKEY" {
		t.Fatalf("adjustment = %+v", adj)
	}
}

func TestNilMetricsIsSafe(t *testing.T) {
	p := &Plugin{State: func() fabricstate.State { return indexed1 }}

	if adj := create(t, p); adj == nil {
		t.Fatal("expected adjustment")
	}
}

func TestSetNodeState(t *testing.T) {
	m := NewMetrics(prometheus.NewRegistry())
	m.SetNodeState(fabricstate.Indexed)
	m.SetNodeState(fabricstate.None)

	if testutil.ToFloat64(m.nodeState.WithLabelValues("indexed")) != 0 ||
		testutil.ToFloat64(m.nodeState.WithLabelValues("none")) != 1 {
		t.Fatal("node_state must be 1 only for the current state")
	}
}

func TestConcurrentCreateAndObserve(t *testing.T) {
	tr := fabricstate.NewTracker(t.TempDir() + "/state")
	m := NewMetrics(prometheus.NewRegistry())
	p := &Plugin{State: tr.Current, Metrics: m}

	stop := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)

		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}

			// Each target reading is observed twice so the tracker's
			// 2-consecutive-reading rule actually replaces the state.
			next := fabricstate.State{Kind: fabricstate.Slot0}
			if i%2 == 1 {
				next = fabricstate.State{Kind: fabricstate.Indexed, PKey: 0x0600, Index: 1 + (i/2)%3}
			}

			tr.Observe(next)
			tr.Observe(next)
		}
	}()

	var wg sync.WaitGroup

	for g := 0; g < 50; g++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := 0; i < 200; i++ {
				adj, _, err := p.CreateContainer(context.Background(), &api.PodSandbox{}, &api.Container{Env: []string{"A=b"}})
				if err != nil {
					t.Error(err)
					return
				}

				if adj == nil {
					continue
				}

				n, convErr := strconv.Atoi(adj.Env[0].Value)
				if convErr != nil || n < 1 || n > 3 {
					t.Errorf("invalid index %q", adj.Env[0].Value)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(stop)
	<-done

	if testutil.ToFloat64(m.adjusted) == 0 || skipped(m, "not_indexed") == 0 {
		t.Fatalf("state never flipped: adjusted=%v not_indexed=%v", testutil.ToFloat64(m.adjusted), skipped(m, "not_indexed"))
	}
}
