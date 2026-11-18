#!/usr/bin/env bash
# End-to-end test of nccl-fabric-nri under a real containerd with NRI, in a
# throwaway kind cluster. See README.md in this directory.
#
# PRODUCTION SAFETY: every kubectl/helm call goes through k()/h(), which pass
# this test's own kubeconfig and context and refuse to run unless that
# kubeconfig points at the kind node's published API port. kind create/delete
# pass --kubeconfig. Nothing here reads or writes ~/.kube/config.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)

CLUSTER=nri-e2e
CTX=kind-$CLUSTER
NODE=$CLUSTER-control-plane
KCFG=$here/.kubeconfig
WORK=$here/.work
NS=nccl-fabric-nri
# kind v0.31's v1.35.0 node image: containerd v2.2.0 (NRI v0.11), as on NKS nodes.
NODE_IMAGE=${NRI_E2E_NODE_IMAGE:-kindest/node:v1.35.0@sha256:452d707d4862f52530247495d180205e029056831160e22870e37e3f6c1ac31f}
BUSYBOX_BASE=busybox:1.37.0
# Rebuilt locally: kind load cannot import a pulled multi-platform image
# ("content digest ... not found"), but a locally built one is single-platform.
BUSYBOX=nri-e2e-busybox:e2e
PLUGIN_REPO=nccl-fabric-nri
PLUGIN_TAG=e2e
ENV_IMAGE=nri-e2e-env:e2e
INTERVAL=5 # seconds; the chart default --interval
LIVE=/fixture/sys # hostSysRoot on the node; the plugin sees it at /host/sys

# Never fall back to the developer's kubeconfig, even if a call slips past k()/h().
export KUBECONFIG=$KCFG
if [ -z "${DOCKER_HOST:-}" ] && [ -S "$HOME/.colima/default/docker.sock" ]; then
  export DOCKER_HOST=unix://$HOME/.colima/default/docker.sock
fi

die() { echo "FATAL: $*" >&2; exit 2; }
log() { echo "--- $*"; }

# guard aborts unless $KCFG's current context is ours and its server is the
# kind node's published 6443 port.
guard() {
  [ -f "$KCFG" ] || die "no kubeconfig at $KCFG"
  local cur server port
  cur=$(kubectl --kubeconfig "$KCFG" config current-context 2>/dev/null) || die "cannot read current context of $KCFG"
  [ "$cur" = "$CTX" ] || die "current context of $KCFG is '$cur', not '$CTX'"
  server=$(kubectl --kubeconfig "$KCFG" config view --minify --context "$CTX" -o jsonpath='{.clusters[0].cluster.server}')
  port=$(docker port "$NODE" 6443/tcp 2>/dev/null | grep '^127\.0\.0\.1:' | head -1) || true
  [ -n "$port" ] || die "kind node $NODE publishes no 127.0.0.1 port for 6443"
  [ "$server" = "https://$port" ] || die "API server '$server' is not the kind node's https://$port"
}
k() { guard; kubectl --kubeconfig "$KCFG" --context "$CTX" "$@"; }
h() { guard; helm --kubeconfig "$KCFG" --kube-context "$CTX" "$@"; }
node() { docker exec "$NODE" "$@"; }

cleanup() {
  local rc=$?
  if [ "${NRI_E2E_KEEP:-0}" = 1 ]; then
    echo "NRI_E2E_KEEP=1: leaving cluster $CLUSTER (kubeconfig $KCFG)"
  else
    kind delete cluster --name "$CLUSTER" --kubeconfig "$KCFG" > /dev/null 2>&1 || true
    rm -f "$KCFG"
  fi
  exit $rc
}

# ---------------------------------------------------------------- checks

pass=0 fail=0
ok() { echo "ok   $*"; pass=$((pass + 1)); }
bad() { echo "FAIL $*"; fail=$((fail + 1)); }
note() { echo "     $*"; }

