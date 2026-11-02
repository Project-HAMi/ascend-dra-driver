#!/bin/sh
# Copyright 2026 The HAMi Authors.
# SPDX-License-Identifier: Apache-2.0

set -eu

chart=${1:-deployments/helm/ascend-dra-driver}
rendered=$(helm template ascend-dra-driver "$chart" --namespace ascend-dra-driver)
image=$(printf '%s\n' "$rendered" | awk '$1 == "image:" && !seen { print $2; seen = 1 }')
test -n "$image"

if ! docker manifest inspect "$image" >/dev/null; then
    echo "Cannot inspect default driver image $image. Publish the matching version and verify registry access before releasing this chart." >&2
    exit 1
fi
