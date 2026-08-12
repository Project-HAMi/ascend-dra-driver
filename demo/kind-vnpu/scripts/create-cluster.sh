#!/usr/bin/env bash

set -euo pipefail

CURRENT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
# shellcheck source=common.sh
source "${CURRENT_DIR}/common.sh"

require_file "${STATE_FILE}"
[[ -x "${KIND_BIN}" ]] || fail "Kind binary is not executable: ${KIND_BIN}"

cluster_exists &&
  fail "Kind cluster ${KIND_CLUSTER_NAME} already exists"

section "Render the Kind configuration"

render_template \
  "${DEMO_DIR}/templates/kind-config.yaml.tpl" \
  "${DEMO_STATE_DIR}/kind-config.yaml" \
  NPU_SMI_HOST_PATH "${NPU_SMI_HOST_PATH}"

sed -n '1,220p' "${DEMO_STATE_DIR}/kind-config.yaml"

section "Create the two-node Kind cluster"

create_status=0
KIND_EXPERIMENTAL_PROVIDER=docker "${KIND_BIN}" create cluster \
  --retain \
  --name "${KIND_CLUSTER_NAME}" \
  --image "${CUSTOM_KIND_NODE_IMAGE}" \
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
  test -c /dev/davinci0
  test -c /dev/davinci1
  test -c /dev/davinci_manager
  test -c /dev/devmm_svm
  test -c /dev/hisi_hdc
  grep -q "containerd.runtimes.ascend" /etc/containerd/config.toml
  grep -q "SystemdCgroup = false" /etc/containerd/config.toml
  grep -q "enable_cdi = true" /etc/containerd/config.toml
'

kubectl get --raw /apis/resource.k8s.io/v1 >/dev/null
kubectl apply -f "${DEMO_DIR}/yaml/runtimeclass.yaml"
kubectl get runtimeclass ascend -o yaml

save_state
success "Kind cluster and Ascend RuntimeClass are ready"