# pod_env NAME IMAGE [KEY=VALUE...]: runs `env` in a new pod and prints the
# NCCL_IB_PKEY* lines it logged. Returns 1 if the pod does not succeed in 90s.
pod_env() {
  local name=$1 image=$2
  shift 2
  local envyaml=""
  if [ $# -gt 0 ]; then
    envyaml="      env:"
    for kv in "$@"; do
      envyaml+=$'\n'"        - name: ${kv%%=*}"$'\n'"          value: \"${kv#*=}\""
    done
  fi
  k apply -f - > /dev/null <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: $name
  namespace: default
spec:
  restartPolicy: Never
  terminationGracePeriodSeconds: 0
  containers:
    - name: env
      image: $image
      imagePullPolicy: Never
      command: ["env"]
$envyaml
EOF
  if ! k wait --for=jsonpath='{.status.phase}'=Succeeded "pod/$name" --timeout=90s > /dev/null 2>&1; then
    k describe pod "$name" | sed -n '/^Events:/,$p' | sed 's/^/     /' >&2
    return 1
  fi
  k logs "$name" | grep '^NCCL_IB_PKEY' || true
}

# expect_env DESC NAME IMAGE WANT [KEY=VALUE...]: WANT is the exact set of
# NCCL_IB_PKEY* lines ("" for none).
expect_env() {
  local desc=$1 name=$2 image=$3 want=$4 got
  shift 4
  if ! got=$(pod_env "$name" "$image" "$@"); then
    bad "$desc: pod $name did not succeed"
    return
  fi
  if [ "$got" = "$want" ]; then
    ok "$desc (got: ${got:-<none>})"
  else
    bad "$desc: want '${want:-<none>}', got '${got:-<none>}'"
  fi
}

plugin_pod() {
  k -n "$NS" get pod -l app=nccl-fabric-nri -o json \
    | jq -r '[.items[] | select(.metadata.deletionTimestamp == null)][0].metadata.name // empty'
}

# wait_plugin_registered: waits for a live plugin pod to log NRI registration;
# prints "<pod> <seconds>".
wait_plugin_registered() {
  local start=$SECONDS pod=""
  while [ $((SECONDS - start)) -lt 90 ]; do
    pod=$(plugin_pod)
    if [ -n "$pod" ] && k -n "$NS" logs "$pod" 2> /dev/null | grep -q 'Started plugin 90-nccl-fabric-nri'; then
      echo "$pod $((SECONDS - start))"
      return 0
    fi
    sleep 1
  done
  return 1
}

# plugin_state POD: the state in the pod's last "node state changed" line.
plugin_state() {
  k -n "$NS" logs "$1" | grep 'node state changed' | tail -1 | sed -n 's/.* state=\([a-z0-9]*\).*/\1/p'
}

wait_plugin_state() { # POD STATE
  local start=$SECONDS
  while [ $((SECONDS - start)) -lt $((INTERVAL * 6)) ]; do
    [ "$(plugin_state "$1")" = "$2" ] && return 0
    sleep 1
  done
  return 1
}

metrics() { # POD
  local ip
  ip=$(k -n "$NS" get pod "$1" -o jsonpath='{.status.podIP}')
  node curl -sf "http://$ip:9900/metrics" | grep '^nccl_fabric_nri_'
}

use_fixture() { # hfm | handmade | no-ib | garbage | empty
  if [ "$1" = empty ]; then
    node sh -c "rm -rf $LIVE/*"
  elif [ "$1" = garbage ]; then
    # The HFM layout with one p_key entry unreadable as a p_key. Garbage, not
    # chmod: the plugin runs as root and would read the file anyway.
    node sh -c "rm -rf $LIVE/* && cp -a /fixtures/hfm/. $LIVE/ && echo zz > $LIVE/devices/pci0/infiniband/mlx5_0/ports/1/pkeys/1"
  else
    node sh -c "rm -rf $LIVE/* && cp -a /fixtures/$1/. $LIVE/"
  fi
}

install_plugin() { # extra helm args...
  h upgrade --install nccl-fabric-nri "$WORK/chart" -n "$NS" --create-namespace \
    --set ncclFabricNri.enabled=true \
    --set ncclFabricNri.image.repository=$PLUGIN_REPO \
    --set ncclFabricNri.image.tag=$PLUGIN_TAG \
    --set ncclFabricNri.hostSysRoot=$LIVE \
    "$@" > /dev/null
}

# ---------------------------------------------------------------- setup

for tool in docker kind helm kubectl yq jq; do
  command -v "$tool" > /dev/null || die "needs $tool"
done

trap cleanup EXIT
rm -rf "$WORK"
mkdir -p "$WORK"

if kind get clusters 2> /dev/null | grep -qx "$CLUSTER"; then
  log "deleting leftover cluster $CLUSTER"
  kind delete cluster --name "$CLUSTER" --kubeconfig "$KCFG"
fi
rm -f "$KCFG"

log "building images"
docker build -q -t "$PLUGIN_REPO:$PLUGIN_TAG" -f "$root/cmd/nccl-fabric-nri/Dockerfile" "$root" > /dev/null
docker image inspect "$BUSYBOX_BASE" > /dev/null 2>&1 || docker pull -q "$BUSYBOX_BASE" > /dev/null
echo "FROM $BUSYBOX_BASE" | docker build -q -t "$BUSYBOX" - > /dev/null
docker build -q -t "$ENV_IMAGE" --build-arg BASE="$BUSYBOX_BASE" "$here/env-image" > /dev/null

log "creating kind cluster $CLUSTER ($NODE_IMAGE)"
sed "s|@FIXTURES@|$here/fixtures|" "$here/kind.yaml" > "$WORK/kind.yaml"
kind create cluster --name "$CLUSTER" --kubeconfig "$KCFG" --image "$NODE_IMAGE" \
  --config "$WORK/kind.yaml" --wait 120s
guard
log "guard ok: $(kubectl --kubeconfig "$KCFG" config current-context) -> $(docker port "$NODE" 6443/tcp | head -1)"

log "node runtime"
node containerd --version
nri_cfg=$(node containerd config dump | sed -n '/^ *\[plugins\.'"'"'io\.containerd\.nri\.v1\.nri'"'"'\]/,/^ *\[/p')
echo "$nri_cfg"
echo "$nri_cfg" | grep -q 'disable = false' || die "containerd NRI is disabled in the node"
node test -S /var/run/nri/nri.sock || die "no NRI socket at /var/run/nri/nri.sock"
node ls -l /var/run/nri/nri.sock

for img in "$PLUGIN_REPO:$PLUGIN_TAG" "$BUSYBOX" "$ENV_IMAGE"; do
  kind load docker-image "$img" --name "$CLUSTER" > /dev/null
done
k label node "$NODE" nvidia.com/gpu.present=true > /dev/null
node test -L /fixtures/hfm/class/infiniband/mlx5_0 || die "fixture symlinks did not survive the extraMount"
node mkdir -p "$LIVE"
use_fixture hfm

# A copy of the chart without subcharts: this test deploys only the plugin.
mkdir -p "$WORK/chart/templates/nccl-fabric-nri"
yq 'del(.dependencies)' "$root/chart/Chart.yaml" > "$WORK/chart/Chart.yaml"
cp "$root/chart/templates/_helpers.tpl" "$WORK/chart/templates/"
cp "$root"/chart/templates/nccl-fabric-nri/*.yaml "$WORK/chart/templates/nccl-fabric-nri/"
yq '{"ncclFabricNri": .ncclFabricNri}' "$root/chart/values.yaml" > "$WORK/chart/values.yaml"

# ---------------------------------------------------------------- red

log "before install (check 1 must fail here)"
got=$(pod_env red-hfm "$BUSYBOX") || die "pod failed before install"
if [ -z "$got" ]; then
  ok "before install: check 1 fails as expected (no NCCL_IB_PKEY on the HFM fixture)"
else
  bad "before install: NCCL_IB_PKEY already present ($got)"
fi

# ---------------------------------------------------------------- install

log "installing the plugin"
# Start like a GPU node without ib_core: no class/infiniband at all.
use_fixture no-ib
install_plugin
if reg=$(wait_plugin_registered); then
  POD=${reg% *}
  ok "registration: $POD registered with NRI ${reg#* }s after install"
  k -n "$NS" logs "$POD" | sed 's/^/     | /'
else
  bad "registration: no 'Started plugin' log within 90s"
  k -n "$NS" get pods -o wide >&2 || true
  pod=$(plugin_pod)
  k -n "$NS" logs "$pod" --previous >&2 || k -n "$NS" logs "$pod" >&2 || true
  k -n "$NS" describe pod "$pod" | sed -n '/^Containers:/,/^Conditions:/p' >&2 || true
  exit 1
fi

log "no ib_core: fixture without class/infiniband"
note "metrics: $(metrics "$POD" | grep node_state | tr '\n' ' ')"
if metrics "$POD" | grep -qx 'nccl_fabric_nri_node_state{state="none"} 1'; then
  ok "no-ib plugin started and reads state none"
else
  bad "no-ib plugin state is not none"
fi
expect_env "no-ib pod gets no NCCL_IB_PKEY" c0-noib "$BUSYBOX" ""

log "check 1-2: HFM fixture"
use_fixture hfm
if wait_plugin_state "$POD" indexed; then
  ok "1a plugin reads indexed through class/infiniband symlinks into devices/"
else
  bad "1a plugin state is '$(plugin_state "$POD")', want indexed"
fi
line=$(k -n "$NS" logs "$POD" | grep 'node state changed' | tail -1)
case $line in
  *" ports=2"*) ok "1b state-change log counts the 2 tenant ports" ;;
  *) bad "1b state-change log lacks ports=2: $line" ;;
esac
expect_env "1 HFM fixture adds NCCL_IB_PKEY=1" c1-hfm "$BUSYBOX" "NCCL_IB_PKEY=1"
expect_env "2 user NCCL_IB_PKEY=3 in the pod spec is kept" c2-user "$BUSYBOX" "NCCL_IB_PKEY=3" NCCL_IB_PKEY=3
expect_env "2b user NCCL_IB_PKEY_VALUE in the pod spec suppresses NCCL_IB_PKEY" c2-value "$BUSYBOX" \
  "NCCL_IB_PKEY_VALUE=0x8600" NCCL_IB_PKEY_VALUE=0x8600

log "image ENV"
before=$(metrics "$POD" | sed -n 's/^nccl_fabric_nri_containers_skipped_total{reason="user_set"} //p')
if got=$(pod_env ce-image "$ENV_IMAGE"); then
  after=$(metrics "$POD" | sed -n 's/^nccl_fabric_nri_containers_skipped_total{reason="user_set"} //p')
  note "image sets ENV NCCL_IB_PKEY=5; container env: ${got:-<none>}; user_set skips ${before:-0} -> ${after:-0}"
  if [ "$got" = "NCCL_IB_PKEY=5" ] && [ "${after:-0}" != "${before:-0}" ]; then
    ok "image ENV NCCL_IB_PKEY=5 is kept (containerd passes image ENV to NRI; counted as user_set)"
  elif [ "$got" = "NCCL_IB_PKEY=5" ]; then
    bad "image ENV kept but the plugin did not count user_set (${before:-0} -> ${after:-0})"
  else
    bad "image ENV NCCL_IB_PKEY=5 not respected: NRI does not see image ENV (container env: $got)"
  fi
else
  bad "image ENV pod did not succeed"
fi

log "check 3: hand-made fixture"
use_fixture handmade
if wait_plugin_state "$POD" slot0; then
  ok "3a plugin moved to slot0 after two contradicting polls"
else
  bad "3a plugin state is '$(plugin_state "$POD")', want slot0"
fi
expect_env "3 hand-made fixture adds nothing" c3-handmade "$BUSYBOX" ""
if node test ! -e /var/lib/nccl-fabric-nri/state; then
  ok "3b state file removed once indexed was replaced"
else
  bad "3b state file still present: $(node cat /var/lib/nccl-fabric-nri/state)"
fi

log "unreadable p_key table"
use_fixture garbage
if wait_plugin_state "$POD" ambiguous; then
  ok "3c plugin reads ambiguous with a garbage p_key entry"
else
  bad "3c plugin state is '$(plugin_state "$POD")', want ambiguous"
fi
expect_env "3c garbage p_key entry: pod starts with no NCCL_IB_PKEY" c3-garbage "$BUSYBOX" ""

log "check 4: sticky"
use_fixture hfm
if wait_plugin_state "$POD" indexed; then
  ok "4a plugin back to indexed on the HFM fixture"
else
  bad "4a plugin state is '$(plugin_state "$POD")', want indexed"
fi
note "state file: $(node cat /var/lib/nccl-fabric-nri/state 2> /dev/null || echo '<missing>')"
use_fixture empty
sleep $((INTERVAL * 3))
note "metrics with ports removed: $(metrics "$POD" | grep node_state | tr '\n' ' ')"
expect_env "4 ports removed: still NCCL_IB_PKEY=1 (sticky)" c4-sticky "$BUSYBOX" "NCCL_IB_PKEY=1"

log "SIGTERM: deleting the plugin pod while pods start"
cid=$(k -n "$NS" get pod "$POD" -o jsonpath='{.status.containerStatuses[0].containerID}')
cid=${cid#containerd://}
k -n "$NS" logs -f "$POD" > "$WORK/old-plugin.log" 2>&1 &
logpid=$!
# Record the old container's state from the node until it exits: the kubelet
# removes it soon after, so this runs alongside the pod creation below.
node sh -c "for i in \$(seq 1 300); do
  s=\$(crictl inspect -o go-template --template '{{.status.state}} {{.status.exitCode}} {{.status.reason}}' $cid 2>/dev/null)
  case \$s in CONTAINER_EXITED*) echo \"\$s after \$((i / 5)).\$((i % 5 * 2))s\"; exit 0;; esac
  sleep 0.2
done" > "$WORK/old-exit" 2>&1 &
exitpid=$!
k -n "$NS" delete pod "$POD" --wait=false > /dev/null
# No container may be blocked while the plugin terminates and restarts.
for i in 1 2 3 4 5 6; do
  k apply -f - > /dev/null <<EOF
apiVersion: v1
kind: Pod
metadata: {name: restart-$i, namespace: default}
spec:
  restartPolicy: Never
  terminationGracePeriodSeconds: 0
  containers: [{name: env, image: $BUSYBOX, imagePullPolicy: Never, command: ["env"]}]
EOF
  sleep 3
done
wait $exitpid || true
exit_info=$(cat "$WORK/old-exit")
wait $logpid 2> /dev/null || true
note "old plugin container $cid: ${exit_info:-<not observed>}"
note "old plugin's last log lines:"
tail -4 "$WORK/old-plugin.log" | sed 's/^/     | /'
case $exit_info in
  "CONTAINER_EXITED 0 "*) ok "SIGTERM: plugin exited 0 ($exit_info)" ;;
  "") bad "SIGTERM: could not observe the old container's exit" ;;
  *) bad "SIGTERM: plugin did not exit 0 ($exit_info)" ;;
esac
blocked=0
note "restart-N pods created before the replacement registered get no variable (by design: never blocked, not injected)"
for i in 1 2 3 4 5 6; do
  if k wait --for=jsonpath='{.status.phase}'=Succeeded "pod/restart-$i" --timeout=60s > /dev/null 2>&1; then
    note "restart-$i: $(k logs "restart-$i" | grep '^NCCL_IB_PKEY' || echo '<none>')"
  else
    blocked=1
    bad "pod restart-$i (created during the plugin restart) did not succeed"
  fi
done
[ $blocked = 0 ] && ok "no container blocked: all 6 pods created during the plugin restart started"

if reg=$(wait_plugin_registered); then
  POD=${reg% *}
  ok "re-registration: $POD registered with NRI"
  note "startup state: $(plugin_state "$POD") (sysfs empty, state reloaded from file)"
else
  bad "re-registration: no 'Started plugin' log within 90s"
fi
expect_env "4 after plugin restart with ports still removed: NCCL_IB_PKEY=1 (state file reloaded)" \
  c4-restart "$BUSYBOX" "NCCL_IB_PKEY=1"

log "check 5: plugin stopped"
use_fixture hfm
install_plugin --set-string "ncclFabricNri.nodeSelector.nri-e2e/disabled=true"
if k -n "$NS" wait --for=delete pod -l app=nccl-fabric-nri --timeout=90s > /dev/null 2>&1; then
  ok "5a plugin pod gone (DaemonSet selects no node)"
else
  bad "5a plugin pod still present"
fi
expect_env "5 plugin stopped: pod starts normally without NCCL_IB_PKEY" c5-stopped "$BUSYBOX" ""

log "check 6: no conflict"
install_plugin
if reg=$(wait_plugin_registered); then
  POD=${reg% *}
  ok "6a plugin re-installed and registered"
else
  bad "6a plugin did not register"
fi
# containerd logs the invocation order each time a plugin syncs; kind's own
# kindnet runs an NRI plugin (kube-network-policies), so ours is not alone.
order=$(node journalctl -u containerd --no-pager \
  | awk '/plugin invocation order/ { delete o; n = 0; next } /msg="  #[0-9]+: / { sub(/.*msg="  /, ""); sub(/ \(external.*/, ""); o[++n] = $0 } END { for (i = 1; i <= n; i++) print o[i] }' \
  | tr -d '\\"' | tr '\n' ' ')
note "NRI plugin invocation order: ${order:-<unknown>}"
case $order in
  *"#1: "*" #"*": 90-nccl-fabric-nri ") ok "6c plugin runs alongside another NRI plugin, ordered last ($order)" ;;
  "#1: 90-nccl-fabric-nri ") note "nccl-fabric-nri is the only NRI plugin on the node" ;;
  *) bad "6c nccl-fabric-nri missing from the NRI invocation order or not ordered last ($order)" ;;
esac
expect_env "6 HFM fixture pod starts with exactly NCCL_IB_PKEY=1 (no env conflict)" c6-hfm "$BUSYBOX" "NCCL_IB_PKEY=1"
if k get events -A --field-selector reason=Failed -o json | jq -e '[.items[] | select(.message | test("NRI|nri"))] | length == 0' > /dev/null; then
  ok "6b no NRI-related Failed events in the cluster"
else
  bad "6b NRI-related Failed events: $(k get events -A --field-selector reason=Failed | grep -i nri)"
fi
note "final metrics: $(metrics "$POD" | tr '\n' ' ')"

echo
echo "passed $pass, failed $fail"
[ "$fail" = 0 ]
