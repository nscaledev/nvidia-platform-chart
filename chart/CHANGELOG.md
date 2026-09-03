# Changelog

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
