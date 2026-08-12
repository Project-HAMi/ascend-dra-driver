#!/usr/bin/env bash

set -euo pipefail

DEMO_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
exec "${DEMO_DIR}/demo.sh" run "$@"
