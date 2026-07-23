#!/usr/bin/env bash

set -euo pipefail

CURRENT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
# shellcheck source=common.sh
source "${CURRENT_DIR}/common.sh"

cluster_exists || fail "Kind cluster ${KIND_CLUSTER_NAME} does not exist"

if [[ -z "${UID_A:-}" ]]; then
  UID_A="$(
    kubectl -n "${TEST_NAMESPACE}" get resourceclaim npu-share-a \
      -o jsonpath='{.metadata.uid}'
  )"
fi
if [[ -z "${UID_B:-}" ]]; then
  UID_B="$(
    kubectl -n "${TEST_NAMESPACE}" get resourceclaim npu-share-b \
      -o jsonpath='{.metadata.uid}'
  )"
fi
export UID_A UID_B
save_state

section "Delete the workloads and wait for Unprepare"

kubectl -n "${TEST_NAMESPACE}" delete pod \
  npu-share-a npu-share-b --ignore-not-found --wait=true

for _ in $(seq 1 60); do
  if ! docker exec "${WORKER}" grep -R -q "${UID_A}" /var/run/cdi &&
    ! docker exec "${WORKER}" grep -R -q "${UID_B}" /var/run/cdi &&
    ! docker exec "${WORKER}" test -e \
      "/usr/local/hami-vnpu-core/containers/${UID_A}" &&
    ! docker exec "${WORKER}" test -e \
      "/usr/local/hami-vnpu-core/containers/${UID_B}"; then
    break
  fi
  sleep 2
done

! docker exec "${WORKER}" grep -R -q "${UID_A}" /var/run/cdi
! docker exec "${WORKER}" grep -R -q "${UID_B}" /var/run/cdi
! docker exec "${WORKER}" test -e \
  "/usr/local/hami-vnpu-core/containers/${UID_A}"
! docker exec "${WORKER}" test -e \
  "/usr/local/hami-vnpu-core/containers/${UID_B}"

docker exec "${WORKER}" cat \
  /var/lib/kubelet/plugins/npu.project-hami.io/checkpoint.json \
  > "${DEMO_STATE_DIR}/checkpoint-after-unprepare.json"
! grep -q "${UID_A}" "${DEMO_STATE_DIR}/checkpoint-after-unprepare.json"
! grep -q "${UID_B}" "${DEMO_STATE_DIR}/checkpoint-after-unprepare.json"

kubectl get resourceslices -o yaml \
  > "${DEMO_STATE_DIR}/resourceslices-after-unprepare.yaml"
grep -q 'npu-0-0' \
  "${DEMO_STATE_DIR}/resourceslices-after-unprepare.yaml"

kubectl -n "${DRIVER_NAMESPACE}" logs \
  daemonset/ascend-dra-driver-kubeletplugin \
  -c plugin --tail=500 \
  > "${DEMO_STATE_DIR}/driver-after-unprepare.log"

success "Claim CDI, checkpoint entries, and local shmem were cleaned"
