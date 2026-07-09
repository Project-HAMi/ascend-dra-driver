#!/usr/bin/env bash
# Copyright 2024 The HAMi Authors.
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

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "Initializing git submodules..."
git submodule update --init --recursive

MIND_CLUSTER_DIR="$ROOT/third_party/mind-cluster"
if [ -d "$MIND_CLUSTER_DIR/.git" ]; then
    echo "Configuring sparse checkout for mind-cluster (only ascend-common)..."
    cd "$MIND_CLUSTER_DIR"
    git sparse-checkout init --cone
    git sparse-checkout set component/ascend-common
    git checkout HEAD -- .
fi

echo "Submodules initialized."
