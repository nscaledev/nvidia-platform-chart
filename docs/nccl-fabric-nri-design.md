# nccl-fabric-nri: pointing NCCL at the tenant InfiniBand partition on NKS nodes

**Status:** draft for review, 2026-11-18.

## Why

hpc-fabric-manager (HFM) isolates InfiniBand per VPC network. When a host passes
its `fabric.unikorn-cloud.org/partition-ready` gate, HFM makes the host's compute
ports full members of the network's p_key. It leaves the default partition
`0x7fff` (limited membership) at **index 0** of each port's p_key table and puts
the tenant p_key at **index 1** (`index0: false`). That layout is deliberate:
SHARP's control connection to the Aggregation Manager uses UCX, which on the
SHARP versions we run only uses the slot-0 p_key. Those versions are SHARP
3.13.0 in UFM 6.23.1 and SHARP 3.11.0 in HPC-X 2.23, and the limitation is a
documented known issue up to 3.14. The network team needs SHARP.

NCCL uses p_key index 0 unless told otherwise. On an HFM-programmed host a
multi-node NCCL job therefore lands on the default partition, where hosts are
limited members and cannot reach each other, and fails after the IB timeout
(`IBV_WC_RETRY_EXC_ERR`, about 71 s). It works once the job sets
`NCCL_IB_PKEY=1`.

NKS v2 GPU node pools are reservation-placed (CAPNS reservation mode), so their
hosts go through HFM's gate once it is on in production. Customers running
multi-node training in pods need this to work without setting anything.

Existing NKS clusters sit on hand-made partitions with the **opposite** layout:
the tenant p_key is at index 0 as a full member (`0x8030`) and `0x7fff` at index
1. NCCL already works there. Setting `NCCL_IB_PKEY=1` on those hosts would point
NCCL at the default partition and break them.

