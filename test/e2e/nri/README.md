# nccl-fabric-nri kind test

Runs the nccl-fabric-nri plugin under a real containerd with NRI, in a
throwaway kind cluster, against fixture `/sys/class/infiniband` trees.

```sh
bash test/e2e/nri/run.sh            # creates, tests, deletes the cluster
NRI_E2E_KEEP=1 bash test/e2e/nri/run.sh   # leave the cluster up for debugging
```

Needs Docker, kind, helm, kubectl, jq and [mikefarah yq](https://github.com/mikefarah/yq) v4
(not the Python yq). If `DOCKER_HOST` is unset and the colima socket
`$HOME/.colima/default/docker.sock` exists, the script uses colima.
A run takes about two minutes.

## Safety

The cluster is `nri-e2e` with its own kubeconfig at `test/e2e/nri/.kubeconfig`
(git-ignored). Every kubectl and helm call passes that kubeconfig and
`--context kind-nri-e2e`, and aborts unless the kubeconfig's current context
is `kind-nri-e2e` and its API server is the kind node's published
`127.0.0.1` port. `KUBECONFIG` is also pointed at that file. `~/.kube/config`
is never read or written.

## What it sets up

- Node image `kindest/node:v1.35.0` (pinned by digest): containerd v2.2.0
  with NRI enabled by default, as on NKS nodes. The script fails if NRI is
  disabled or `/var/run/nri/nri.sock` is missing.
- `fixtures/` is mounted read-only at `/fixtures` in the node. Each fixture is
  a mini sysfs root laid out like the real one: `class/infiniband/mlx5_N` is a
  relative symlink to `../../devices/pci0/infiniband/mlx5_N`, where the port
  files live. The DaemonSet's `ncclFabricNri.hostSysRoot` is the node-local
  `/fixture/sys` (mounted at `/host/sys`, as production mounts the host's
  `/sys`), and the script copies a fixture into it (or empties it) to switch
  layouts. The script checks the symlinks survive the mount.
  - `hfm/`: `0x7fff` at index 0 and tenant `0x8600` at index 1 on two ports,
    plus a management port (`0xffff` only) and an Ethernet port.
  - `handmade/`: tenant `0x8030` at index 0.
  - `no-ib/`: no `class/infiniband` at all (a node without `ib_core`).
- The plugin image is built from `cmd/nccl-fabric-nri/Dockerfile` and loaded
  with `kind load`, and the chart is installed from a copy without subcharts
  (this test deploys only the plugin). The node is labelled
  `nvidia.com/gpu.present=true`.

## Checks

Each pod runs `env`; the script reads `NCCL_IB_PKEY*` from its log.

- Before install, with the HFM fixture in place, a pod has no `NCCL_IB_PKEY`.
- The plugin registers with NRI (logs `Started plugin 90-nccl-fabric-nri`).
- No `ib_core`: the plugin is installed on the `no-ib/` fixture; it starts,
  its state metric is `none`, and a pod gets no `NCCL_IB_PKEY`.
- 1: HFM fixture: the plugin reads `indexed` through the class symlinks, its
  state-change log counts `ports=2`, and a pod gets `NCCL_IB_PKEY=1`.
- 2: `NCCL_IB_PKEY=3` in the pod spec stays `3`; `NCCL_IB_PKEY_VALUE` in the
  pod spec suppresses `NCCL_IB_PKEY`.
- Image ENV: an image with `ENV NCCL_IB_PKEY=5` keeps `5`, and the plugin
  counts it as `user_set`.
- 3: hand-made fixture: the plugin moves to `slot0`, a new pod gets nothing,
  and the state file is removed.
- Unreadable table: the HFM layout with one `pkeys` entry overwritten with
  `zz` makes the plugin read `ambiguous` (after the two-poll rule when it held
  `indexed`), and a pod starts without the variable.
- 4: sticky: back on HFM, then the ports removed, a pod still gets `1`.
  Deleting the plugin pod: it exits 0 on SIGTERM, six pods created during the
  restart all start (those created before the new plugin registers get no
  variable), and the new plugin reloads `indexed` from the state file.
- 5: with the DaemonSet scheduled nowhere, a pod starts without the variable.
- 6: reinstalled, the plugin runs last, after kind's own NRI plugin
  (`10-kube-network-policies`; any other order fails), a pod gets exactly
  `NCCL_IB_PKEY=1`, and there are no NRI-related `Failed` events.
