# Copyright 2023 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

ARG GOLANG_VERSION=undefined
ARG BASE_IMAGE=undefined
ARG LIBVNPU_BUILD_IMAGE=ascendai/cann:9.0.0-devel

# Stage 1: Build HAMi libvnpu (Rust)
# Uses ${LIBVNPU_BUILD_IMAGE} as the build environment to provide CANN
# headers/libs required by hami-vnpu-core. rustup is installed on-the-fly
# so no fixed Rust version needs to be maintained in this Dockerfile.
FROM ${LIBVNPU_BUILD_IMAGE} AS libvnpu-builder

# Install latest rustup (which brings in the latest stable cargo/rustc)
ENV RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable --no-modify-path \
    && chmod -R a+w ${RUSTUP_HOME} ${CARGO_HOME}
ENV PATH="${CARGO_HOME}/bin:${PATH}"

WORKDIR /build
COPY hami-vnpu-core/ ./hami-vnpu-core/
RUN if [ -f hami-vnpu-core/Cargo.toml ]; then \
        cd hami-vnpu-core && cargo build --release; \
    else \
        echo "WARN: hami-vnpu-core submodule not present, skipping libvnpu build"; \
    fi

RUN mkdir -p /hami-vnpu-core-assets
RUN if [ -f /build/hami-vnpu-core/target/release/libvnpu.so ]; then \
        cp /build/hami-vnpu-core/target/release/libvnpu.so /hami-vnpu-core-assets && \
        cp /build/hami-vnpu-core/target/release/limiter /hami-vnpu-core-assets && \
        echo "/hami-vnpu-core/libvnpu.so" \
          > /hami-vnpu-core-assets/ld.so.preload; \
    fi

# Stage 2: Build Go driver
FROM golang:${GOLANG_VERSION} AS build
WORKDIR /build
COPY . .
RUN mkdir /artifacts
RUN make PREFIX=/artifacts cmds

# Stage 3: Final image
FROM ${BASE_IMAGE}

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl iputils-ping wget \
    && rm -rf /var/lib/apt/lists/*

LABEL io.k8s.display-name="Ascend DRA Resource Driver for Kubernetes"
LABEL name="Ascend DRA Resource Driver for Kubernetes"
LABEL vendor="example.com"
ARG VERSION="N/A"
LABEL version=${VERSION}
ARG RELEASE="N/A"
LABEL release="N/A"
LABEL summary="Ascend DRA resource driver with HAMi libvnpu consumable capacity support"
LABEL description="See summary"

COPY --from=libvnpu-builder /hami-vnpu-core-assets /usr/local/hami-vnpu-core-assets
COPY --from=build /artifacts/ascend-dra-kubeletplugin /usr/bin/ascend-dra-kubeletplugin
RUN chmod +x /usr/local/hami-vnpu-core-assets/limiter
