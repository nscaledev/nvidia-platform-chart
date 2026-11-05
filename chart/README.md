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
| [NVSentinel](https://github.com/NVIDIA/NVSentinel) | GPU fault detection (opt-in, monitor-only): reports GPU faults as node conditions, Events and labels |

## Prerequisites

- Kubernetes 1.32+
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
request a GPU and a NIC aligned to the same PCIe root or NUMA node via a constraint on a
`ResourceClaimTemplate`:

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
`pcieRoot` does not align GPUs and NICs**: the GPU and NIC are not colocated under one PCI switch
the way plain ConnectX-7 setups are, and there is no standard attribute that expresses the resulting
topology yet (tracked in
[dra-driver-nvidia-gpu#1123](https://github.com/kubernetes-sigs/dra-driver-nvidia-gpu/issues/1123)).
On that hardware, constrain on `resource.kubernetes.io/numaNode` instead: both the GPU DRA driver
(v0.5.1) and DRANET (chart v1.5.0) pinned here publish it, so a `matchAttribute` constraint on it
aligns a GPU claim and a NIC claim to the same NUMA node. This is coarser than PCIe-root alignment —
any NIC on the GPU's NUMA node matches, not only the topologically closest one. Whole-node claims
need no constraint at all, since a `ResourceClaimTemplate` that consumes every GPU and every NIC on
a node has no other claimant to mismatch against.

Upgrading DRANET from chart v1.4.0 to v1.5.0 adds a host mount for `/var/run/dranet`, where DRANET
persists pod device configuration. Stop pods that use DRANET-managed devices before upgrading, or
their state is orphaned inside the old container filesystem.

Switching from the RDMA shared device plugin to DRANET also changes the capacity model: the
plugin's `rdmaHcaMax` (8 by default) lets multiple pods share one HCA, while DRA allocates each NIC
to exactly one consumer.

## DCGM Exporter Metrics

The chart ships a platform-owned dcgm-exporter field list in
[files/dcgm-metrics.csv](files/dcgm-metrics.csv) and renders it into a ConfigMap on every cluster,
in both the `ClusterPolicy` and `GPUCluster` stacks. The GPU Operator uses it in place of the
exporter's default list (`dcp-metrics-included.csv`) rather than merging the two, so the file keeps
all 26 entries of that default (dcgm-exporter 4.8.4, as bundled by GPU Operator v26.7.2) and adds
the fields Blackwell fault detection needs: ECC errors, row-remap pending and inforom validity,
clock-event reasons and slowdown durations, power and thermal violations, slowdown/shutdown
temperatures and the enforced power limit, the XID error count, DCGM health status, the GPU
recovery action, NVLink 5 error, recovery and bit-error-rate counters, NVLink fabric status and
health, and Grace-Blackwell C2C link errors. The exporter silently skips fields a GPU doesn't report
(e.g. C2C on B200); the pre-Blackwell NVLink error counters are deliberately left out because
Blackwell doesn't populate them.

The ConfigMap's name, set in `gpu-operator.dcgmExporter.config.name`, must be `nscale-dcgm-metrics-`
followed by the first 8 hex characters of the file's sha256, and rendering fails until it matches:

```bash
sha256sum chart/files/dcgm-metrics.csv | cut -c1-8
```

The GPU Operator mounts the list into the exporter via `subPath`, which never picks up ConfigMap
updates, so the new name on every edit is what rolls the exporter DaemonSet onto the new list.

dcgm-exporter refuses to start if the list contains a field name it doesn't recognise, so check any
new name against the exporter version the GPU Operator bundles. When upgrading the GPU Operator,
re-check the list against the new exporter's `dcp-metrics-included.csv` and keep it a superset.

## Enabling NVSentinel GPU and NIC fault detection

[NVSentinel](https://github.com/NVIDIA/NVSentinel) is off by default and requires Kubernetes 1.34+
(NVSentinel's own documented minimum). Its gpu-health-monitor needs GPU Operator's standalone DCGM
hostengine, which [values-dra.yaml](values-dra.yaml) enables but the default `ClusterPolicy` mode
does not, so the render fails unless DCGM is enabled too:

```bash
# GPUCluster (DRA) mode
helm install nvidia-platform ./chart --namespace nvidia-platform --create-namespace \
  -f chart/values-dra.yaml --set nvsentinel.enabled=true

# Default ClusterPolicy mode
helm install nvidia-platform ./chart --namespace nvidia-platform --create-namespace \
  --set nvsentinel.enabled=true --set gpu-operator.dcgm.enabled=true
```

NVSentinel is monitor-only: it reports GPU and NIC faults as node conditions, Events in the
`default` namespace, `nvsentinel.dgxc.nvidia.com/*` node labels and the `dgxc.nvidia.com/devices`
annotation on GPU pods. It never cordons, drains or evicts; its acting components are pinned off.

Every health signal that works without a datastore is on: DCGM GPU health, including the
power-brake and thermal-margin checks; kernel-log XID, SXID, GPU-fallen-off and mlx5 NIC driver
errors; InfiniBand and RoCE NIC port state, character devices and error counters; and nodes
NotReady for over 2 hours. A few signals are deliberately off:

- **DCGM power and thermal throttling codes.** They follow load, not faults, so they would fire on
  every busy GPU. See `nvsentinel.gpu-health-monitor.dcgmHealthCheck.suppressedErrorCodes`.
- **DCGM IMEX health.** B200 has no IMEX, and on GB300 the DRA driver tears IMEX down when it's idle.
- **Signals that need more than this chart deploys.** health-events-analyzer needs a datastore,
  preflight needs cert-manager and modifies tenant GPU pods, and the NVCRE certification, CSP and
  Slurm monitors depend on NVCRE, a public cloud or Slurm respectively.

The NIC monitor reads each NIC's RDMA device (`mlx5_*`) from host sysfs, which needs the host's
RDMA subsystem in its default shared netns mode. When DRANET gives a NIC to a pod it moves only the
NIC's network interface, leaving the RDMA device visible on the host, so InfiniBand monitoring isn't
affected. In exclusive mode DRANET moves the RDMA device too, and every claim would be reported as a
fatal device disappearance. The Ethernet (RoCE) checks only act on Ethernet ports. On RoCE fabrics,
check before rollout that moving an interface into a pod doesn't briefly take the port down. Also
note that while a NIC is claimed, its interface statistics counters aren't monitored.

In DRA mode, metadata-collector requests an admin-access DRA GPU claim instead of using the `nvidia`
RuntimeClass, which GPUCluster mode doesn't have. That needs `nvsentinel.global.gpuDraEnabled=true`:
values-dra.yaml sets it, and the render fails without it, so set it yourself if you configure
GPUCluster mode without values-dra.yaml. The API server only admits the claim in a namespace
labelled `resource.kubernetes.io/admin-access=true`. GPU Operator adds that
label only after the install, so a pre-install/pre-upgrade hook Job labels the release namespace
first. The label grants namespace-wide DRA admin access and is never removed.

Its node DaemonSets run at `system-node-critical` priority, so on a full GPU node they can preempt
tenant pods. They request about 600m CPU / 1Gi per GPU node (platform-connectors 200m/512Mi;
gpu-health-monitor, syslog-health-monitor, nic-health-monitor and metadata-collector 100m/128Mi
each). metadata-collector uses host networking and binds node port 2112. kubernetes-object-monitor
adds one 100m/128Mi pod on a GPU node, since it publishes through a platform-connectors socket.

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| https://helm.ngc.nvidia.com/nvidia | gpu-operator | v26.7.2 |
| https://helm.ngc.nvidia.com/nvidia | network-operator | 26.7.1 |
| oci://ghcr.io/nvidia | nvsentinel | v1.30.0 |
| oci://registry.k8s.io/networking/charts | dranet | v1.5.0 |
| oci://registry.k8s.io/nfd/charts | node-feature-discovery | 0.20.1 |

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
| gpu-operator.dcgmExporter.config.name | string | `"nscale-dcgm-metrics-d6de0dba"` | ConfigMap holding the platform-owned dcgm-exporter field list (files/dcgm-metrics.csv), which replaces the exporter's default list. The chart renders this ConfigMap while the name starts with nscale-dcgm-metrics-, and the suffix must be the first 8 hex characters of the file's sha256 (from `sha256sum chart/files/dcgm-metrics.csv`); rendering fails otherwise. Update it with every edit to the file so the exporter pods roll onto the new list. |
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
| nicClusterPolicy.ofedDriver.version | string | `"doca3.5.0-26.07-0.7.7.0-1"` | Image version tag |
| nicClusterPolicy.rdmaSharedDevicePlugin.configs | list | `[{"rdmaHcaMax":8,"resourceName":"rdmashare","resourcePrefix":"nscale.com","selectors":{"deviceIDs":["1021","1023"],"vendors":["15b3"]}}]` | RDMA shared device plugin config list |
| nicClusterPolicy.rdmaSharedDevicePlugin.enabled | bool | `true` | Enable RDMA shared device plugin |
| nicClusterPolicy.rdmaSharedDevicePlugin.image | string | `"k8s-rdma-shared-dev-plugin"` | RDMA shared device plugin image name |
| nicClusterPolicy.rdmaSharedDevicePlugin.repository | string | `"nvcr.io/nvidia/mellanox"` | Image repository |
| nicClusterPolicy.rdmaSharedDevicePlugin.useCdi | bool | `true` | Enable Container Device Interface (CDI) for the RDMA shared device plugin |
| nicClusterPolicy.rdmaSharedDevicePlugin.version | string | `"network-operator-v26.7.1"` | Image version tag |
| node-feature-discovery.enabled | bool | `true` | Enable Node Feature Discovery subchart |
| node-feature-discovery.fullnameOverride | string | `"node-feature-discovery"` | Override the fullname to avoid including the release name |
| node-feature-discovery.gc.enable | bool | `true` | Enable NFD garbage collector |
| node-feature-discovery.gc.replicaCount | int | `1` | Number of garbage collector replicas |
| node-feature-discovery.master.config.extraLabelNs | list | `["nvidia.com"]` | Extra label namespaces for NFD master |
| node-feature-discovery.priorityClassName | string | `"system-cluster-critical"` | Priority class for NFD pods |
| node-feature-discovery.worker.config.sources.pci.deviceClassWhitelist | list | `["02","03","0b40","12"]` | PCI device classes to detect |
| node-feature-discovery.worker.config.sources.pci.deviceLabelFields | list | `["vendor"]` | PCI fields to use as labels |
| node-feature-discovery.worker.tolerations | list | `[{"operator":"Exists"}]` | Tolerations for NFD worker pods |
| nvsentinel.enabled | bool | `false` | Enable the NVSentinel subchart. Monitor-only: it reports GPU and NIC faults as node conditions, Events and labels, and never cordons, drains, evicts or reboots. Requires gpu-operator.dcgm.enabled=true (values-dra.yaml sets it; the classic default does not), and in GPUCluster mode also nvsentinel.global.gpuDraEnabled=true (values-dra.yaml sets it too); the render fails without them |
| nvsentinel.global.dcgm.service.endpoint | string | `"nvidia-dcgm-dra,nvidia-dcgm"` | GPU Operator's standalone DCGM Services, tried in order (nvidia-dcgm-dra in GPUCluster mode, nvidia-dcgm in ClusterPolicy mode). Short names resolve in the release namespace; the upstream default hard-codes the gpu-operator namespace |
| nvsentinel.global.faultQuarantine.enabled | bool | `false` | Pinned off: cordons and taints faulty nodes |
| nvsentinel.global.faultRemediation.enabled | bool | `false` | Pinned off: remediates (e.g. reboots) faulty nodes |
| nvsentinel.global.gpuDraEnabled | bool | `false` | Set true (as values-dra.yaml does) in GPUCluster/DRA mode: metadata-collector then uses an admin-access DRA GPU claim instead of the nvidia RuntimeClass, and this chart labels its namespace for DRA admin access in a pre-install/pre-upgrade hook |
| nvsentinel.global.janitor.enabled | bool | `false` | Pinned off: executes remediation actions |
| nvsentinel.global.janitorProvider.enabled | bool | `false` | Pinned off: CSP provider for janitor remediation actions |
| nvsentinel.global.kubernetesObjectMonitor.enabled | bool | `true` | Report nodes NotReady for over 2 hours |
| nvsentinel.global.lifecycleManager.enabled | bool | `false` | Pinned off: reconciles node maintenance requests |
| nvsentinel.global.metadataCollector.enabled | bool | `true` | Pinned on: maps faults to the specific GPU and the pods using it |
| nvsentinel.global.nicHealthMonitor.enabled | bool | `true` | Monitor InfiniBand and RoCE NIC port state, character devices and error counters from host sysfs. Requires the host's RDMA subsystem in shared netns mode (the kernel default): in exclusive mode DRANET moves each claimed mlx5 device out of the host's view, which this monitor reports as a fatal device disappearance |
| nvsentinel.global.nodeDrainer.enabled | bool | `false` | Pinned off: drains faulty nodes |
| nvsentinel.global.priorityClassName | string | `"system-node-critical"` | Priority class for NVSentinel's node DaemonSets |
| nvsentinel.gpu-health-monitor.dcgmConnectivity.runtimeDebounce.failureThreshold | int | `3` | Consecutive failed DCGM polls before reporting a connectivity failure |
| nvsentinel.gpu-health-monitor.dcgmConnectivity.startupGate.enabled | bool | `true` | Wait for DCGM before starting, avoiding a false fatal GpuDcgmConnectivityFailure on every boot and DCGM restart |
| nvsentinel.gpu-health-monitor.dcgmFieldsMonitoring.gpuPowerBrakeMonitoringEnabled | bool | `true` | Report GPUs whose hardware power brake is forcing clocks down |
| nvsentinel.gpu-health-monitor.dcgmFieldsMonitoring.gpuPowerBrakeStoreOnly | bool | `false` | Publish power-brake events. Upstream's store-only default sends them only to the datastore, which this chart doesn't deploy, so they would never be seen |
| nvsentinel.gpu-health-monitor.dcgmFieldsMonitoring.gpuTempLimitStoreOnly | bool | `false` | Publish GPU thermal-margin (T.Limit) events, for the same reason |
| nvsentinel.gpu-health-monitor.dcgmHealthCheck.suppressedErrorCodes | list | `["DCGM_FR_CLOCK_THROTTLE_POWER","DCGM_FR_CLOCKS_EVENT_POWER","DCGM_FR_CLOCK_THROTTLE_THERMAL","DCGM_FR_CLOCKS_EVENT_THERMAL","DCGM_FR_NVLINK_EFFECTIVE_BER_THRESHOLD","DCGM_FR_NVLINK_SYMBOL_BER_THRESHOLD"]` | DCGM codes never reported. Restates upstream's list so the choice is explicit. The POWER and THERMAL throttling codes are raised whenever a busy GPU reaches its power or thermal cap: DCGM's recommended action for them is NONE and they follow load rather than faults, so reporting them would flood Events on healthy, busy GPUs. Real power and thermal faults still surface through the power-brake and thermal-margin checks and DCGM's other codes. Each throttling code is listed under both its current and deprecated name. The NVLink BER codes were removed in DCGM 4.7.0 because they failed healthy GPUs; only older hostengines still report them |
| nvsentinel.kubernetes-object-monitor.affinity | object | `{"nodeAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"nodeSelectorTerms":[{"matchExpressions":[{"key":"nvsentinel.dgxc.nvidia.com/driver.installed","operator":"In","values":["true"]}]}]}}}` | Run beside a platform-connectors pod: it publishes through that pod's node-local socket, and platform-connectors only runs on GPU nodes (see platformConnector.affinity) |
| nvsentinel.networkPolicy.enabled | bool | `false` | Create NVSentinel's metrics-access NetworkPolicy. Off because it selects every pod in the release namespace and would block DCGM (port 5555) |
| nvsentinel.platformConnector.affinity | object | `{"nodeAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"nodeSelectorTerms":[{"matchExpressions":[{"key":"nvsentinel.dgxc.nvidia.com/driver.installed","operator":"In","values":["true"]}]}]}}}` | Keep platform-connectors on GPU nodes. NVSentinel's labeler sets this label once a driver pod is Ready; if it never appears, see nvsentinel.labeler.driverAppLabel |
| nvsentinel.podMonitor.enabled | bool | `false` | Create NVSentinel's PodMonitor. Off because it renders without checking for the Prometheus Operator CRDs, which would fail installs on clusters without them |
| nvsentinel.syslog-health-monitor.enabledChecks | list | `["SysLogsXIDError","SysLogsSXIDError","SysLogsGPUFallenOff","SysLogsNICDriverError"]` | Kernel-log checks. Upstream's three GPU checks plus the mlx5 NIC driver check; Helm replaces lists, so upstream's defaults are restated |
| nvsentinel.syslog-health-monitor.nicDriverDetection.patterns | list | `[{"enabled":true,"name":"cmd_exec_timeout"},{"enabled":true,"name":"health_poll_failed"},{"enabled":true,"name":"unrecoverable_err"},{"enabled":true,"name":"netdev_watchdog"},{"enabled":true,"name":"pci_power_insufficient"},{"enabled":true,"name":"port_module_high_temp"},{"enabled":true,"name":"access_reg_failed"},{"enabled":true,"name":"module_unplugged"},{"enabled":true,"name":"mlx5_tx_timeout_detected"},{"enabled":true,"name":"mlx5_rx_timeout_detected"},{"enabled":true,"name":"mlx5_napi_soft_lockup"}]` | mlx5 kernel-log patterns for SysLogsNICDriverError, all enabled (upstream ships them all disabled). Regexes, severities and actions are defined upstream |
| nvsentinelNamespaceLabel.image | string | `"registry.k8s.io/kubectl:v1.36.5"` | kubectl image for the pre-install/pre-upgrade hook Job that labels the release namespace resource.kubernetes.io/admin-access=true |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.14.2](https://github.com/norwoodj/helm-docs/releases/v1.14.2)
