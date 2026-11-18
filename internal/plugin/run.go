package plugin

import (
	"context"
	"time"

	"github.com/nscaledev/nvidia-platform-chart/internal/fabricstate"
)

// Poller reads the node state through a tracker, keeps the node-state gauge in
// step and reports each change of the tracker's current state. Step and Run
// must be used from one goroutine at a time.
type Poller struct {
	read   func() (fabricstate.State, int)
	t      *fabricstate.Tracker
	m      *Metrics
	onLog  func(fabricstate.State, int)
	last   fabricstate.State
	logged bool
}

// NewPoller returns a Poller. read returns a reading and the number of ports
// counted. onChange is called once per change of the tracker's current state,
// including the first reading, with the ports counted in the reading that
// produced it; the count alone never triggers a call.
func NewPoller(read func() (fabricstate.State, int), t *fabricstate.Tracker, m *Metrics, onChange func(fabricstate.State, int)) *Poller {
	return &Poller{read: read, t: t, m: m, onLog: onChange}
}

// Step performs one read: read, Observe, SetNodeState, report on change.
func (p *Poller) Step() {
	s, ports := p.read()
	cur := p.t.Observe(s)
	p.m.SetNodeState(cur.Kind)

	if p.logged && cur == p.last {
		return
	}

	p.last, p.logged = cur, true
	p.onLog(cur, ports)
}

// Run calls Step every interval until ctx is done. It does not step first.
func (p *Poller) Run(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			p.Step()
		}
	}
}
