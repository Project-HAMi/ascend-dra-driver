#!/usr/bin/env bash

set -euo pipefail

CURRENT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
# shellcheck source=common.sh
source "${CURRENT_DIR}/common.sh"

require_file "${STATE_FILE}"

section "Remove only this demo's Kubernetes resources"

if cluster_exists; then
  require_file "${DEMO_STATE_DIR}/kind-container-ids.expected"
  [[ -s "${DEMO_STATE_DIR}/kind-container-ids.expected" ]] ||
    fail "cluster ownership record is empty; refusing to delete ${KIND_CLUSTER_NAME}"

  kind_container_ids > "${DEMO_STATE_DIR}/kind-container-ids.current"
  LC_ALL=C comm -13 \
    "${DEMO_STATE_DIR}/kind-container-ids.expected" \
    "${DEMO_STATE_DIR}/kind-container-ids.current" \
    > "${DEMO_STATE_DIR}/kind-container-ids.unowned"
  [[ ! -s "${DEMO_STATE_DIR}/kind-container-ids.unowned" ]] || {
    cat "${DEMO_STATE_DIR}/kind-container-ids.unowned" >&2
    fail "the named cluster contains containers not created by this demo"
  }

  if kubectl cluster-info >/dev/null 2>&1; then
    if [[ -f "${DEMO_STATE_DIR}/workloads.yaml" ]]; then
      kubectl delete -f "${DEMO_STATE_DIR}/workloads.yaml" \
        --ignore-not-found --wait=true || true
    fi
    if [[ -f "${DEMO_STATE_DIR}/setup-resources.yaml" ]]; then
      kubectl delete -f "${DEMO_STATE_DIR}/setup-resources.yaml" \
        --ignore-not-found --wait=true || true
    else
      kubectl delete namespace "${TEST_NAMESPACE}" \
        --ignore-not-found --wait=true || true
      kubectl delete deviceclass ascend-vnpu-same-device-e2e \
        --ignore-not-found || true
    fi

    helm uninstall "${HELM_RELEASE}" \
      -n "${DRIVER_NAMESPACE}" || true
    kubectl delete namespace "${DRIVER_NAMESPACE}" \
      --ignore-not-found --wait=true || true
    kubectl delete -f "${DEMO_DIR}/yaml/runtimeclass.yaml" \
      --ignore-not-found || true
  else
    warn "the API server is unavailable; deleting the named Kind cluster"
  fi

  KIND_EXPERIMENTAL_PROVIDER=docker "${KIND_BIN}" delete cluster \
    --name "${KIND_CLUSTER_NAME}"
else
  warn "Kind cluster ${KIND_CLUSTER_NAME} does not exist"
fi

[[ -z "$(docker ps -a -q --filter "name=^/${KIND_CLUSTER_NAME}-")" ]] ||
  fail "one or more demo Kind node containers remain"

section "Restore the original NPU device-share state"

if [[ -f "${DEMO_STATE_DIR}/device-share.may-have-changed" ]]; then
  require_file "${DEMO_STATE_DIR}/device-share.before"
  while IFS=$'\t' read -r card_id initial_state; do
    [[ -n "${card_id}" ]] || continue
    if [[ "${initial_state}" == "False" ]]; then
      current_file="${DEMO_STATE_DIR}/device-share-card-${card_id}.current.txt"
      "${NPU_SMI_HOST_PATH}" info -t device-share -i "${card_id}" |
        tee "${current_file}"
      if grep -q True "${current_file}"; then
        printf 'Y\n' |
          "${NPU_SMI_HOST_PATH}" set \
            -t device-share \
            -i "${card_id}" \
            -c "${DEVICE_SHARE_CHIP_ID}" \
            -d 0
      fi
    fi
    "${NPU_SMI_HOST_PATH}" info -t device-share -i "${card_id}" \
      > "${DEMO_STATE_DIR}/device-share-card-${card_id}.after.txt"
    grep -q "${initial_state}" \
      "${DEMO_STATE_DIR}/device-share-card-${card_id}.after.txt"
  done < "${DEMO_STATE_DIR}/device-share.before"
else
  warn "the driver never reached the device-share stage; no NPU state restore is needed"
fi

section "Verify the host Docker state"

if [[ -f "${DEMO_STATE_DIR}/docker-images.before" ]]; then
  verify_preexisting_images
else
  warn "Docker image baseline is missing; skipping the image set comparison"
fi

if [[ -f "${DEMO_STATE_DIR}/docker-info.before" ]]; then
  docker info \
    --format 'default_runtime={{.DefaultRuntime}} cgroup_driver={{.CgroupDriver}} cgroup_version={{.CgroupVersion}}' \
    > "${DEMO_STATE_DIR}/docker-info.after"
  cmp "${DEMO_STATE_DIR}/docker-info.before" \
    "${DEMO_STATE_DIR}/docker-info.after"
fi

if [[ -f "${DEMO_STATE_DIR}/demo-images.expected" ]]; then
  while IFS= read -r image; do
    [[ -n "${image}" ]] || continue
    docker image inspect "${image}" >/dev/null
  done < "${DEMO_STATE_DIR}/demo-images.expected"
fi

success "cluster removed; pre-existing and demo dependency images are retained"
