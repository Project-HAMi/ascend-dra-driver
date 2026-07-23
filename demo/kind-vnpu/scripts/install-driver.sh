#!/usr/bin/env bash

set -euo pipefail

CURRENT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
# shellcheck source=common.sh
source "${CURRENT_DIR}/common.sh"

cluster_exists || fail "Kind cluster ${KIND_CLUSTER_NAME} does not exist"
docker image inspect "${DRIVER_IMAGE}" >/dev/null 2>&1 ||
  fail "driver image does not exist: ${DRIVER_IMAGE}"

section "Load the driver image into the Kind worker"

"${KIND_BIN}" load docker-image "${DRIVER_IMAGE}" \
  --name "${KIND_CLUSTER_NAME}" \
  --nodes "${WORKER}"

docker exec "${WORKER}" crictl images |
  grep -F "${DRIVER_IMAGE_REPOSITORY}"

section "Install and start the DRA kubelet plugin"

render_template \
  "${DEMO_DIR}/templates/helm-values.yaml.tpl" \
  "${DEMO_STATE_DIR}/helm-values.yaml" \
  DRIVER_IMAGE_REPOSITORY "${DRIVER_IMAGE_REPOSITORY}" \
  DRIVER_IMAGE_TAG "${DRIVER_IMAGE_TAG}"

helm upgrade --install "${HELM_RELEASE}" \
  "${PROJECT_DIR}/deployments/helm/ascend-dra-driver" \
  --namespace "${DRIVER_NAMESPACE}" \
  --create-namespace \
  -f "${DEMO_STATE_DIR}/helm-values.yaml"

: > "${DEMO_STATE_DIR}/device-share.may-have-changed"

kubectl -n "${DRIVER_NAMESPACE}" patch \
  daemonset ascend-dra-driver-kubeletplugin \
  --type=json \
  -p='[
    {
      "op":"replace",
      "path":"/spec/template/spec/containers/0/command",
      "value":["/usr/bin/ascend-dra-kubeletplugin"]
    },
    {
      "op":"add",
      "path":"/spec/template/spec/containers/0/args",
      "value":["--feature-gates=HAMivNPUCore=true"]
    }
  ]'

kubectl -n "${DRIVER_NAMESPACE}" rollout status \
  daemonset/ascend-dra-driver-kubeletplugin \
  --timeout=5m

kubectl -n "${DRIVER_NAMESPACE}" logs \
  daemonset/ascend-dra-driver-kubeletplugin \
  -c plugin --tail=300 |
  tee "${DEMO_STATE_DIR}/driver-startup.log"

section "Verify published capacity and device-share"

for _ in $(seq 1 60); do
  if kubectl get resourceslices -o yaml \
    > "${DEMO_STATE_DIR}/resourceslices.poll.yaml" &&
    grep -q 'npu-0-0' \
      "${DEMO_STATE_DIR}/resourceslices.poll.yaml"; then
    break
  fi
  sleep 2
done

kubectl get resourceslices -o yaml |
  tee "${DEMO_STATE_DIR}/resourceslices.yaml"

grep -q 'npu-0-0' "${DEMO_STATE_DIR}/resourceslices.yaml"
grep -q 'allowMultipleAllocations: true' \
  "${DEMO_STATE_DIR}/resourceslices.yaml"
grep -q 'npu.project-hami.io/memory' \
  "${DEMO_STATE_DIR}/resourceslices.yaml"
grep -q 'npu.project-hami.io/aicore' \
  "${DEMO_STATE_DIR}/resourceslices.yaml"

for card_id in ${DEVICE_SHARE_CARD_IDS}; do
  npu-smi info -t device-share -i "${card_id}" |
    tee "${DEMO_STATE_DIR}/device-share-card-${card_id}.txt"
  grep -q True \
    "${DEMO_STATE_DIR}/device-share-card-${card_id}.txt"
done

success "DRA plugin published shareable HAMivNPUCore capacity"
