#!/usr/bin/env bash

set -euo pipefail

CURRENT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
# shellcheck source=common.sh
source "${CURRENT_DIR}/common.sh"

cluster_exists || fail "Kind cluster ${KIND_CLUSTER_NAME} does not exist"
kubectl get runtimeclass ascend >/dev/null
kubectl -n "${DRIVER_NAMESPACE}" rollout status \
  daemonset/ascend-dra-driver-kubeletplugin \
  --timeout=30s

section "Verify and load the NPU workload image"

docker image inspect "${WORKLOAD_IMAGE}" >/dev/null 2>&1 ||
  fail "NPU workload image is missing: ${WORKLOAD_IMAGE}"
[[ "$(docker image inspect "${WORKLOAD_IMAGE}" --format '{{.Architecture}}')" == "arm64" ]] ||
  fail "${WORKLOAD_IMAGE} is not an arm64 image"
docker run --rm --entrypoint sh "${WORKLOAD_IMAGE}" -c '
  set -e
  for required_command in bash python3; do
    if ! command -v "${required_command}" >/dev/null 2>&1; then
      printf "ERROR workload image is missing required command: %s\n" \
        "${required_command}" >&2
      exit 127
    fi
  done
  python3 -c "import ctypes"
'
record_demo_image "${WORKLOAD_IMAGE}"

"${KIND_BIN}" load docker-image "${WORKLOAD_IMAGE}" \
  --name "${KIND_CLUSTER_NAME}" \
  --nodes "${WORKER}"
docker exec "${WORKER}" crictl inspecti "${WORKLOAD_IMAGE}" >/dev/null

section "Create the shared demo resources"

render_template \
  "${DEMO_DIR}/templates/setup-resources.yaml.tpl" \
  "${DEMO_STATE_DIR}/setup-resources.yaml" \
  TEST_NAMESPACE "${TEST_NAMESPACE}"
kubectl apply -f "${DEMO_STATE_DIR}/setup-resources.yaml"

success "workload image, test namespace, and DeviceClass are ready"
