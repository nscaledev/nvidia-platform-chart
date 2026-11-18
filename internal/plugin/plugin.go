// Package plugin implements the NRI CreateContainer hook. The hook reads
// in-memory state only and never returns an error: under NRI an error (or a
// slow plugin) fails the container's creation.
package plugin

import (
	"context"
	"strconv"
	"strings"

	"github.com/containerd/nri/pkg/api"
	"github.com/containerd/nri/pkg/stub"

	"github.com/nscaledev/nvidia-platform-chart/internal/fabricstate"
)

const (
	envPKey      = "NCCL_IB_PKEY"
	envPKeyValue = "NCCL_IB_PKEY_VALUE"
)

// Plugin adds NCCL_IB_PKEY to containers on HFM-programmed nodes.
type Plugin struct {
	// State returns the node's current state; it must not block or do I/O.
	State   func() fabricstate.State
	Metrics *Metrics
}

// CreateContainer adds NCCL_IB_PKEY=<index> when the node state is indexed and
// the container sets neither NCCL_IB_PKEY nor NCCL_IB_PKEY_VALUE. It always
// returns a nil error, and a recovered panic yields nil, nil, nil.
func (p *Plugin) CreateContainer(_ context.Context, _ *api.PodSandbox, ctr *api.Container) (adjust *api.ContainerAdjustment, updates []*api.ContainerUpdate, err error) {
	defer func() {
		if r := recover(); r != nil {
			adjust, updates, err = nil, nil, nil

			p.safeSkip(reasonError)
		}
	}()

	s := p.State()
	if s.Kind != fabricstate.Indexed {
		p.Metrics.countSkipped(reasonNotIndexed)

		return nil, nil, nil
	}

	if userSet(ctr.GetEnv()) {
		p.Metrics.countSkipped(reasonUserSet)

		return nil, nil, nil
	}

	adj := &api.ContainerAdjustment{}
	adj.AddEnv(envPKey, strconv.Itoa(s.Index))
	p.Metrics.countAdjusted()

	return adj, nil, nil
}

func (p *Plugin) safeSkip(reason string) {
	defer func() { _ = recover() }()

	p.Metrics.countSkipped(reason)
}

// userSet reports whether env sets either variable in any form: KEY=v, KEY=
// or a bare KEY.
func userSet(env []string) bool {
	for _, e := range env {
		key, _, _ := strings.Cut(e, "=")
		if key == envPKey || key == envPKeyValue {
			return true
		}
	}

	return false
}

var _ stub.CreateContainerInterface = (*Plugin)(nil)
