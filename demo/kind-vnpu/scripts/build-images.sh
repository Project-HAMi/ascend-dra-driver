#!/usr/bin/env bash

set -euo pipefail

CURRENT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
# shellcheck source=common.sh
source "${CURRENT_DIR}/common.sh"

require_command docker
require_file "${STATE_FILE}"
select_kind_cluster_image
require_boolean SKIP_EXISTING_IMAGE_BUILDS "${SKIP_EXISTING_IMAGE_BUILDS}"

section "Prepare the Kind node images"

if ! docker image inspect "${KIND_NODE_IMAGE}" >/dev/null 2>&1; then
  docker pull --platform=linux/arm64 "${KIND_NODE_IMAGE}"
fi
[[ "$(docker image inspect "${KIND_NODE_IMAGE}" --format '{{.Architecture}}')" == "arm64" ]] ||
  fail "${KIND_NODE_IMAGE} is not an arm64 image"
record_demo_image "${KIND_NODE_IMAGE}"

if [[ "${KIND_RESOLVED_CGROUP_MODE}" == "cgroupfs" ]]; then
  if ! docker image inspect "${CUSTOM_KIND_NODE_IMAGE}" >/dev/null 2>&1; then
    docker build \
      --build-arg "KIND_NODE_IMAGE=${KIND_NODE_IMAGE}" \
      -f "${DEMO_DIR}/config/Dockerfile.ascend" \
      -t "${CUSTOM_KIND_NODE_IMAGE}" \
      "${DEMO_DIR}/config"
  fi
  record_demo_image "${CUSTOM_KIND_NODE_IMAGE}"

  docker run --rm --entrypoint sh "${CUSTOM_KIND_NODE_IMAGE}" -c '
    set -e
    test -x /usr/local/bin/ascend-docker-runtime-wrapper
    grep -q "containerd.runtimes.ascend" /etc/containerd/config.toml
    grep -q "SystemdCgroup = false" /etc/containerd/config.toml
    grep -q "enable_cdi = true" /etc/containerd/config.toml
  '
elif [[ "${KIND_RESOLVED_CGROUP_MODE}" == "systemd" ]]; then
  success "cgroupfs compatibility is not needed; skipping custom Kind image builds"
else
  fail "unsupported resolved Kind cgroup mode: ${KIND_RESOLVED_CGROUP_MODE}"
fi

[[ "$(docker image inspect "${KIND_CLUSTER_IMAGE}" --format '{{.Architecture}}')" == "arm64" ]] ||
  fail "${KIND_CLUSTER_IMAGE} is not an arm64 image"
success "Kind node image is ready: ${KIND_CLUSTER_IMAGE}"

section "Build the DRA driver image"

driver_image_exists=false
if docker image inspect "${DRIVER_IMAGE}" >/dev/null 2>&1; then
  driver_image_exists=true
fi

if [[ "${SKIP_EXISTING_IMAGE_BUILDS}" == "true" &&
  "${driver_image_exists}" == "true" ]]; then
  success "reusing existing driver image: ${DRIVER_IMAGE}"
else
  if [[ "${driver_image_exists}" == "true" ]]; then
    warn "rebuilding existing driver image: ${DRIVER_IMAGE}"
  else
    warn "driver image is missing and will be built: ${DRIVER_IMAGE}"
  fi

  docker image inspect "${CANN_IMAGE}" >/dev/null 2>&1 ||
    fail "CANN build/runtime image is missing: ${CANN_IMAGE}"
  docker image inspect "${GOLANG_IMAGE}" >/dev/null 2>&1 ||
    fail "Go builder image is missing: ${GOLANG_IMAGE}"

  (
    cd "${PROJECT_DIR}"
    git submodule update --init --recursive
    DOCKER_BUILDKIT=1 docker build --progress=plain \
      --build-arg "GOLANG_VERSION=${GOLANG_VERSION}" \
      --build-arg "LIBVNPU_BUILD_IMAGE=${CANN_IMAGE}" \
      --build-arg "BASE_IMAGE=${CANN_IMAGE}" \
      --build-arg "HTTP_PROXY=${HTTP_PROXY:-}" \
      --build-arg "HTTPS_PROXY=${HTTPS_PROXY:-}" \
      --build-arg "GOPROXY=${GOPROXY:-https://proxy.golang.org,direct}" \
      -f deployments/container/Dockerfile \
      -t "${DRIVER_IMAGE}" \
      .
  )
fi

[[ "$(docker image inspect "${DRIVER_IMAGE}" --format '{{.Architecture}}')" == "arm64" ]] ||
  fail "${DRIVER_IMAGE} is not an arm64 image"
record_demo_image "${DRIVER_IMAGE}"

docker run --rm --entrypoint sh "${DRIVER_IMAGE}" -c '
  set -e
  /usr/bin/ascend-dra-kubeletplugin --version
  test -f /usr/local/hami-vnpu-core-assets/libvnpu.so
  test -f /usr/local/hami-vnpu-core-assets/ld.so.preload
  test ! -e /usr/local/hami-vnpu-core-assets/limiter
  grep -Fx /hami-vnpu-core/libvnpu.so \
    /usr/local/hami-vnpu-core-assets/ld.so.preload
'

if docker run --rm --entrypoint sh "${DRIVER_IMAGE}" \
  -c 'command -v readelf >/dev/null 2>&1'; then
  docker run --rm --entrypoint sh "${DRIVER_IMAGE}" -c '
    readelf -d /usr/local/hami-vnpu-core-assets/libvnpu.so |
      grep -F libdcmi.so
  '
else
  warn "readelf is unavailable in ${DRIVER_IMAGE}; skipping the optional libdcmi dependency check"
fi

save_state
success "driver image is ready: ${DRIVER_IMAGE}"
