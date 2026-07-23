#!/usr/bin/env bash

set -euo pipefail

CURRENT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
# shellcheck source=common.sh
source "${CURRENT_DIR}/common.sh"

section "Save the host baseline"

mkdir -p "${DEMO_STATE_DIR}/bin"

require_command docker
require_command kubectl
require_command helm
require_command python3
require_command curl
require_command sha256sum

if [[ -z "${KIND_BIN:-}" ]]; then
  if command -v kind >/dev/null 2>&1; then
    KIND_BIN="$(command -v kind)"
  elif [[ -x "${DEMO_STATE_DIR}/bin/kind" ]]; then
    KIND_BIN="${DEMO_STATE_DIR}/bin/kind"
  else
    section "Download Kind ${KIND_VERSION}"
    curl --fail --location \
      --output "${DEMO_STATE_DIR}/bin/kind-linux-arm64" \
      "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-linux-arm64"
    curl --fail --location \
      --output "${DEMO_STATE_DIR}/bin/kind-linux-arm64.sha256sum" \
      "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-linux-arm64.sha256sum"
    (
      cd "${DEMO_STATE_DIR}/bin"
      sha256sum --check kind-linux-arm64.sha256sum
    )
    mv "${DEMO_STATE_DIR}/bin/kind-linux-arm64" \
      "${DEMO_STATE_DIR}/bin/kind"
    chmod 0755 "${DEMO_STATE_DIR}/bin/kind"
    KIND_BIN="${DEMO_STATE_DIR}/bin/kind"
  fi
fi

[[ -x "${KIND_BIN}" ]] || fail "Kind binary is not executable: ${KIND_BIN}"

if cluster_exists; then
  fail "Kind cluster ${KIND_CLUSTER_NAME} already exists; inspect or clean it first"
fi

snapshot_images > "${DEMO_STATE_DIR}/docker-images.before"
docker ps -a --format '{{.ID}} {{.Names}} {{.Networks}}' |
  LC_ALL=C sort > "${DEMO_STATE_DIR}/containers.before"
docker network ls --format '{{.ID}} {{.Name}}' |
  LC_ALL=C sort > "${DEMO_STATE_DIR}/networks.before"
docker info \
  --format 'default_runtime={{.DefaultRuntime}} cgroup_driver={{.CgroupDriver}} cgroup_version={{.CgroupVersion}}' \
  > "${DEMO_STATE_DIR}/docker-info.before"
: > "${DEMO_STATE_DIR}/demo-images.expected"
: > "${DEMO_STATE_DIR}/kind-container-ids.expected"
rm -f "${DEMO_STATE_DIR}/device-share.may-have-changed"

section "Check the ARM64 Ascend host"

[[ "$(uname -m)" == "aarch64" ]] ||
  fail "this demo currently expects an aarch64 host"

require_directory /usr/local/Ascend
require_directory /usr/local/dcmi
require_file /etc/ascend_install.info
require_file /etc/ascend-docker-runtime.d/base.list
[[ -x /usr/local/Ascend/Ascend-Docker-Runtime/ascend-docker-runtime ]] ||
  fail "ascend-docker-runtime is missing"
[[ -x /usr/local/Ascend/Ascend-Docker-Runtime/ascend-docker-hook ]] ||
  fail "ascend-docker-hook is missing"

require_character_device /dev/davinci0
require_character_device /dev/davinci1
require_character_device /dev/davinci_manager
require_character_device /dev/devmm_svm
require_character_device /dev/hisi_hdc

NPU_SMI_HOST_PATH="${NPU_SMI_HOST_PATH:-$(command -v npu-smi || true)}"
[[ -n "${NPU_SMI_HOST_PATH}" && -x "${NPU_SMI_HOST_PATH}" ]] ||
  fail "npu-smi is missing from PATH"

npu-smi info || true

if [[ -z "${DEVICE_SHARE_CARD_IDS:-}" ]]; then
  if [[ -t 0 ]]; then
    read -r -p \
      "Enter physical NPU card IDs shown above, separated by spaces: " \
      DEVICE_SHARE_CARD_IDS
  else
    fail "set DEVICE_SHARE_CARD_IDS for non-interactive execution"
  fi
fi
[[ -n "${DEVICE_SHARE_CARD_IDS}" ]] ||
  fail "DEVICE_SHARE_CARD_IDS must not be empty"
[[ "${DEVICE_SHARE_CHIP_ID}" =~ ^[0-9]+$ ]] ||
  fail "DEVICE_SHARE_CHIP_ID must be a non-negative integer"

read -r -a device_share_cards <<< "${DEVICE_SHARE_CARD_IDS}"
[[ "${#device_share_cards[@]}" -eq 2 ]] ||
  fail "DEVICE_SHARE_CARD_IDS must contain exactly two card IDs"
[[ "${device_share_cards[0]}" != "${device_share_cards[1]}" ]] ||
  fail "DEVICE_SHARE_CARD_IDS must contain two distinct card IDs"

share_state_file="${DEMO_STATE_DIR}/device-share.before"
: > "${share_state_file}"
for card_id in "${device_share_cards[@]}"; do
  [[ "${card_id}" =~ ^[0-9]+$ ]] ||
    fail "invalid physical NPU card ID: ${card_id}"
  output_file="${DEMO_STATE_DIR}/device-share-card-${card_id}.before.txt"
  npu-smi info -t device-share -i "${card_id}" |
    tee "${output_file}"
  if grep -q True "${output_file}"; then
    printf '%s\tTrue\n' "${card_id}" >> "${share_state_file}"
  elif grep -q False "${output_file}"; then
    printf '%s\tFalse\n' "${card_id}" >> "${share_state_file}"
  else
    fail "cannot determine the initial device-share state for card ${card_id}"
  fi
done

save_state

success "host prerequisites and Docker baseline are ready"
cat "${DEMO_STATE_DIR}/docker-info.before"

if [[ "${RUN_GO_TESTS}" == "true" ]]; then
  section "Run the Go and submodule gate"
  (
    cd "${PROJECT_DIR}"
    git submodule update --init --recursive

    expected_commit="$(
      git ls-tree HEAD hami-vnpu-core |
        awk '{print $3}'
    )"
    actual_commit="$(git -C hami-vnpu-core rev-parse HEAD)"
    [[ -n "${expected_commit}" && "${actual_commit}" == "${expected_commit}" ]] ||
      fail "hami-vnpu-core does not match the repository gitlink"

    go test ./cmd/ascend-dra-kubeletplugin \
      -run 'TestEnableHAMivNPUDeviceShare|TestNPUSmiDeviceShareRunner|TestMockDRALibvNPU|TestLibvNPU|TestDefaultModeUnprepare' \
      -count=1 -v
    go test -count=1 ./...
    make build
  )
  success "Go tests and build passed"
else
  warn "RUN_GO_TESTS=false; the Go test gate was skipped"
fi
