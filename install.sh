#!/usr/bin/env bash
set -euo pipefail

OWNER=${OWNER:-cosdt}
REPO_NAME=${REPO_NAME:-ascend-dra-driver}
NAMESPACE=${NAMESPACE:-ascend-dra-driver}
VERSION=${VERSION:-latest}

if [ "$VERSION" = "latest" ]; then
  echo "Fetching latest release version from $OWNER/$REPO_NAME..."
  VERSION=$(curl -s https://api.github.com/repos/$OWNER/$REPO_NAME/releases/latest \
    | grep '"tag_name":' \
    | sed -E 's/.*"([^"]+)".*/\1/') || {
      echo "❌ Failed to fetch latest release version."
      exit 1
    }
fi

ARCHIVE="ascend-dra-driver-${VERSION}.tgz"
BASE_URL="https://github.com/${OWNER}/${REPO_NAME}/releases/download/${VERSION}"

echo "🚀 Installing Ascend DRA Driver ${VERSION} into namespace ${NAMESPACE}..."

curl -sSL -o "$ARCHIVE" "${BASE_URL}/${ARCHIVE}"

helm upgrade -i \
  --create-namespace \
  --namespace $NAMESPACE \
  "$REPO_NAME" \
  "./${ARCHIVE}"

echo "✅ Ascend DRA Driver ${VERSION} installed successfully."
