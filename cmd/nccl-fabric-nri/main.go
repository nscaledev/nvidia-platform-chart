// Command nccl-fabric-nri is an NRI plugin that sets NCCL_IB_PKEY in
// containers on nodes whose InfiniBand ports carry an HFM-programmed tenant
// p_key.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/containerd/nri/pkg/stub"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/nscaledev/nvidia-platform-chart/internal/fabricstate"
	"github.com/nscaledev/nvidia-platform-chart/internal/plugin"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		sysfs       = flag.String("sysfs", "/sys/class/infiniband", "InfiniBand sysfs directory")
		stateFile   = flag.String("state-file", "/var/lib/nccl-fabric-nri/state", "file persisting the last known tenant layout")
		interval    = flag.Duration("interval", 5*time.Second, "node state poll interval")
		pluginIdx   = flag.String("plugin-idx", "90", "NRI plugin index")
		nriSocket   = flag.String("nri-socket", "/var/run/nri/nri.sock", "NRI socket path")
		metricsAddr = flag.String("metrics-addr", ":9900", "address serving /metrics")
	)

	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	if *interval <= 0 {
		log.Error("--interval must be greater than 0", "interval", interval.String())
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	reg := prometheus.NewRegistry()
	metrics := plugin.NewMetrics(reg)
	tracker := fabricstate.NewTracker(*stateFile)

	// Read the node once before registering with NRI so the first containers
	// never see an unpopulated state after a restart.
	poller := plugin.NewPoller(func() (fabricstate.State, int) { return fabricstate.Read(*sysfs) }, tracker, metrics,
		func(s fabricstate.State, ports int) {
			log.Info("node state changed", "state", string(s.Kind), "pkey", fmt.Sprintf("0x%04x", s.PKey), "index", s.Index, "ports", ports)
		})
	poller.Step()

	// Bind before registering so a port clash fails start-up loudly.
	ln, err := net.Listen("tcp", *metricsAddr)
	if err != nil {
		log.Error("metrics listen failed", "addr", *metricsAddr, "err", err)
		return 1
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		// A later serve error must not take the NRI plugin down.
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("metrics server stopped", "err", err)
		}
	}()

	go poller.Run(ctx, *interval)

	p := &plugin.Plugin{State: tracker.Current, Metrics: metrics}

	// A closed NRI connection means containerd restarted or dropped us: exit
	// non-zero so the DaemonSet restarts the pod and the plugin re-registers.
	s, err := stub.New(p,
		stub.WithPluginName("nccl-fabric-nri"),
		stub.WithPluginIdx(*pluginIdx),
		stub.WithSocketPath(*nriSocket),
		stub.WithOnClose(func() {
			if ctx.Err() != nil {
				return // we are stopping on a signal; run() exits 0
			}

			log.Error("NRI connection closed, exiting")
			os.Exit(1)
		}),
	)
	if err != nil {
		log.Error("creating NRI stub failed", "err", err)
		return 1
	}

	// The stub's ttrpc server ignores ctx once started, so stop it on a
	// signal; otherwise Run never returns and the kubelet kills us after the
	// grace period. Upstream window left open: stub.Start holds the stub lock
	// while it waits for Configure (cfgErrC), so a SIGTERM between
	// RegisterPlugin and Configure blocks Stop and can hang until SIGKILL.
	go func() {
		<-ctx.Done()
		s.Stop()
	}()

	err = s.Run(ctx)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = srv.Shutdown(shutdownCtx)

	if err != nil && !errors.Is(err, context.Canceled) && ctx.Err() == nil {
		log.Error("NRI plugin stopped", "err", err)
		return 1
	}

	return 0
}
