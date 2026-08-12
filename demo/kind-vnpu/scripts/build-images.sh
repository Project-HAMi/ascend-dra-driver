#!/usr/bin/env bash

set -euo pipefail

CURRENT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
# shellcheck source=common.sh
source "${CURRENT_DIR}/common.sh"

require_command docker
require_file "${STATE_FILE}"

section "Prepare the Kind node images"

if ! docker image inspect "${KIND_NODE_IMAGE}" >/dev/null 2>&1; then
  docker pull --platform=linux/arm64 "${KIND_NODE_IMAGE}"
fi
[[ "$(docker image inspect "${KIND_NODE_IMAGE}" --format '{{.Architecture}}')" == "arm64" ]] ||
  fail "${KIND_NODE_IMAGE} is not an arm64 image"
record_demo_image "${KIND_NODE_IMAGE}"

if ! docker image inspect "${BASE_CUSTOM_KIND_NODE_IMAGE}" >/dev/null 2>&1; then
  docker build \
    --build-arg "KIND_NODE_IMAGE=${KIND_NODE_IMAGE}" \
    -f "${DEMO_DIR}/config/Dockerfile.cgroupfs" \
    -t "${BASE_CUSTOM_KIND_NODE_IMAGE}" \
    "${DEMO_DIR}/config"
fi
record_demo_image "${BASE_CUSTOM_KIND_NODE_IMAGE}"

if ! docker image inspect "${CUSTOM_KIND_NODE_IMAGE}" >/dev/null 2>&1; then
  docker build \
    --build-arg "BASE_KIND_NODE_IMAGE=${BASE_CUSTOM_KIND_NODE_IMAGE}" \
    -f "${DEMO_DIR}/config/Dockerfile.ascend" \
    -t "${CUSTOM_KIND_NODE_IMAGE}" \
    "${DEMO_DIR}/config"
fi
record_demo_image "${CUSTOM_KIND_NODE_IMAGE}"

[[ "$(docker image inspect "${CUSTOM_KIND_NODE_IMAGE}" --format '{{.Architecture}}')" == "arm64" ]] ||
  fail "${CUSTOM_KIND_NODE_IMAGE} is not an arm64 image"

docker run --rm --entrypoint sh "${CUSTOM_KIND_NODE_IMAGE}" -c '
  set -e
  test -x /usr/local/bin/ascend-docker-runtime-wrapper
  grep -q "containerd.runtimes.ascend" /etc/containerd/config.toml
  grep -q "SystemdCgroup = false" /etc/containerd/config.toml
  grep -q "enable_cdi = true" /etc/containerd/config.toml
'
success "Kind node image is ready: ${CUSTOM_KIND_NODE_IMAGE}"

section "Build the DRA driver image"

docker image inspect "${CANN_IMAGE}" >/dev/null 2>&1 ||
  fail "CANN build/runtime image is missing: ${CANN_IMAGE}"
docker image inspect "${GOLANG_IMAGE}" >/dev/null 2>&1 ||
  fail "Go builder image is missing: ${GOLANG_IMAGE}"

if [[ "${REUSE_DRIVER_IMAGE}" != "true" ]] ||
  ! docker image inspect "${DRIVER_IMAGE}" >/dev/null 2>&1; then
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
  readelf -d /usr/local/hami-vnpu-core-assets/libvnpu.so |
    grep -F libdcmi.so
'

save_state
success "driver image is ready: ${DRIVER_IMAGE}"
