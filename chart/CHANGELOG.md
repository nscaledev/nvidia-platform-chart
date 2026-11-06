# Changelog

## [0.5.0](https://github.com/nscaledev/nvidia-platform-chart/compare/nvidia-platform-0.4.0...nvidia-platform-0.5.0) (2026-11-06)


### Features

* **chart:** add opt-in monitor-only NVSentinel ([4572b1f](https://github.com/nscaledev/nvidia-platform-chart/commit/4572b1f33369eafd9b34fb3ef2a3307859c03e98))
* **chart:** add opt-in monitor-only NVSentinel ([5e8d0ea](https://github.com/nscaledev/nvidia-platform-chart/commit/5e8d0ea8aa1f2bb06072823dff10ace931218951))
* **chart:** enable all NVSentinel health signals ([b17f34c](https://github.com/nscaledev/nvidia-platform-chart/commit/b17f34cdb97ebb5b7e40182e25092766fec3d584))
* **chart:** fail the render when NVSentinel lacks gpuDraEnabled in GPUCluster mode ([2646f13](https://github.com/nscaledev/nvidia-platform-chart/commit/2646f131fb3e339100bec380c2535fafe27d2b1d))


### Bug Fixes

* **chart:** keep NVSentinel's kubernetes-object-monitor off ([23a5ecf](https://github.com/nscaledev/nvidia-platform-chart/commit/23a5ecf6b996dcbbe60b8e23a78765cb5588b3f7))
* **chart:** label NVSentinel's namespace for DRA admin access only with metadata-collector ([a459e4e](https://github.com/nscaledev/nvidia-platform-chart/commit/a459e4e0e3be87480b5f16ea9882363a6f80785f))
* **chart:** run NVSentinel's kubernetes-object-monitor on GPU nodes again ([4df6a45](https://github.com/nscaledev/nvidia-platform-chart/commit/4df6a459926c8e514b773836475222ee63a7f595))


### Documentation

* **chart:** record what validating NVSentinel on B200 showed ([3389178](https://github.com/nscaledev/nvidia-platform-chart/commit/33891784f6ef7ed4275fa7dc5adf97e741e822ab))
* **chart:** state NVSentinel's Kubernetes 1.34+ requirement ([cb94be6](https://github.com/nscaledev/nvidia-platform-chart/commit/cb94be6ca5c199377ccec4bdbea3580e3a9d63b5))

## [0.4.0](https://github.com/nscaledev/nvidia-platform-chart/compare/nvidia-platform-0.3.2...nvidia-platform-0.4.0) (2026-10-29)


### Features

* **chart:** ship platform-owned DCGM field list ([1061fd6](https://github.com/nscaledev/nvidia-platform-chart/commit/1061fd6465ce75651b40fdf4e5a6820314efa44a))
* **chart:** ship platform-owned DCGM field list ([183c408](https://github.com/nscaledev/nvidia-platform-chart/commit/183c40886c667621a66ce0b17c10d2c87683104a))

## [0.3.2](https://github.com/nscaledev/nvidia-platform-chart/compare/nvidia-platform-0.3.1...nvidia-platform-0.3.2) (2026-10-28)


### Dependencies

* **dranet:** upgrade to v1.5.0 and document NUMA alignment ([6264024](https://github.com/nscaledev/nvidia-platform-chart/commit/62640247b6c4d0b1e166ad3bd4825b5a182ca946))
* upgrade gpu-operator to v26.7.2, network-operator to 26.7.0 ([f21b061](https://github.com/nscaledev/nvidia-platform-chart/commit/f21b061709ff5adbad29edd9100bb3386c4baa49))
* upgrade network-operator to 26.7.1, node-feature-discovery to 0.20.1 ([51750f3](https://github.com/nscaledev/nvidia-platform-chart/commit/51750f373eedd950afefd46b482c76ebcd5e7aa6))

## [0.3.1](https://github.com/nscaledev/nvidia-platform-chart/compare/nvidia-platform-0.3.0...nvidia-platform-0.3.1) (2026-09-30)


### Bug Fixes

* **gpu-node-config:** run scripts via stdin to support noexec /tmp ([9d52cb1](https://github.com/nscaledev/nvidia-platform-chart/commit/9d52cb1b6bf8c07d31766b604bd014eb2ea042cb))
* **gpu-node-config:** run scripts via stdin to support noexec /tmp ([5c74466](https://github.com/nscaledev/nvidia-platform-chart/commit/5c74466ab98d0d34d172b27d85e854e1a7e803c8))

## [0.3.0](https://github.com/nscaledev/nvidia-platform-chart/compare/nvidia-platform-0.2.0...nvidia-platform-0.3.0) (2026-09-19)


### Features

* **chart:** add DRANET as a DRA-based alternative for RDMA NICs ([e3cf4f2](https://github.com/nscaledev/nvidia-platform-chart/commit/e3cf4f2d6173a3a563db7439045f79070b4748df))
* **chart:** add DRANET as a DRA-based alternative for RDMA NICs ([31a9e4b](https://github.com/nscaledev/nvidia-platform-chart/commit/31a9e4bc28249512262fdbc2c688272acae05c06))


### Bug Fixes

* **chart:** enable standalone DCGM hostengine ([7a5a773](https://github.com/nscaledev/nvidia-platform-chart/commit/7a5a77375dfdd23dd3ea28c40171f05c246dac7d))


### Documentation

* **chart:** regenerate README for dranetDeviceClass.excludePciSlots ([7f0de0f](https://github.com/nscaledev/nvidia-platform-chart/commit/7f0de0f8a241318fbd3f2ee4ef78f874cab919df))

## [0.2.0](https://github.com/nscaledev/nvidia-platform-chart/compare/nvidia-platform-0.1.2...nvidia-platform-0.2.0) (2026-09-03)


### ⚠ BREAKING CHANGES

* **chart:** on an existing installation, this deletes the unprotected ClusterPolicy CR (gpu-operator.operator.cleanupCRD defaults to false, so it isn't kept), which cascades into the driver/ toolkit/device-plugin stack being torn down and replaced on every GPU node. Classic nvidia.com/gpu resource scheduling stops working until workloads migrate to DRA ResourceClaims. NVIDIA recommends performing this switch on a fresh cluster rather than upgrading in place; see the new "Upgrading" section in the chart README.

### Features

* **chart:** enable CDI for network operator RDMA shared device plugin ([d209763](https://github.com/nscaledev/nvidia-platform-chart/commit/d209763dc5408abee941477067402f1c0d0757d2))
* **chart:** enable CDI for network operator RDMA shared device plugin ([ea4d4f0](https://github.com/nscaledev/nvidia-platform-chart/commit/ea4d4f083022b788d4a705324659661a6c69726b))
* **chart:** switch GPU Operator to the GPUCluster/DRA-managed stack ([3f7b696](https://github.com/nscaledev/nvidia-platform-chart/commit/3f7b69640493274d23d4ddc524d83da508f8e333))


### Bug Fixes

* **chart:** fix ACS/IOMMU handling for boot-time-configured platforms ([dac019a](https://github.com/nscaledev/nvidia-platform-chart/commit/dac019af58f7bf4f71bd90baf7373cbb3f9a572e))
* **chart:** fix ACS/IOMMU handling for boot-time-configured platforms ([83001d7](https://github.com/nscaledev/nvidia-platform-chart/commit/83001d71cb87608c960574a9e4ded10c7004c305))
* **chart:** pin ccManager disabled by default ([389bf79](https://github.com/nscaledev/nvidia-platform-chart/commit/389bf79af4d0769d134df8c4646e087212de387d))
* **chart:** pin network-operator to 26.4.1, the latest published chart ([01afe22](https://github.com/nscaledev/nvidia-platform-chart/commit/01afe2227a4c65fbfd0c34e7bbe025201aa3946f))


### Dependencies

* upgrade GPU/Network operators and NFD to the 26.7 train ([077657b](https://github.com/nscaledev/nvidia-platform-chart/commit/077657b6a0f0af430a5a0f046e1d95a9fce76acf))


### Reverts

* **chart:** default GPU Operator back to the classic ClusterPolicy stack ([ae92ded](https://github.com/nscaledev/nvidia-platform-chart/commit/ae92ded4b4e8fb776a13b246c7db066a8ac34307))

## [0.1.2](https://github.com/nscaledev/nvidia-platform-chart/compare/nvidia-platform-0.1.1...nvidia-platform-0.1.2) (2026-08-03)


### Bug Fixes

* **chart:** expose NVIDIA RDMA controls ([#9](https://github.com/nscaledev/nvidia-platform-chart/issues/9)) ([6d7936f](https://github.com/nscaledev/nvidia-platform-chart/commit/6d7936f399b07588304feef030ad63b71a405ecd))

## [0.1.1](https://github.com/nscaledev/nvidia-platform-chart/compare/nvidia-platform-v0.1.0...nvidia-platform-0.1.1) (2026-02-19)


### Code Refactoring

* **gpu-node-config:** simplify ACS script to disable-only ([#6](https://github.com/nscaledev/nvidia-platform-chart/issues/6)) ([9bae487](https://github.com/nscaledev/nvidia-platform-chart/commit/9bae487aa7021f1c8ed0063bcdf04f298909d692))

## [0.1.0](https://github.com/nscaledev/nvidia-platform-chart/compare/nvidia-platform-v0.0.1...nvidia-platform-v0.1.0) (2026-02-19)


### Features

* **chart:** add NVIDIA platform umbrella Helm chart ([#1](https://github.com/nscaledev/nvidia-platform-chart/issues/1)) ([6d48292](https://github.com/nscaledev/nvidia-platform-chart/commit/6d48292821575b4eeeecdb5b56dde5027277b958))


### Bug Fixes

* **chart:** set initial version to 0.0.1 for release-please ([#5](https://github.com/nscaledev/nvidia-platform-chart/issues/5)) ([0ada956](https://github.com/nscaledev/nvidia-platform-chart/commit/0ada956e0a130211e1ee7ed1c40ad675f6a822f1))
