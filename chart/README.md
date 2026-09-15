# nvidia-platform

Umbrella chart for deploying NVIDIA GPU/network infrastructure and node-level tuning

## What This Chart Installs

| Component | Description |
|-----------|-------------|
| [Node Feature Discovery](https://github.com/kubernetes-sigs/node-feature-discovery) | Detects hardware features and labels nodes (GPU, NIC, PCI devices) |
| [GPU Operator](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/) | Manages NVIDIA GPU drivers, device plugin, and related components |
| [Network Operator](https://docs.nvidia.com/networking/display/cokan10/network+operator) | Manages NVIDIA networking components (RDMA, SR-IOV, etc.) |
| GPU Node Config | DaemonSet that configures IOMMU passthrough and disables ACS on PCI switches |
| NIC Cluster Policy | Configures RDMA shared device plugin for ConnectX NICs |
| [DRANET](https://dranet.sigs.k8s.io/) | DRA driver exposing RDMA-capable NICs via a `DeviceClass`, as an alternative to the RDMA shared device plugin |

## Prerequisites

- Kubernetes 1.27+
- Helm 3.12+
- Nodes with NVIDIA GPUs and/or Mellanox ConnectX NICs
- A CDI-capable container runtime (neither operator configures the container runtime for CDI itself)

## Installation

```bash
# Add the NVIDIA Helm repository
helm repo add nvidia https://helm.ngc.nvidia.com/nvidia

# Build subchart dependencies
helm dependency build ./chart

# Install
helm install nvidia-platform ./chart --namespace nvidia-platform --create-namespace
```

## Enabling Dynamic Resource Allocation (DRA)

By default this chart deploys the GPU Operator's classic `ClusterPolicy` stack (device plugin +
toolkit) and the RDMA shared device plugin for NICs. NVIDIA also ships a `GPUCluster` CR that
manages the DRA driver for GPU and ComputeDomain resource allocation, and [DRANET](https://dranet.sigs.k8s.io/)
is a DRA driver that exposes RDMA-capable NICs the same way. [values-dra.yaml](values-dra.yaml) is
an example overlay that switches to the full DRA stack (both GPUs and NICs) so that workloads can
request a GPU and a NIC aligned to the same PCIe root via a constraint on a `ResourceClaimTemplate`:

```bash
helm install nvidia-platform ./chart --namespace nvidia-platform --create-namespace \
  -f chart/values-dra.yaml
```

Requires Kubernetes 1.35+ (DRANET's own documented minimum; higher than the GPU DRA driver's
1.34.2+) and an NRI-enabled container runtime (containerd 1.7+ or CRI-O with NRI enabled) on every
node — neither this chart nor Network Operator configures the runtime for NRI.

`gpu-operator.operator.cleanupCRD` defaults to `false` upstream, and the `ClusterPolicy`/`GPUCluster`
CR is only protected with a `helm.sh/resource-policy: keep` annotation when that value is `true`. On
an existing installation, switching stacks will therefore delete the unprotected CR through normal
Helm prune-on-diff behavior, which cascades into the GPU Operator tearing down and replacing the
driver/toolkit/device-plugin stack on every GPU node; it also changes how workloads request RDMA
NICs (extended resource `nscale.com/rdmashare` → `ResourceClaim` against the `rdma.nscale.com`
`DeviceClass` this chart creates). This converges rather than failing outright, but is materially
disruptive to a running cluster — NVIDIA recommends performing this switch on a fresh cluster
instead of upgrading in place.

DRANET only exposes RDMA devices that the host kernel already has configured; it does not install
the RDMA driver itself, so `nicClusterPolicy.ofedDriver` stays enabled regardless of which NIC stack
is active. DRANET moves a claimed interface, including any IP already configured on it, into the
requesting pod's network namespace; it automatically excludes the interface holding the node's
default route, but not other host-reserved RDMA interfaces (e.g. one also carrying storage
traffic) — exclude those explicitly via `--set dranet.args.filter='<CEL expression>'` if applicable.

Workloads request GPU/NIC alignment themselves via a `ResourceClaimTemplate`; see
[DRANET's NVIDIA guide](https://dranet.sigs.k8s.io/docs/user/nvidia-dranet/) for the general
pattern. That pattern is written against the `resource.kubernetes.io/pcieRoot` attribute, which
works as intended on hardware without NVIDIA's ConnectX-8 "Data Direct" feature (e.g. plain
ConnectX-7 nodes). **On Data Direct hardware (GB200/GB300, and upcoming VR-generation platforms),
GPU/NIC topology alignment is blocked upstream**: the GPU and NIC are not colocated under one PCI
switch the way plain ConnectX-7 setups are, and there is no standard attribute that correctly
expresses the resulting topology yet (tracked in
[dra-driver-nvidia-gpu#1123](https://github.com/kubernetes-sigs/dra-driver-nvidia-gpu/issues/1123)).
`resource.kubernetes.io/numaNode` is not a working substitute today either: the DRANET version this
chart pins (chart v1.4.0) only publishes its own `dra.net/numaNode` attribute, not the standardized
one that the pinned GPU DRA driver (v0.5.0) uses, and a `matchAttribute` constraint requires the
identical fully-qualified attribute name from both drivers. Until upstream resolves this, the
closest working approximation is a per-request CEL selector directly on DRANET's own attribute
(e.g. `device.attributes["dra.net"].numaNode == 0`), pinning a specific NUMA node per node pool
rather than portably matching a GPU claim to a NIC claim. Whole-node claims are unaffected by this
limitation, since a `ResourceClaimTemplate` that consumes every GPU and every NIC on a node has no
other claimant to mismatch against.

Switching from the RDMA shared device plugin to DRANET also changes the capacity model: the
plugin's `rdmaHcaMax` (8 by default) lets multiple pods share one HCA, while DRA allocates each NIC
to exactly one consumer.

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| https://helm.ngc.nvidia.com/nvidia | gpu-operator | v26.7.0 |
| https://helm.ngc.nvidia.com/nvidia | network-operator | 26.4.1 |
| oci://registry.k8s.io/networking/charts | dranet | v1.4.0 |
| oci://registry.k8s.io/nfd/charts | node-feature-discovery | 0.19.0 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| dranet.tolerations | list | `[{"operator":"Exists"}]` | Tolerations for DRANET DaemonSet pods |
| dranetDeviceClass.enabled | bool | `false` | Enable the DRANET subchart and the DeviceClass workloads reference in ResourceClaims/ResourceClaimTemplates for RDMA-capable NICs |
| dranetDeviceClass.excludePciSlots | list | `["bb:00"]` | PCI bus:device pairs (the first two segments of a PCI address, e.g. "bb:00" in "0000:bb:00.0") to exclude from the RDMA DeviceClass selector. NVIDIA's HGX/DGX B200 baseboards expose ConnectX-based PCI functions that Fabric Manager uses to manage the on-baseboard NVSwitches (see the Fabric Manager User Guide's "SMDL" VPD field); these report rdma=true like the real backend fabric NICs but aren't network devices. Their VPD differentiates them, but DRANET doesn't currently surface VPD data. The PCI bus:device assignment is fixed by the GPU baseboard's PCIe topology -- the Fabric Manager guide documents this as "a fixed PCIe BDF assignment... as part of the GPU baseboard", and it's been confirmed consistent (bus:device "bb:00") across two different baseboard vendors -- so this excludes them by PCI slot as a portable stopgap. Replace with a VPD- or topology-derived attribute (e.g. a DRANET webhook provider) once available. |
| dranetDeviceClass.name | string | `"rdma.nscale.com"` | Name of the DeviceClass created for RDMA-capable NICs |
| gpu-operator.ccManager.defaultMode | string | `"off"` | Default CC mode applied to compatible GPUs (on/off/devtools) |
| gpu-operator.ccManager.enabled | bool | `false` | Deploy the Confidential Computing manager. Pinned to false: the subchart flipped this default to true in v26.3.x without a release note. |
| gpu-operator.cdi.enabled | bool | `true` | Enable Container Device Interface |
| gpu-operator.daemonsets.tolerations | list | `[{"operator":"Exists"}]` | Tolerations for GPU Operator DaemonSets |
| gpu-operator.driver.enabled | bool | `true` | Enable GPU driver |
| gpu-operator.driver.rdma.enabled | bool | `false` | Enable GPUDirect RDMA support in the GPU driver |
| gpu-operator.driver.rdma.useHostMofed | bool | `false` | Use MOFED drivers pre-installed on the host |
| gpu-operator.driver.upgradePolicy.autoUpgrade | bool | `true` | Enable automatic driver upgrades |
| gpu-operator.driver.upgradePolicy.drain.deleteEmptyDir | bool | `true` | Delete emptyDir volumes on drain |
| gpu-operator.driver.upgradePolicy.drain.enable | bool | `true` | Drain nodes before driver upgrade |
| gpu-operator.driver.upgradePolicy.drain.force | bool | `true` | Force drain (evict even without controllers) |
| gpu-operator.driver.upgradePolicy.drain.podSelector | string | `""` | Label selector for pods to drain (empty = all) |
| gpu-operator.driver.upgradePolicy.drain.timeoutSeconds | int | `300` | Drain timeout in seconds |
| gpu-operator.driver.upgradePolicy.maxParallelUpgrades | int | `1` | Max nodes upgraded in parallel |
| gpu-operator.enabled | bool | `true` | Enable GPU Operator subchart |
| gpu-operator.nfd.enabled | bool | `false` | Deploy NFD from GPU Operator (disabled, using standalone) |
| gpu-operator.nfd.nodefeaturerules | bool | `true` | Enable GPU Operator NodeFeatureRules |
| gpuNodeConfig.enabled | bool | `true` | Enable GPU node config DaemonSet |
| gpuNodeConfig.image | string | `"ubuntu:24.04"` | Container image for the node config DaemonSet |
| gpuNodeConfig.maxUnavailable | int | `1` | Max unavailable nodes during rolling update |
| gpuNodeConfig.nodeSelector | object | `{"nvidia.com/gpu.present":"true"}` | Node selector for GPU config pods |
| gpuNodeConfig.tolerations | list | `[{"operator":"Exists"}]` | Tolerations for GPU config pods |
| network-operator.enabled | bool | `true` | Enable Network Operator subchart |
| network-operator.nfd.enabled | bool | `false` | Deploy NFD from Network Operator (disabled, using standalone) |
| network-operator.operator.fullnameOverride | string | `"network-operator"` | Override the fullname to avoid including the release name |
| nicClusterPolicy.enabled | bool | `true` | Enable NIC Cluster Policy |
| nicClusterPolicy.ofedDriver.enabled | bool | `true` | Enable OFED driver |
| nicClusterPolicy.ofedDriver.env | list | `[{"name":"UNLOAD_STORAGE_MODULES","value":"true"}]` | Environment variables for the driver pod |
| nicClusterPolicy.ofedDriver.forcePrecompiled | bool | `false` | Force use of precompiled driver |
| nicClusterPolicy.ofedDriver.image | string | `"doca-driver"` | OFED driver image name |
| nicClusterPolicy.ofedDriver.livenessProbe | object | `{"initialDelaySeconds":30,"periodSeconds":30}` | Liveness probe for the driver pods |
| nicClusterPolicy.ofedDriver.readinessProbe | object | `{"initialDelaySeconds":10,"periodSeconds":30}` | Readiness probe for the driver pods |
| nicClusterPolicy.ofedDriver.repository | string | `"nvcr.io/nvidia/mellanox"` | Image repository |
| nicClusterPolicy.ofedDriver.startupProbe | object | `{"initialDelaySeconds":10,"periodSeconds":20}` | Startup probe for the driver pods |
| nicClusterPolicy.ofedDriver.terminationGracePeriodSeconds | int | `300` | Termination grace period in seconds |
| nicClusterPolicy.ofedDriver.upgradePolicy.autoUpgrade | bool | `true` | Enable automatic driver upgrades |
| nicClusterPolicy.ofedDriver.upgradePolicy.drain.deleteEmptyDir | bool | `true` | Delete emptyDir volumes on drain |
| nicClusterPolicy.ofedDriver.upgradePolicy.drain.enable | bool | `true` | Drain nodes before driver upgrade |
| nicClusterPolicy.ofedDriver.upgradePolicy.drain.force | bool | `true` | Force drain (evict even without controllers) |
| nicClusterPolicy.ofedDriver.upgradePolicy.drain.podSelector | string | `""` | Label selector for pods to drain (empty = all) |
| nicClusterPolicy.ofedDriver.upgradePolicy.drain.timeoutSeconds | int | `300` | Drain timeout in seconds |
| nicClusterPolicy.ofedDriver.upgradePolicy.maxParallelUpgrades | int | `1` | Max nodes upgraded in parallel |
| nicClusterPolicy.ofedDriver.upgradePolicy.safeLoad | bool | `false` | Enable safe driver loading |
| nicClusterPolicy.ofedDriver.version | string | `"doca3.4.1-26.04-1.1.0.0-1"` | Image version tag |
| nicClusterPolicy.rdmaSharedDevicePlugin.configs | list | `[{"rdmaHcaMax":8,"resourceName":"rdmashare","resourcePrefix":"nscale.com","selectors":{"deviceIDs":["1021","1023"],"vendors":["15b3"]}}]` | RDMA shared device plugin config list |
| nicClusterPolicy.rdmaSharedDevicePlugin.enabled | bool | `true` | Enable RDMA shared device plugin |
| nicClusterPolicy.rdmaSharedDevicePlugin.image | string | `"k8s-rdma-shared-dev-plugin"` | RDMA shared device plugin image name |
| nicClusterPolicy.rdmaSharedDevicePlugin.repository | string | `"nvcr.io/nvidia/mellanox"` | Image repository |
| nicClusterPolicy.rdmaSharedDevicePlugin.useCdi | bool | `true` | Enable Container Device Interface (CDI) for the RDMA shared device plugin |
| nicClusterPolicy.rdmaSharedDevicePlugin.version | string | `"network-operator-v26.4.1"` | Image version tag |
| node-feature-discovery.enabled | bool | `true` | Enable Node Feature Discovery subchart |
| node-feature-discovery.fullnameOverride | string | `"node-feature-discovery"` | Override the fullname to avoid including the release name |
| node-feature-discovery.gc.enable | bool | `true` | Enable NFD garbage collector |
| node-feature-discovery.gc.replicaCount | int | `1` | Number of garbage collector replicas |
| node-feature-discovery.master.config.extraLabelNs | list | `["nvidia.com"]` | Extra label namespaces for NFD master |
| node-feature-discovery.priorityClassName | string | `"system-cluster-critical"` | Priority class for NFD pods |
| node-feature-discovery.worker.config.sources.pci.deviceClassWhitelist | list | `["02","03","0b40","12"]` | PCI device classes to detect |
| node-feature-discovery.worker.config.sources.pci.deviceLabelFields | list | `["vendor"]` | PCI fields to use as labels |
| node-feature-discovery.worker.tolerations | list | `[{"operator":"Exists"}]` | Tolerations for NFD worker pods |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.14.2](https://github.com/norwoodj/helm-docs/releases/v1.14.2)
