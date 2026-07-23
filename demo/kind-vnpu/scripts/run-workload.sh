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

if kubectl get namespace "${TEST_NAMESPACE}" >/dev/null 2>&1; then
  fail "test namespace ${TEST_NAMESPACE} already exists; clean it before rerunning"
fi

section "Create two Claims for the same physical NPU"

render_template \
  "${DEMO_DIR}/templates/workloads.yaml.tpl" \
  "${DEMO_STATE_DIR}/workloads.yaml" \
  TEST_NAMESPACE "${TEST_NAMESPACE}" \
  DRIVER_IMAGE "${DRIVER_IMAGE}"

kubectl apply -f "${DEMO_STATE_DIR}/workloads.yaml"

kubectl -n "${TEST_NAMESPACE}" wait \
  --for=jsonpath='{.status.allocation}' \
  resourceclaim/npu-share-a --timeout=5m
kubectl -n "${TEST_NAMESPACE}" wait \
  --for=jsonpath='{.status.allocation}' \
  resourceclaim/npu-share-b --timeout=5m
kubectl -n "${TEST_NAMESPACE}" wait \
  --for=condition=Ready pod/npu-share-a --timeout=5m
kubectl -n "${TEST_NAMESPACE}" wait \
  --for=condition=Ready pod/npu-share-b --timeout=5m

kubectl -n "${TEST_NAMESPACE}" get pods -o wide
success "both soft-split workloads are Running"
