#!/usr/bin/env bash

set -euo pipefail

DEMO_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"

usage() {
  cat <<'EOF'
Usage: demo.sh COMMAND

Commands:
  setup       Create Kind, install the driver, and prepare shared demo resources
  prepare     Create the demo ResourceClaims and Pods
  verify      Verify the prepared ResourceClaims and Pods
  unprepare   Delete the demo ResourceClaims and Pods
  cleanup     Delete the complete Kind demo environment, retaining images
  all         Run setup, prepare, verify, unprepare, and cleanup in sequence
  status      Show cluster, Pod, ResourceClaim, and driver status

Configuration defaults are stored in demo.env and documented in README.md.
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
if [[ "$#" -ne 1 ]]; then
  usage >&2
  exit 2
fi

case "${command_name}" in
setup)
  "${DEMO_DIR}/scripts/prepare-host.sh"
  "${DEMO_DIR}/scripts/build-images.sh"
  "${DEMO_DIR}/scripts/create-cluster.sh"
  "${DEMO_DIR}/scripts/install-driver.sh"
  "${DEMO_DIR}/scripts/setup-resources.sh"
  printf '\nSetup complete. No ResourceClaims or workload Pods were created.\n'
  printf 'Continue with: %s/demo.sh prepare\n' "${DEMO_DIR}"
  ;;
prepare)
  "${DEMO_DIR}/scripts/prepare-workload.sh"
  ;;
verify)
  "${DEMO_DIR}/scripts/verify-workload.sh"
  ;;
all)
  "${DEMO_DIR}/demo.sh" setup
  "${DEMO_DIR}/demo.sh" prepare
  "${DEMO_DIR}/demo.sh" verify
  "${DEMO_DIR}/demo.sh" unprepare
  "${DEMO_DIR}/demo.sh" cleanup
  ;;
unprepare)
  "${DEMO_DIR}/scripts/unprepare.sh"
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
