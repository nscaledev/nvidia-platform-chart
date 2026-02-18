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

## Prerequisites

- Kubernetes 1.27+
- Helm 3.12+
- Nodes with NVIDIA GPUs and/or Mellanox ConnectX NICs

## Installation

```bash
# Add the NVIDIA Helm repository
helm repo add nvidia https://helm.ngc.nvidia.com/nvidia

# Build subchart dependencies
helm dependency build ./chart

# Install
helm install nvidia-platform ./chart --namespace nvidia-platform --create-namespace
```

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| https://helm.ngc.nvidia.com/nvidia | gpu-operator | v25.10.1 |
| https://helm.ngc.nvidia.com/nvidia | network-operator | 25.10.0 |
| oci://registry.k8s.io/nfd/charts | node-feature-discovery | 0.18.3 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| gpu-operator.cdi.enabled | bool | `true` | Enable Container Device Interface |
| gpu-operator.daemonsets.tolerations | list | `[{"operator":"Exists"}]` | Tolerations for GPU Operator DaemonSets |
| gpu-operator.driver.enabled | bool | `true` | Enable GPU driver |
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
| nicClusterPolicy.ofedDriver.version | string | `"doca3.2.0-25.10-1.2.8.0-2"` | Image version tag |
| nicClusterPolicy.rdmaSharedDevicePlugin.configs | list | `[{"rdmaHcaMax":8,"resourceName":"rdmashare","resourcePrefix":"nscale.com","selectors":{"deviceIDs":["1021","1023"],"vendors":["15b3"]}}]` | RDMA shared device plugin config list |
| nicClusterPolicy.rdmaSharedDevicePlugin.enabled | bool | `true` | Enable RDMA shared device plugin |
| nicClusterPolicy.rdmaSharedDevicePlugin.image | string | `"k8s-rdma-shared-dev-plugin"` | RDMA shared device plugin image name |
| nicClusterPolicy.rdmaSharedDevicePlugin.repository | string | `"nvcr.io/nvidia/mellanox"` | Image repository |
| nicClusterPolicy.rdmaSharedDevicePlugin.version | string | `"network-operator-v25.10.0"` | Image version tag |
| node-feature-discovery.enabled | bool | `true` | Enable Node Feature Discovery subchart |
| node-feature-discovery.gc.enabled | bool | `true` | Enable NFD garbage collector |
| node-feature-discovery.gc.replicaCount | int | `1` | Number of garbage collector replicas |
| node-feature-discovery.master.config.extraLabelNs | list | `["nvidia.com"]` | Extra label namespaces for NFD master |
| node-feature-discovery.priorityClassName | string | `"system-cluster-critical"` | Priority class for NFD pods |
| node-feature-discovery.worker.config.sources.pci.deviceClassWhitelist | list | `["02","03","0b40","12"]` | PCI device classes to detect |
| node-feature-discovery.worker.config.sources.pci.deviceLabelFields | list | `["vendor"]` | PCI fields to use as labels |
| node-feature-discovery.worker.tolerations | list | `[{"operator":"Exists"}]` | Tolerations for NFD worker pods |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.14.2](https://github.com/norwoodj/helm-docs/releases/v1.14.2)
