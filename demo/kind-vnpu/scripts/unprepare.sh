#!/usr/bin/env bash

set -euo pipefail

CURRENT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
# shellcheck source=common.sh
source "${CURRENT_DIR}/common.sh"

cluster_exists || fail "Kind cluster ${KIND_CLUSTER_NAME} does not exist"

section "Delete the demo ResourceClaims and Pods"

kubectl -n "${TEST_NAMESPACE}" delete pod \
  npu-share-a npu-share-b --ignore-not-found --wait=true
kubectl -n "${TEST_NAMESPACE}" delete resourceclaim \
  npu-share-a npu-share-b --ignore-not-found --wait=true

unset UID_A UID_B
save_state

success "ResourceClaims and workload Pods were deleted"
