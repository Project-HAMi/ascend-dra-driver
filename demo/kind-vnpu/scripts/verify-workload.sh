#!/usr/bin/env bash

set -euo pipefail

CURRENT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
# shellcheck source=common.sh
source "${CURRENT_DIR}/common.sh"

cluster_exists || fail "Kind cluster ${KIND_CLUSTER_NAME} does not exist"

section "Verify DRA allocations"

kubectl -n "${TEST_NAMESPACE}" get resourceclaim \
  npu-share-a npu-share-b -o json \
  > "${DEMO_STATE_DIR}/claims.json"

python3 "${CURRENT_DIR}/assert-claims.py" \
  "${DEMO_STATE_DIR}/claims.json"

section "Verify the two independent 1Gi libvnpu quotas"

for pod in npu-share-a npu-share-b; do
  kubectl -n "${TEST_NAMESPACE}" logs "${pod}" |
    tee "${DEMO_STATE_DIR}/${pod}.log"
  grep -Eq \
    'set_device_ret=0 probe_ret=0 free=[0-9]+ total=1073741824' \
    "${DEMO_STATE_DIR}/${pod}.log"
done

section "Verify environment, preload, mounts, and device isolation"

for pod in npu-share-a npu-share-b; do
  kubectl -n "${TEST_NAMESPACE}" exec "${pod}" -- bash -c '
    set -e
    test "$ASCEND_VISIBLE_DEVICES" = "0"
    test "$NPU_MEM_QUOTA" = "1024"
    test "$NPU_PRIORITY" = "50"
    test "$NPU_GLOBAL_SHM_PATH" = \
      "/hami-shared-region/0_global_registry"
    test "$NPU_LOCAL_SHM_PATH" = \
      "/hami-vnpu-shmem/vnpu_local_shmem"

    test -f /hami-vnpu-core/libvnpu.so
    test ! -e /hami-vnpu-core/limiter
    test -f /hami-vnpu-shmem/vnpu_local_shmem
    test -f /hami-shared-region/0_global_registry
    grep -Fx /hami-vnpu-core/libvnpu.so /etc/ld.so.preload
    grep -q /hami-vnpu-core/libvnpu.so /proc/1/maps

    test -c /dev/davinci0
    test ! -e /dev/davinci1
    test -c /dev/davinci_manager
    test -c /dev/devmm_svm
    test -c /dev/hisi_hdc

    cat /sys/fs/cgroup/devices/devices.list
  '
done

section "Verify Claim-scoped CDI and checkpoint state"

UID_A="$(
  kubectl -n "${TEST_NAMESPACE}" get resourceclaim npu-share-a \
    -o jsonpath='{.metadata.uid}'
)"
UID_B="$(
  kubectl -n "${TEST_NAMESPACE}" get resourceclaim npu-share-b \
    -o jsonpath='{.metadata.uid}'
)"
export UID_A UID_B

[[ -n "${UID_A}" && -n "${UID_B}" && "${UID_A}" != "${UID_B}" ]] ||
  fail "claim UIDs are missing or identical"

printf 'uid_a=%s\nuid_b=%s\n' "${UID_A}" "${UID_B}" |
  tee "${DEMO_STATE_DIR}/claim-uids.txt"

docker exec "${WORKER}" test -f \
  "/usr/local/hami-vnpu-core/containers/${UID_A}/vnpu_local_shmem"
docker exec "${WORKER}" test -f \
  "/usr/local/hami-vnpu-core/containers/${UID_B}/vnpu_local_shmem"
docker exec "${WORKER}" test -f \
  /usr/local/hami-shared-region/0_global_registry

docker exec "${WORKER}" sh -c \
  "grep -R -l '${UID_A}' /var/run/cdi | xargs -r cat" \
  > "${DEMO_STATE_DIR}/claim-a-cdi.yaml"
docker exec "${WORKER}" sh -c \
  "grep -R -l '${UID_B}' /var/run/cdi | xargs -r cat" \
  > "${DEMO_STATE_DIR}/claim-b-cdi.yaml"

for claim in a b; do
  cdi_file="${DEMO_STATE_DIR}/claim-${claim}-cdi.yaml"
  [[ -s "${cdi_file}" ]] || fail "Claim ${claim} CDI spec is empty"
  grep -q \
    'NPU_LOCAL_SHM_PATH=/hami-vnpu-shmem/vnpu_local_shmem' \
    "${cdi_file}"
  ! grep -q '/usr/local/bin/npu-smi' "${cdi_file}"
done

grep -q "/containers/${UID_A}" \
  "${DEMO_STATE_DIR}/claim-a-cdi.yaml"
grep -q "/containers/${UID_B}" \
  "${DEMO_STATE_DIR}/claim-b-cdi.yaml"

docker exec "${WORKER}" cat \
  /var/lib/kubelet/plugins/npu.project-hami.io/checkpoint.json \
  > "${DEMO_STATE_DIR}/checkpoint-after-prepare.json"
grep -q "${UID_A}" "${DEMO_STATE_DIR}/checkpoint-after-prepare.json"
grep -q "${UID_B}" "${DEMO_STATE_DIR}/checkpoint-after-prepare.json"

save_state
success "same-device soft split, device isolation, and 1Gi quotas passed"
