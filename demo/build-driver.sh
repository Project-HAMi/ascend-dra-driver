#!/usr/bin/env bash

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

# This scripts invokes `kind build image` so that the resulting
# image has a containerd with CDI support.
#
# Usage: kind-build-image.sh <tag of generated image>

# A reference to the current directory where this script is located
CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

set -ex
set -o pipefail

source "${CURRENT_DIR}/scripts/common.sh"

# Build the example driver image
${SCRIPTS_DIR}/build-driver-image.sh

# 2. 若 minikube 集群已经运行，则加载镜像
if minikube status --profile="${MINIKUBE_PROFILE_NAME}" &>/dev/null; then
  echo "Minikube cluster '${MINIKUBE_PROFILE_NAME}' detected; loading driver image..."
  minikube image load "${DRIVER_IMAGE}" --profile="${MINIKUBE_PROFILE_NAME}"
else
  echo "No running minikube cluster named '${MINIKUBE_PROFILE_NAME}' found. Skip loading image."
fi

set +x
printf '\033[0;32m'
echo "Driver build complete: ${DRIVER_IMAGE}"
printf '\033[0m'
