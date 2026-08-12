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

ARG BASE_IMAGE=ubuntu:20.04
FROM ${BASE_IMAGE} AS build

ARG GOLANG_VERSION=1.26.2
ARG TARGETARCH
ARG GOPROXY=https://proxy.golang.org,direct
ARG VERSION=unknown
ARG REVISION=unknown
ARG BUILD_DATE=unknown

ENV DEBIAN_FRONTEND=noninteractive \
    GOPATH=/go \
    GOPROXY=${GOPROXY} \
    PATH=/usr/local/go/bin:/go/bin:${PATH}

RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates gcc libc6-dev make wget \
    && rm -rf /var/lib/apt/lists/* \
    && wget -qO /tmp/go.tgz "https://go.dev/dl/go${GOLANG_VERSION}.linux-${TARGETARCH}.tar.gz" \
    && tar -C /usr/local -xzf /tmp/go.tgz \
    && rm /tmp/go.tgz

WORKDIR /build
COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /artifacts && \
    make PREFIX=/artifacts VERSION=${VERSION} REVISION=${REVISION} BUILD_DATE=${BUILD_DATE} cmds

FROM ${BASE_IMAGE}

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl iputils-ping wget \
    && rm -rf /var/lib/apt/lists/*

LABEL io.k8s.display-name="Ascend NPU Resource Driver for Dynamic Resource Allocation (DRA)"
LABEL name="Ascend NPU Resource Driver for Dynamic Resource Allocation (DRA)"
LABEL vendor="hami.io"
ARG VERSION="N/A"
LABEL version=${VERSION}
ARG RELEASE="N/A"
LABEL release=${RELEASE}
LABEL summary="Ascend NPU DRA resource driver with HAMi libvnpu consumable capacity support"
LABEL description="Resource driver for exposing and partitioning Ascend NPUs through Kubernetes DRA"

COPY dist/hami-vnpu-core/ /usr/local/hami-vnpu-core-assets/
COPY --from=build /artifacts/ascend-dra-kubeletplugin /usr/bin/ascend-dra-kubeletplugin

ENTRYPOINT ["/usr/bin/ascend-dra-kubeletplugin"]
