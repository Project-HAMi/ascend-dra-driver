#!/usr/bin/env bash

set -ex
set -o pipefail

CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"
source "${CURRENT_DIR}/common.sh"

# 检查 minikube 是否已启动，若已存在则跳过创建
if minikube status --profile="${MINIKUBE_PROFILE_NAME}" &>/dev/null; then
  echo "Minikube cluster (profile: ${MINIKUBE_PROFILE_NAME}) already exists. Skip creation."
  exit 0
fi

# **启动 minikube**
minikube start \
  --profile="${MINIKUBE_PROFILE_NAME}" \
  --driver=docker \
  --container-runtime=containerd \
  --feature-gates=DynamicResourceAllocation=true \
  --extra-config=apiserver.runtime-config=resource.k8s.io/v1beta1=true \
  --extra-config=apiserver.v=1 \
  --extra-config=controller-manager.v=1 \
  --extra-config=scheduler.v=1 \
  --extra-config=kubelet.v=1 \
  --mount \
  --mount --mount-string="/usr/local/Ascend/:/usr/local/Ascend/" \
  --wait=all

# 打开CDI
docker exec "${MINIKUBE_PROFILE_NAME}" sed -i '/\[plugins."io.containerd.grpc.v1.cri"\]/a \    enable_cdi = true' /etc/containerd/config.toml
docker exec "${MINIKUBE_PROFILE_NAME}" systemctl restart containerd

# 设置默认
minikube profile "${MINIKUBE_PROFILE_NAME}"

echo "Minikube cluster (${MINIKUBE_PROFILE_NAME}) is ready!"
