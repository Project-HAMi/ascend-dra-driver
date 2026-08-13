#!/usr/bin/env bash

set -euo pipefail

CURRENT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
# shellcheck source=common.sh
source "${CURRENT_DIR}/common.sh"

require_file "${STATE_FILE}"
[[ -x "${KIND_BIN}" ]] || fail "Kind binary is not executable: ${KIND_BIN}"
[[ -x "${DEMO_DIR}/config/ascend-docker-runtime-wrapper" ]] ||
  fail "Ascend runtime wrapper is not executable"
select_kind_cluster_image

cluster_exists &&
  fail "Kind cluster ${KIND_CLUSTER_NAME} already exists"

section "Render the Kind configuration"

containerd_config_patches="$(kind_containerd_config_patches)"
kubelet_cgroup_patch="$(kind_kubelet_cgroup_patch)"
davinci_device_mounts="$(
  kind_davinci_device_mounts "${DAVINCI_DEVICE_NODES}"
)"
render_template \
  "${DEMO_DIR}/templates/kind-config.yaml.tpl" \
  "${DEMO_STATE_DIR}/kind-config.yaml" \
  CONTAINERD_CONFIG_PATCHES "${containerd_config_patches}" \
  KUBELET_CGROUP_PATCH "${kubelet_cgroup_patch}" \
  DAVINCI_DEVICE_MOUNTS "${davinci_device_mounts}" \
  ASCEND_RUNTIME_WRAPPER_HOST_PATH "${DEMO_DIR}/config/ascend-docker-runtime-wrapper" \
  NPU_SMI_HOST_PATH "${NPU_SMI_HOST_PATH}"

sed -n '1,220p' "${DEMO_STATE_DIR}/kind-config.yaml"

section "Create the two-node Kind cluster"

create_status=0
KIND_EXPERIMENTAL_PROVIDER=docker "${KIND_BIN}" create cluster \
  --retain \
  --name "${KIND_CLUSTER_NAME}" \
  --image "${KIND_CLUSTER_IMAGE}" \
  --config "${DEMO_STATE_DIR}/kind-config.yaml" \
  --kubeconfig "${KUBECONFIG}" \
  --wait 5m || create_status=$?

kind_container_ids \
  > "${DEMO_STATE_DIR}/kind-container-ids.expected"

if [[ "${create_status}" -ne 0 ]]; then
  fail "Kind cluster creation failed; retained node IDs were recorded for cleanup"
fi

[[ -s "${DEMO_STATE_DIR}/kind-container-ids.expected" ]] ||
  fail "Kind created no identifiable node containers"

kubectl wait --for=condition=Ready node --all --timeout=180s
kubectl get nodes -o wide

docker exec "${WORKER}" sh -c '
  set -e
  test -x /usr/local/bin/npu-smi
  test -x /usr/local/Ascend/Ascend-Docker-Runtime/ascend-docker-runtime
  test -x /usr/local/Ascend/Ascend-Docker-Runtime/ascend-docker-hook
  test -f /etc/ascend-docker-runtime.d/base.list
  test -c /dev/davinci_manager
  test -c /dev/devmm_svm
  test -c /dev/hisi_hdc
  grep -q "containerd.runtimes.ascend" /etc/containerd/config.toml
  grep -q "enable_cdi = true" /etc/containerd/config.toml
'

while IFS= read -r node; do
  [[ -n "${node}" ]] || continue
  docker exec "${WORKER}" test -c "${node}"
done <<< "${DAVINCI_DEVICE_NODES}"

if [[ "${KIND_RESOLVED_CGROUP_MODE}" == "cgroupfs" ]]; then
  docker exec "${WORKER}" \
    grep -q 'SystemdCgroup = false' /etc/containerd/config.toml
  docker exec "${WORKER}" \
    grep -Eq '^[[:space:]]*cgroupDriver:[[:space:]]*cgroupfs$' \
    /var/lib/kubelet/config.yaml
elif [[ "${KIND_RESOLVED_CGROUP_MODE}" == "systemd" ]]; then
  docker exec "${WORKER}" \
    grep -q 'SystemdCgroup = true' /etc/containerd/config.toml
  docker exec "${WORKER}" \
    grep -Eq '^[[:space:]]*cgroupDriver:[[:space:]]*systemd$' \
    /var/lib/kubelet/config.yaml
else
  fail "unsupported resolved Kind cgroup mode: ${KIND_RESOLVED_CGROUP_MODE}"
fi

kubectl get --raw /apis/resource.k8s.io/v1 >/dev/null
kubectl apply -f "${DEMO_DIR}/yaml/runtimeclass.yaml"
kubectl get runtimeclass ascend -o yaml

save_state
success "Kind cluster and Ascend RuntimeClass are ready"
