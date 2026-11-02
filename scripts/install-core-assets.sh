#!/bin/sh
# Copyright 2026 The HAMi Authors.
# SPDX-License-Identifier: Apache-2.0
set -eu

assets=${1:-/usr/local/hami-vnpu-core-assets}
root=${2:-/usr/local/hami-vnpu-core}
revision=$(cat "$assets/core-revision")
layout=$(cat "$assets/core-layout")
case "$revision" in ''|*[!0-9a-f]*) echo "invalid core revision" >&2; exit 1 ;; esac
test "${#revision}" -eq 40
test "$revision" = 98ec21f47d6c42c9ef092e7e7e55f10e7f838072
test "$layout" = per-device-v1
(cd "$assets" && sha256sum -c libvnpu.sha256)
grep -Fxq '/hami-vnpu-core/libvnpu.so' "$assets/ld.so.preload"

mkdir -p "$root/releases"
target="$root/releases/$revision"
verify_existing() {
    cmp "$assets/libvnpu.sha256" "$target/libvnpu.sha256"
    cmp "$assets/core-layout" "$target/core-layout"
    cmp "$assets/core-revision" "$target/core-revision"
    cmp "$assets/ld.so.preload" "$target/ld.so.preload"
    (cd "$target" && sha256sum -c libvnpu.sha256)
}
if [ -d "$target" ]; then
    # Never overwrite a library potentially mapped by a running workload.
    verify_existing
    exit 0
fi
stage=$(mktemp -d "$root/releases/.install.XXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
for asset in libvnpu.so ld.so.preload libvnpu.sha256 core-revision core-layout; do
    cp "$assets/$asset" "$stage/$asset"
    chmod 0644 "$stage/$asset"
done
chmod 0755 "$stage"
if ! mv -T "$stage" "$target"; then
    # A concurrent installer can win; accept only identical verified assets.
    verify_existing
fi
