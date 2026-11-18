# nvidia-platform-chart

Opinionated umbrella chart for deploying NVIDIA components and node-level tuning.

See [chart/](chart/) for the Helm chart and installation instructions.

## nccl-fabric-nri image

The `ghcr.io/nscaledev/nccl-fabric-nri` image is built from the release commit
and tagged with the chart version. release-please only bumps the `chart`
package for commits that touch `chart/`, so a Go change (`cmd/`, `internal/`)
that must ship immediately should also touch `chart/`.
