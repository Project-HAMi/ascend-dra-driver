#!/usr/bin/env bash

set -euo pipefail

DEMO_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"

usage() {
  cat <<'EOF'
Usage: demo.sh COMMAND

Commands:
  setup       Check/build the environment, create Kind, and install the driver
  run         Create two same-device Claims and verify the soft split
  verify      Re-run the read-only workload assertions
  all         Run setup and run in sequence
  unprepare   Delete the workload Pods and verify ResourceUnprepare cleanup
  finish      Run unprepare and cleanup
  cleanup     Delete this demo's resources and Kind cluster, retaining images
  status      Show cluster, Pod, ResourceClaim, and driver status

Environment overrides are documented in README.md.
EOF
}

on_error() {
  local status=$?
  printf '\nDemo failed with status %s.\n' "${status}" >&2
  printf 'The cluster is intentionally retained for diagnosis.\n' >&2
  printf 'Inspect it with: %s/demo.sh status\n' "${DEMO_DIR}" >&2
  printf 'Clean it with:   %s/demo.sh cleanup\n' "${DEMO_DIR}" >&2
  exit "${status}"
}

trap on_error ERR

command_name="${1:-}"
case "${command_name}" in
setup)
  "${DEMO_DIR}/scripts/prepare-host.sh"
  "${DEMO_DIR}/scripts/build-images.sh"
  "${DEMO_DIR}/scripts/create-cluster.sh"
  "${DEMO_DIR}/scripts/install-driver.sh"
  ;;
run)
  "${DEMO_DIR}/scripts/run-workload.sh"
  "${DEMO_DIR}/scripts/verify-workload.sh"
  ;;
verify)
  "${DEMO_DIR}/scripts/verify-workload.sh"
  ;;
all)
  "${DEMO_DIR}/demo.sh" setup
  "${DEMO_DIR}/demo.sh" run
  ;;
unprepare)
  "${DEMO_DIR}/scripts/unprepare.sh"
  ;;
finish)
  "${DEMO_DIR}/demo.sh" unprepare
  "${DEMO_DIR}/demo.sh" cleanup
  ;;
cleanup)
  "${DEMO_DIR}/scripts/cleanup.sh"
  ;;
status)
  "${DEMO_DIR}/scripts/status.sh"
  ;;
-h | --help | help)
  usage
  ;;
*)
  usage >&2
  exit 2
  ;;
esac

trap - ERR
