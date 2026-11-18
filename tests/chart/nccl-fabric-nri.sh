#!/usr/bin/env bash
# Render assertions for the ncclFabricNri DaemonSet. Needs helm and yq; renders
# only (never talks to a cluster).
#
# Renders the full chart, so its dependencies must be built (CI); missing
# dependencies are a hard failure. For local use only, NCCL_FABRIC_NRI_STRIPPED=1
# opts in to rendering a copy of the chart without subcharts, which is enough
# for this template: it only uses .Chart and .Values.ncclFabricNri.
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

chart="$root/chart"
if helm dependency list "$chart" 2>/dev/null | grep -q missing; then
  if [ "${NCCL_FABRIC_NRI_STRIPPED:-}" = 1 ]; then
    echo "note: NCCL_FABRIC_NRI_STRIPPED=1, rendering a copy without subcharts"
    chart="$work/chart"
    mkdir -p "$chart/templates/nccl-fabric-nri"
    yq 'del(.dependencies)' "$root/chart/Chart.yaml" > "$chart/Chart.yaml"
    cp "$root/chart/templates/_helpers.tpl" "$chart/templates/"
    cp "$root"/chart/templates/nccl-fabric-nri/*.yaml "$chart/templates/nccl-fabric-nri/"
    yq '{"ncclFabricNri": .ncclFabricNri}' "$root/chart/values.yaml" > "$chart/values.yaml"
  else
    echo "error: chart dependencies are not built; run 'helm dependency build chart'" >&2
    echo "       (or set NCCL_FABRIC_NRI_STRIPPED=1 to render a copy without subcharts, local use only)" >&2
    exit 2
  fi
fi

fail=0
check() { # description, command...
  local desc=$1
  shift
  if "$@" > /dev/null 2>&1; then echo "ok   $desc"; else echo "FAIL $desc"; fail=1; fi
}

render() { helm template t "$chart" "$@"; }
ds() { yq 'select(.kind == "DaemonSet" and .metadata.name == "nccl-fabric-nri")' "$work/on.yaml"; }
ds_has() { [ "$(ds | yq "$1")" = "true" ]; }

version=$(yq .version "$root/chart/Chart.yaml")

render > "$work/off.yaml"
check "disabled renders no nccl-fabric-nri objects" \
  bash -c "! grep -q nccl-fabric-nri '$work/off.yaml'"

render --set ncclFabricNri.enabled=true > "$work/on.yaml"
check "enabled renders exactly one DaemonSet" \
  [ "$(yq 'select(.kind == "DaemonSet" and .metadata.name == "nccl-fabric-nri") | .kind' "$work/on.yaml" | grep -c DaemonSet)" = 1 ]
check "the DaemonSet is the only object named nccl-fabric-nri (no ServiceAccount or RBAC)" \
  [ "$(yq 'select(.metadata.name == "nccl-fabric-nri") | .kind' "$work/on.yaml" | grep -c .)" = 1 ]
check "hostPath /var/run/nri" \
  ds_has '[.spec.template.spec.volumes[] | select(.hostPath.path == "/var/run/nri")] | length == 1'
check "hostPath /sys (type Directory)" \
  ds_has '[.spec.template.spec.volumes[] | select(.name == "sysfs" and .hostPath.path == "/sys" and .hostPath.type == "Directory")] | length == 1'
check "hostPath /var/lib/nccl-fabric-nri is DirectoryOrCreate" \
  ds_has '[.spec.template.spec.volumes[] | select(.hostPath.path == "/var/lib/nccl-fabric-nri" and .hostPath.type == "DirectoryOrCreate")] | length == 1'
check "host sysfs mounted read-only at /host/sys" \
  ds_has '[.spec.template.spec.containers[0].volumeMounts[] | select(.name == "sysfs" and .mountPath == "/host/sys" and .readOnly == true)] | length == 1'
check "nothing mounted under the container's own /sys" \
  ds_has '[.spec.template.spec.containers[0].volumeMounts[] | select(.mountPath | test("^/sys(/|$)"))] | length == 0'
check "not privileged" \
  ds_has '.spec.template.spec.containers[0].securityContext.privileged != true'
check "runs as root (uid 0, runAsNonRoot false)" \
  ds_has '(.spec.template.spec.containers[0].securityContext.runAsUser == 0) and (.spec.template.spec.containers[0].securityContext.runAsNonRoot == false)'
check "privilege escalation disallowed" \
  ds_has '.spec.template.spec.containers[0].securityContext.allowPrivilegeEscalation == false'
check "read-only root filesystem" \
  ds_has '.spec.template.spec.containers[0].securityContext.readOnlyRootFilesystem == true'
check "all capabilities dropped, none added" \
  ds_has '((.spec.template.spec.containers[0].securityContext.capabilities.drop | length) == 1 and .spec.template.spec.containers[0].securityContext.capabilities.drop[0] == "ALL") and (.spec.template.spec.containers[0].securityContext.capabilities.add == null)'
check "RuntimeDefault seccomp profile" \
  ds_has '.spec.template.spec.containers[0].securityContext.seccompProfile.type == "RuntimeDefault"'
check "maxUnavailable defaults to 1" \
  ds_has '.spec.updateStrategy.rollingUpdate.maxUnavailable == 1'
check "no service account token automount" \
  ds_has '.spec.template.spec.automountServiceAccountToken == false'
check "arg --plugin-idx=90" \
  ds_has '.spec.template.spec.containers[0].args | contains(["--plugin-idx=90"])'
check "arg --interval=5s" \
  ds_has '.spec.template.spec.containers[0].args | contains(["--interval=5s"])'
check "arg --metrics-addr=:9900" \
  ds_has '.spec.template.spec.containers[0].args | contains(["--metrics-addr=:9900"])'
check "arg --sysfs=/host/sys/class/infiniband" \
  ds_has '.spec.template.spec.containers[0].args | contains(["--sysfs=/host/sys/class/infiniband"])'
check "metrics container port 9900" \
  ds_has '[.spec.template.spec.containers[0].ports[] | select(.containerPort == 9900)] | length == 1'
check "image tag defaults to the chart version ($version)" \
  ds_has ".spec.template.spec.containers[0].image == \"ghcr.io/nscaledev/nccl-fabric-nri:$version\""
check "runs on GPU nodes" \
  ds_has '.spec.template.spec.nodeSelector["nvidia.com/gpu.present"] == "true"'
check "priorityClassName defaults to system-node-critical" \
  ds_has '.spec.template.spec.priorityClassName == "system-node-critical"'
check "resources default to requests cpu 10m / memory 32Mi, limit memory 64Mi" \
  ds_has '(.spec.template.spec.containers[0].resources.requests.cpu == "10m") and (.spec.template.spec.containers[0].resources.requests.memory == "32Mi") and (.spec.template.spec.containers[0].resources.limits.memory == "64Mi") and (.spec.template.spec.containers[0].resources.limits.cpu == null) and ((.spec.template.spec.containers[0].resources.requests | length) == 2) and ((.spec.template.spec.containers[0].resources.limits | length) == 1)'
check "tolerates all taints" \
  ds_has '.spec.template.spec.tolerations[0].operator == "Exists"'

render --set ncclFabricNri.enabled=true --set ncclFabricNri.image.tag=v9.9.9 \
  --set ncclFabricNri.hostSysRoot=/fixture/sys --set ncclFabricNri.metricsPort=9911 \
  --set ncclFabricNri.maxUnavailable=3 --set ncclFabricNri.pluginIdx=95 --set ncclFabricNri.interval=10s \
  --set ncclFabricNri.priorityClassName=custom-pc --set ncclFabricNri.resources.limits.memory=128Mi > "$work/on.yaml"
check "explicit tag wins" \
  ds_has '.spec.template.spec.containers[0].image == "ghcr.io/nscaledev/nccl-fabric-nri:v9.9.9"'
check "hostSysRoot is the hostPath source, mount path and --sysfs unchanged" \
  ds_has '([.spec.template.spec.volumes[] | select(.name == "sysfs" and .hostPath.path == "/fixture/sys")] | length == 1) and ([.spec.template.spec.containers[0].volumeMounts[] | select(.mountPath == "/host/sys")] | length == 1) and (.spec.template.spec.containers[0].args | contains(["--sysfs=/host/sys/class/infiniband"]))'
check "metricsPort drives the arg and container port" \
  ds_has '(.spec.template.spec.containers[0].args | contains(["--metrics-addr=:9911"])) and (.spec.template.spec.containers[0].ports[0].containerPort == 9911)'
check "maxUnavailable is overridable" \
  ds_has '.spec.updateStrategy.rollingUpdate.maxUnavailable == 3'
check "priorityClassName is overridable" \
  ds_has '.spec.template.spec.priorityClassName == "custom-pc"'
check "resources are overridable" \
  ds_has '.spec.template.spec.containers[0].resources.limits.memory == "128Mi"'
check "pluginIdx and interval are overridable" \
  ds_has '(.spec.template.spec.containers[0].args | contains(["--plugin-idx=95", "--interval=10s"]))'

exit $fail
