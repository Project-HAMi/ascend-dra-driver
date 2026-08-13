#!/usr/bin/env bash

set -euo pipefail

CURRENT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
# shellcheck source=common.sh
source "${CURRENT_DIR}/common.sh"

cluster_exists || fail "Kind cluster ${KIND_CLUSTER_NAME} does not exist"
kubectl get runtimeclass ascend >/dev/null ||
  fail "RuntimeClass ascend is missing; run ${DEMO_DIR}/demo.sh setup"
kubectl get deviceclass ascend-vnpu-same-device-e2e >/dev/null ||
  fail "demo DeviceClass is missing; run ${DEMO_DIR}/demo.sh setup"
kubectl get namespace "${TEST_NAMESPACE}" >/dev/null ||
  fail "test namespace ${TEST_NAMESPACE} is missing; run ${DEMO_DIR}/demo.sh setup"

for resource in \
  resourceclaim/npu-share-a resourceclaim/npu-share-b \
  pod/npu-share-a pod/npu-share-b; do
  if kubectl -n "${TEST_NAMESPACE}" get "${resource}" >/dev/null 2>&1; then
    fail "${resource} already exists; run ${DEMO_DIR}/demo.sh unprepare before preparing again"
  fi
done

section "Create the demo ResourceClaims and Pods"

render_template \
  "${DEMO_DIR}/templates/workloads.yaml.tpl" \
  "${DEMO_STATE_DIR}/workloads.yaml" \
  TEST_NAMESPACE "${TEST_NAMESPACE}" \
  WORKLOAD_IMAGE "${WORKLOAD_IMAGE}"
kubectl apply -f "${DEMO_STATE_DIR}/workloads.yaml"

success "ResourceClaims and workload Pods were created; run ${DEMO_DIR}/demo.sh verify"
