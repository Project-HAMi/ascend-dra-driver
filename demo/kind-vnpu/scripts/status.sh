#!/usr/bin/env bash

set -euo pipefail

CURRENT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
# shellcheck source=common.sh
source "${CURRENT_DIR}/common.sh"

section "Demo state"
printf 'state_dir=%s\ncluster=%s\ndriver_image=%s\n' \
  "${DEMO_STATE_DIR}" "${KIND_CLUSTER_NAME}" "${DRIVER_IMAGE}"

if ! cluster_exists; then
  warn "Kind cluster ${KIND_CLUSTER_NAME} does not exist"
  exit 0
fi

kubectl get nodes -o wide
kubectl get pods -A -o wide
kubectl get resourceslices -o wide || true
kubectl -n "${TEST_NAMESPACE}" get \
  resourceclaims,pods -o wide 2>/dev/null || true

kubectl -n "${DRIVER_NAMESPACE}" logs \
  daemonset/ascend-dra-driver-kubeletplugin \
  -c plugin --tail=100 2>/dev/null || true