So the right value depends on the node, not the pod. An admission-time
mechanism (a MutatingAdmissionPolicy, #32) cannot know which node a pod will
land on, and on Kubernetes 1.35 that API is beta and off. Other providers mostly
put the tenant at index 0 and need nothing. NVIDIA's own GB200 stack (NVKE)
has our layout and sets `NCCL_IB_PKEY=1` from a launcher script inside the
workload. Nothing off the shelf injects it per node. DRANET, NVIDIA's DRA
drivers and the RDMA device plugins cannot add environment variables.

## What it is

A small NRI plugin, `nccl-fabric-nri`, running as a DaemonSet on GPU nodes. It
works out the node's p_key layout from the node itself, and when containerd
creates a container on an HFM-programmed node it adds `NCCL_IB_PKEY=<the tenant's
index>` unless the container already sets it.

## Goals

- Multi-node NCCL works in pods on HFM-programmed NKS nodes with nothing set by
  the customer.
- Correct by construction on every node layout: HFM-programmed, hand-made, not
  gated, and node pools of different kinds side by side in one cluster.
- Every NKS Kubernetes version (1.35, 1.36, 1.37), with no feature gates.
- A job's own setting always wins.
- Never fails or delays a container because of its own logic. The residual
  risk, a hung plugin process, is bounded below (see "NRI facts").

## Non-goals

- Bare-metal and VM instance images. They get `/etc/nccl.conf` from instance
  metadata (ib-ansible-hardware #53).
- UCX/MPI. UCX's default picks the first full-member p_key, which on an
  HFM host is the tenant, and setting `UCX_IB_PKEY` would move SHARP's control
  path off the default partition. Not set. MPI over UCX is checked on the first
  gated hosts instead.
- Per-workload InfiniBand isolation inside one cluster, which is a separate
  design.
- Writing to the fabric, or to the node.

## Anti-goals: failures even if it "works"

- **Breaking a hand-made cluster.** Any doubt about the node's layout means
  setting nothing.
- **Delaying or failing containers.** The hook answers from in-memory state only:
  no file reads, no network calls, no locks held across I/O. See "NRI facts" for
  why this matters more than it looks.
- **Overriding a user.** A container that sets `NCCL_IB_PKEY` (or
  `NCCL_IB_PKEY_VALUE`) keeps its value.

## Behaviour

### Node state

The plugin reads `/sys/class/infiniband/*/ports/*`. It considers only ports
whose `link_layer` is `InfiniBand` and whose `state` is `ACTIVE`. In each port's
`pkeys/*`, a **tenant entry** is one with the full-member bit (`0x8000`) set
whose p_key, with that bit masked off, is neither `0x0000` nor `0x7fff`. That is
the same rule as hpc-fabric-manager's `contrib/host/nccl-fabric-env.sh`.

The node's state is one of:

| State | Condition | Action |
|---|---|---|
| `indexed` | every active port that has a tenant entry has exactly one, the same p_key at the same index, and that index is not 0 | set `NCCL_IB_PKEY=<that index>` |
| `slot0` | the tenant is at index 0 on those ports (hand-made layout) | set nothing; NCCL's default already works |
| `none` | no active port has a tenant entry (not gated, or ports not up yet) | set nothing |
| `ambiguous` | ports disagree on the p_key or index, or a port has more than one tenant entry | set nothing, log, count |

Ports with no tenant entry are ignored, so the NVLink management functions
(`mlx5_6`–`9` on osl1 B200, which hold only `0x7fff` or `0xffff`) never affect the
result.

The state is recomputed at start-up and then every 5 s, and kept in memory.

**The state is sticky.** Once the node has been seen as `indexed` with p_key P at
index i, later readings that find no tenant entry (`none`) do not downgrade it.
Only a reading that contradicts it (`slot0`, `ambiguous`, or `indexed` with a
different p_key or index) replaces it. The last `indexed` state is also written
to `/var/lib/nccl-fabric-nri/state` on the host and reloaded at start-up. This
protects against devices vanishing from the host's view while pods hold them.
NKS node images leave the RDMA subsystem in its default **shared**
network-namespace mode, where devices stay visible, but under **exclusive** mode
DRANET moves a claimed device into the pod and the host loses sight of it. A
node's layout only really changes when the host is reprovisioned, which also
wipes the state file.
Container creation reads the cached state only. A node whose ports come up after
the plugin starts moves from `none` to its real state within seconds. Pods that
start before then get nothing, as they would without the plugin.

### Per container

On `CreateContainer` the plugin adds `NCCL_IB_PKEY=<index>` when **both** of
these hold:
- the cached state is `indexed`;
- the container's environment sets neither `NCCL_IB_PKEY` nor `NCCL_IB_PKEY_VALUE`.

It applies to every container on the node, not only those given InfiniBand
devices. The value is right for anything that uses InfiniBand there, and
anything else ignores it. This avoids depending on whether DRANET's device
injection is visible to this plugin's hook, which would depend on NRI plugin
ordering.

Any error, whether reading state, encoding the adjustment or anything else,
returns **no adjustment and no error** (`nil, nil, nil` from the stub handler).
Under NRI, an error returned by a plugin fails that container's creation.

### NRI facts the design relies on

These were checked against containerd/nri and DRANET source.

- **Versions:** NKS node images run containerd 2.2.x, which vendors NRI v0.11.
  Only NRI v0.12.5+ lets a plugin register a fail-open policy, so we cannot rely
  on one.
- **A slow or hung plugin fails the container:**
  - in NRI v0.8–v0.11, a plugin that misses the request timeout makes
    containerd fail that container's creation and drop the plugin;
  - containerd holds a lock across all plugins for each `CreateContainer`, so a
    slow plugin delays every container creation on the node;
  - the timeout (10 s on NKS images) applies per plugin, per request, and plugins
    run one after another.

  Hence the in-memory-only hook. The accepted residual risk is a hung plugin
  *process*: one container's creation fails, kubelet retries it, containerd
  drops the plugin, and the plugin's process exits on disconnect and is
  restarted by its DaemonSet, which re-registers it.
- **Ordering:** DRANET registers with index `00`. This plugin uses a higher index,
  and later plugins see earlier plugins' adjustments.
- **Conflicts:** if two NRI plugins set the same environment variable, containerd
  fails the container. Nothing else on NKS GPU nodes sets `NCCL_IB_PKEY` today
  (DRANET sets no env), and the integration test checks this.
- **NCCL:** `NCCL_IB_PKEY_VALUE` exists from NCCL 2.31, so a container setting it
  is treated as having chosen its own p_key.

### Observability

- One log line per state change, giving the state, the p_key and index, and the
  ports counted.
- Prometheus metrics on the node:
  - `nccl_fabric_nri_node_state{state}` (1 for the current state);
  - `nccl_fabric_nri_containers_adjusted_total`;
  - `nccl_fabric_nri_containers_skipped_total{reason}` (`user_set`, `not_indexed`, `error`).
- No writes to the Kubernetes API. The plugin needs no RBAC.

## Packaging and rollout

- **Code:** Go, using containerd's NRI stub library, in `nvidia-platform-chart`
  under `cmd/nccl-fabric-nri/`, with a Dockerfile. The image is published by the
  repo's release workflow next to the chart, e.g.
  `ghcr.io/nscaledev/nccl-fabric-nri`, versioned with the chart.
- **Chart:** a `ncclFabricNri` section, `enabled: false` by default. The
  DaemonSet:
  - runs on GPU nodes (same node selector and tolerations as `gpu-node-config`);
  - mounts the NRI socket (`/var/run/nri/nri.sock`), host `/sys` read-only at
    `/host/sys` (the plugin reads `/host/sys/class/infiniband`; all of `/sys`
    because the class entries are relative symlinks into `/sys/devices`), and
    `/var/lib/nccl-fabric-nri` for the sticky state;
  - runs as root with all capabilities dropped (to write the root-owned state
    directory and reach the root-owned NRI socket), not privileged, with no
    Kubernetes API access or RBAC;
  - registers with an NRI plugin index that orders it after DRANET.
- **Rollout:** enabled through the NKS platform releases' hardware addon values
  (`k8s-deploy-nks`), following NKS's release process: a release candidate for
  prerelease organisations, then general availability. It applies to every
  Kubernetes minor.
- **PR #32** (the MutatingAdmissionPolicy) is closed in favour of this.
- **Until it ships:** customers on HFM-programmed clusters set `NCCL_IB_PKEY=1`
  themselves, from the gate-on documentation.

## Testing

1. **Unit:** node-state detection against fixture sysfs trees:
   - HFM layout (`0x7fff` at index 0, tenant at index 1);
   - hand-made layout (`0x8030` at index 0);
   - management-only ports;
   - ports not ACTIVE;
   - ports that disagree;
   - two tenant p_keys;
   - no InfiniBand.

   Per-container decision: added, user sets `NCCL_IB_PKEY`, user sets
   `NCCL_IB_PKEY_VALUE`, state not `indexed`, error path.
2. **Integration (kind, containerd with NRI):**
   - Run the DaemonSet with a fixture sysfs tree (`class/infiniband` symlinks
     into `devices/`) mounted in place of the host's `/sys`.
   - Create pods and check their environment for the HFM layout, the hand-made
     layout and a user-set value.
   - Check that the hook never returns an error, including when node state is
     unreadable, and that pods start normally with the plugin stopped.
   - Check that the state stays `indexed` after the fixture's ports are removed
     (sticky), and is reloaded after the plugin restarts.
3. **Hardware, at HFM gate-on:**
   - a gated NKS v2 GPU node pool runs a multi-node NCCL job with nothing set,
     then a SHARP job;
   - an existing hand-made cluster with the plugin enabled is unchanged;
   - check MPI over UCX between two gated hosts (the UCX non-goal above).

## Left to the builder

- Internal structure, package layout and the exact metric and flag names, within
  the rules above.
- The NRI plugin index value and registration details, as long as the plugin
  orders after DRANET.
- How the image build joins the existing release-please and release workflows.
- The kind integration test's mechanics (how containerd's NRI is enabled in kind,
  how the fixture is mounted).
