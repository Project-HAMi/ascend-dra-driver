# HAMivNPUCore Kind E2E 与同卡软切分复现手册

本文记录在 ARM64 Ascend 主机上复现双卡 DRA E2E 和同一物理卡双 Claim 软切分的具体命令。测试使用定制 Kind node 镜像绕过宿主 cgroup v1 与 Kind systemd cgroup hierarchy 的兼容问题，并在 Kind worker 的 containerd 中注册 Ascend runtime；整个过程不修改或重启 Docker Daemon。

测试覆盖：device-share 初始化、设备发现与发布、跨两张物理卡分配、同一物理卡两个独立 Claim、ResourcePrepare、Claim 级 local shmem、CDI 环境变量和挂载注入、Ascend runtime 设备节点及 device cgroup 注入、`libvnpu.so` 预加载，以及删除 Pod 后的 ResourceUnprepare。

远端连接、代理和机器专用信息见 `AGENTS.local.md`。以下命令均在已登录的 ARM64 Ascend 主机上执行。

## 1. 定义环境

```bash
cd /path/to/hami-dra-driver

export TEST_ROOT=/tmp/hami-dra-kind-e2e-feat-vnpu-core
export KIND_CLUSTER_NAME=hami-dra-e2e-vnpu-softsplit
export KUBECONFIG=${TEST_ROOT}/kubeconfig
export KIND_NODE_IMAGE=kindest/node:v1.34.0
export BASE_CUSTOM_KIND_NODE_IMAGE=kindest/node:v1.34.0-cgroupfs-cdi
export CUSTOM_KIND_NODE_IMAGE=kindest/node:v1.34.0-cgroupfs-cdi-ascend
export DRIVER_IMAGE=registry.example.com/ascend-dra-driver:e2e-vnpu-softsplit
export DRIVER_NAMESPACE=ascend-dra-e2e
export TEST_NAMESPACE=ascend-dra-e2e-test

mkdir -p "${TEST_ROOT}"
docker image ls --no-trunc --digests \
  --format '{{.Repository}}:{{.Tag}} {{.ID}} {{.Digest}}' \
  | sort > "${TEST_ROOT}/docker-images.before"
docker ps -a --format '{{.Names}}' | sort > "${TEST_ROOT}/containers.before"
docker network ls --format '{{.Name}}' | sort > "${TEST_ROOT}/networks.before"
```

确认基础条件。主机上的设备数量可以多于两张，但本测试至少需要前两张卡及公共管理节点：

```bash
uname -m
docker info | grep -E 'Cgroup Driver|Cgroup Version'
docker version
kind version
kubectl version --client
helm version --short

test "$(uname -m)" = aarch64
test -d /usr/local/Ascend
test -x /usr/local/bin/npu-smi
test -f /etc/ascend_install.info
test -x /usr/local/Ascend/Ascend-Docker-Runtime/ascend-docker-runtime
test -x /usr/local/Ascend/Ascend-Docker-Runtime/ascend-docker-hook
test -f /etc/ascend-docker-runtime.d/base.list
test -d /usr/local/dcmi
ls -l /dev/davinci0 /dev/davinci1 /dev/davinci_manager /dev/devmm_svm /dev/hisi_hdc
npu-smi info
```

如果 `kind`、基础镜像或 Go 依赖需要下载，按 `AGENTS.local.md` 为单条命令设置代理，不要修改 Docker Daemon。

## 2. 执行 Go 测试门禁

先确认双设备 CDI 聚合和完整 Go 测试通过：

```bash
git status --short --branch
git submodule update --init --recursive

go test ./cmd/ascend-dra-kubeletplugin \
  -run 'TestMockDRA|TestMockDRALibvNPU|TestLibvNPU' \
  -count=1 -v
go test ./...
```

Go module 下载失败时，可以仅对上述命令设置 `GOPROXY`；具体地址见 `AGENTS.local.md`。

## 3. 准备 Kind node 镜像

如果宿主还没有基础镜像，先拉取：

```bash
docker image inspect "${KIND_NODE_IMAGE}" >/dev/null || docker pull "${KIND_NODE_IMAGE}"
```

若 Docker 直连 registry 失败，可以用可用的镜像下载工具获得 ARM64 OCI/tar 包后执行 `docker load`。不要修改 Docker Daemon 的代理配置。

先创建启用 cgroupfs 和 CDI 的基础定制镜像。使用独立的空 build context，避免把测试证据或镜像 tar 包发送给 Docker builder：

```bash
mkdir -p "${TEST_ROOT}/kind-image"
cat > "${TEST_ROOT}/kind-image/Dockerfile" <<'EOF'
FROM kindest/node:v1.34.0

RUN set -eux; \
    config=/etc/containerd/config.toml; \
    sed -i \
      -e 's/enable_cdi = false/enable_cdi = true/g' \
      -e 's/SystemdCgroup = true/SystemdCgroup = false/g' \
      "$config"; \
    if ! grep -Eq '^[[:space:]]*enable_cdi = true$' "$config"; then \
      sed -i '/\[plugins\."io\.containerd\.grpc\.v1\.cri"\]/a\    enable_cdi = true' "$config"; \
    fi; \
    grep -Eq '^[[:space:]]*enable_cdi = true$' "$config"; \
    ! grep -q 'SystemdCgroup = true' "$config"; \
    grep -n -E 'enable_cdi|SystemdCgroup' "$config"
EOF

docker build \
  -f "${TEST_ROOT}/kind-image/Dockerfile" \
  -t "${BASE_CUSTOM_KIND_NODE_IMAGE}" \
  "${TEST_ROOT}/kind-image"

docker image inspect "${BASE_CUSTOM_KIND_NODE_IMAGE}" \
  --format '{{.Id}} {{.Architecture}}'
```

Ascend runtime 会通过 `dlopen` 加载 DCMI。Kind worker 不继承宿主的动态库搜索配置，因此需要一个只为该 runtime 设置 `LD_LIBRARY_PATH` 的 wrapper。随后在 containerd 中注册 `ascend` handler：

```bash
cat > "${TEST_ROOT}/kind-image/ascend-docker-runtime-wrapper" <<'EOF'
#!/bin/sh

export LD_LIBRARY_PATH="/usr/local/Ascend/driver/lib64:/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/driver/lib64/common:/usr/local/dcmi${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}"
exec /usr/local/Ascend/Ascend-Docker-Runtime/ascend-docker-runtime "$@"
EOF

cat > "${TEST_ROOT}/kind-image/Dockerfile.ascend" <<'EOF'
FROM kindest/node:v1.34.0-cgroupfs-cdi

COPY ascend-docker-runtime-wrapper /usr/local/bin/ascend-docker-runtime-wrapper

RUN set -eux; \
    chmod 0755 /usr/local/bin/ascend-docker-runtime-wrapper; \
    config=/etc/containerd/config.toml; \
    if ! grep -q 'containerd.runtimes.ascend' "$config"; then \
      printf '%s\n' \
        '' \
        '[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.ascend]' \
        '  runtime_type = "io.containerd.runc.v2"' \
        '  base_runtime_spec = "/etc/containerd/cri-base.json"' \
        '[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.ascend.options]' \
        '  BinaryName = "/usr/local/bin/ascend-docker-runtime-wrapper"' \
        '  SystemdCgroup = false' \
        >> "$config"; \
    fi; \
    grep -n -A7 'containerd.runtimes.ascend' "$config"
EOF

docker build \
  -f "${TEST_ROOT}/kind-image/Dockerfile.ascend" \
  -t "${CUSTOM_KIND_NODE_IMAGE}" \
  "${TEST_ROOT}/kind-image"

docker image inspect "${CUSTOM_KIND_NODE_IMAGE}" \
  --format '{{.Id}} {{.Architecture}}'
docker run --rm --entrypoint sh "${CUSTOM_KIND_NODE_IMAGE}" -c '
  test -x /usr/local/bin/ascend-docker-runtime-wrapper
  grep -n -A7 containerd.runtimes.ascend /etc/containerd/config.toml
'
```

本次验证通过的定制镜像 ID 是：

```text
kindest/node:v1.34.0-cgroupfs-cdi
sha256:d669cf33b5f23848ebbb1d03e0884d196cd56e7c2abe0baf25446743e7fba674 arm64

kindest/node:v1.34.0-cgroupfs-cdi-ascend
sha256:a2fe17c2144a140ca024ef5b27cf4e6c4e86b98f4dc8be3abd5e01a59f951858 arm64
```

镜像 ID 不同不一定是错误；应以架构和镜像内配置检查结果为准。

## 4. 构建驱动镜像

初始化 submodule，并使用本地 CANN devel 镜像同时构建 libvnpu 和最终运行时镜像：

```bash
git submodule update --init --recursive
docker image inspect ascendai/cann:9.0.0-devel >/dev/null
docker image inspect golang:1.26.0 >/dev/null

docker build --progress=plain \
  --build-arg GOLANG_VERSION=1.26.0 \
  --build-arg LIBVNPU_BUILD_IMAGE=ascendai/cann:9.0.0-devel \
  --build-arg BASE_IMAGE=ascendai/cann:9.0.0-devel \
  --build-arg HTTP_PROXY="${HTTP_PROXY:-}" \
  --build-arg HTTPS_PROXY="${HTTPS_PROXY:-}" \
  --build-arg GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" \
  -f deployments/container/Dockerfile \
  -t "${DRIVER_IMAGE}" \
  .

docker image inspect "${DRIVER_IMAGE}" --format '{{.Id}} {{.Architecture}} {{.Size}}'
docker run --rm --entrypoint sh "${DRIVER_IMAGE}" -c '
  test -x /usr/bin/ascend-dra-kubeletplugin
  test -f /usr/local/hami-vnpu-core-assets/libvnpu.so
  test ! -e /usr/local/hami-vnpu-core-assets/limiter
  test -f /usr/local/hami-vnpu-core-assets/ld.so.preload
  readelf -d /usr/local/hami-vnpu-core-assets/libvnpu.so | grep -F libdcmi.so
'
```

本次最终驱动镜像 ID 是：

```text
sha256:f567686cc9baac4729515995f59c367b67f81632e037fde3048686d43bf2e2b8
```

## 5. 创建 Kind 集群

生成双节点配置。只给 worker 透传 Ascend 路径与设备；不要把宿主 `/var/run/cdi`、`/usr/local/hami-vnpu-core`、`/usr/local/hami-shared-region` 或 kubelet 状态目录挂入 Kind。

```bash
cat > "${TEST_ROOT}/kind-config.yaml" <<'EOF'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
featureGates:
  DynamicResourceAllocation: true
  DRAConsumableCapacity: true
nodes:
  - role: control-plane
    kubeadmConfigPatches:
      - |-
        kind: KubeletConfiguration
        apiVersion: kubelet.config.k8s.io/v1beta1
        cgroupDriver: cgroupfs
  - role: worker
    labels:
      npu.project-hami.io/e2e-node: "true"
    extraMounts:
      - hostPath: /usr/local/Ascend
        containerPath: /usr/local/Ascend
        readOnly: true
      - hostPath: /usr/local/dcmi
        containerPath: /usr/local/dcmi
        readOnly: true
      - hostPath: /usr/local/bin/npu-smi
        containerPath: /usr/local/bin/npu-smi
        readOnly: true
      - hostPath: /etc/ascend_install.info
        containerPath: /etc/ascend_install.info
        readOnly: true
      - hostPath: /etc/ascend-docker-runtime.d
        containerPath: /etc/ascend-docker-runtime.d
        readOnly: true
      - hostPath: /dev/davinci0
        containerPath: /dev/davinci0
      - hostPath: /dev/davinci1
        containerPath: /dev/davinci1
      - hostPath: /dev/davinci2
        containerPath: /dev/davinci2
      - hostPath: /dev/davinci_manager
        containerPath: /dev/davinci_manager
      - hostPath: /dev/devmm_svm
        containerPath: /dev/devmm_svm
      - hostPath: /dev/hisi_hdc
        containerPath: /dev/hisi_hdc
EOF

KIND_EXPERIMENTAL_PROVIDER=docker kind create cluster \
  --name "${KIND_CLUSTER_NAME}" \
  --image "${CUSTOM_KIND_NODE_IMAGE}" \
  --config "${TEST_ROOT}/kind-config.yaml" \
  --kubeconfig "${KUBECONFIG}" \
  --wait 5m
```

如果宿主没有 `/dev/davinci2`，从配置中删除对应 mount；至少保留 `/dev/davinci0` 和 `/dev/davinci1`。

验证节点、cgroup、CDI 和 Ascend runtime handler：

```bash
kubectl get nodes -o wide
kubectl wait --for=condition=Ready node --all --timeout=180s

WORKER=${KIND_CLUSTER_NAME}-worker
docker exec "${WORKER}" grep -nE 'enable_cdi|SystemdCgroup' /etc/containerd/config.toml
docker exec "${WORKER}" grep -n -A7 containerd.runtimes.ascend /etc/containerd/config.toml
docker exec "${WORKER}" crictl info | grep -n -A18 -B2 ascend
docker exec "${WORKER}" sh -c '
  test -e /dev/davinci0
  test -e /dev/davinci1
  test -d /usr/local/Ascend
  test -x /usr/local/Ascend/Ascend-Docker-Runtime/ascend-docker-runtime
  test -x /usr/local/Ascend/Ascend-Docker-Runtime/ascend-docker-hook
  test -f /etc/ascend-docker-runtime.d/base.list
'
kubectl get --raw /apis/resource.k8s.io/v1 >/dev/null

cat > "${TEST_ROOT}/ascend-runtimeclass.yaml" <<'EOF'
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: ascend
handler: ascend
EOF

kubectl apply -f "${TEST_ROOT}/ascend-runtimeclass.yaml"
kubectl get runtimeclass ascend -o yaml
```

## 6. 导入并部署驱动

```bash
kind load docker-image "${DRIVER_IMAGE}" --name "${KIND_CLUSTER_NAME}"
docker exec "${WORKER}" crictl images | grep ascend-dra-driver

cat > "${TEST_ROOT}/e2e-values.yaml" <<'EOF'
fullnameOverride: ascend-dra-driver
image:
  repository: registry.example.com/ascend-dra-driver
  tag: e2e-vnpu-softsplit
  pullPolicy: Never
kubeletPlugin:
  npuSmiHostPath: /usr/local/bin/npu-smi
  nodeSelector:
    npu.project-hami.io/e2e-node: "true"
EOF

helm upgrade --install ascend-dra-driver \
  deployments/helm/ascend-dra-driver \
  --namespace "${DRIVER_NAMESPACE}" \
  --create-namespace \
  -f "${TEST_ROOT}/e2e-values.yaml"
```

当前 chart 中插件命令仍是 `sleep infinity`，测试时必须将它改为真实插件进程：

```bash
kubectl -n "${DRIVER_NAMESPACE}" patch daemonset ascend-dra-driver-kubeletplugin \
  --type='json' \
  -p='[
    {"op":"replace","path":"/spec/template/spec/containers/0/command","value":["/usr/bin/ascend-dra-kubeletplugin"]},
    {"op":"add","path":"/spec/template/spec/containers/0/args","value":["--feature-gates=HAMivNPUCore=true"]}
  ]'

kubectl -n "${DRIVER_NAMESPACE}" rollout status \
  daemonset/ascend-dra-driver-kubeletplugin --timeout=5m
kubectl -n "${DRIVER_NAMESPACE}" get pods -o wide
kubectl -n "${DRIVER_NAMESPACE}" logs \
  daemonset/ascend-dra-driver-kubeletplugin -c plugin --tail=300
```

等待并检查 ResourceSlice。预期至少有两个设备，且设备具有 `allowMultipleAllocations: true`、`memory` 和 `aicore` capacity：

```bash
kubectl get resourceslices
kubectl get resourceslices -o yaml | tee "${TEST_ROOT}/resourceslices-before-claim.yaml"
```

## 7. 创建双卡 Claim 和工作负载

```bash
cat > "${TEST_ROOT}/two-device-e2e.yaml" <<'EOF'
apiVersion: v1
kind: Namespace
metadata:
  name: ascend-dra-e2e-test
---
apiVersion: resource.k8s.io/v1
kind: DeviceClass
metadata:
  name: ascend-vnpu-e2e
spec:
  selectors:
    - cel:
        expression: >-
          device.driver == "npu.project-hami.io" &&
          device.allowMultipleAllocations == true &&
          device.attributes["npu.project-hami.io"].type == "NPU"
---
apiVersion: resource.k8s.io/v1
kind: ResourceClaim
metadata:
  namespace: ascend-dra-e2e-test
  name: two-npu
spec:
  devices:
    requests:
      - name: npu
        exactly:
          deviceClassName: ascend-vnpu-e2e
          allocationMode: ExactCount
          count: 2
          capacity:
            requests:
              npu.project-hami.io/memory: 1Gi
              npu.project-hami.io/aicore: 50
    constraints:
      - requests: [npu]
        distinctAttribute: npu.project-hami.io/index
---
apiVersion: v1
kind: Pod
metadata:
  namespace: ascend-dra-e2e-test
  name: two-npu-workload
spec:
  runtimeClassName: ascend
  restartPolicy: Never
  nodeSelector:
    npu.project-hami.io/e2e-node: "true"
  containers:
    - name: workload
      image: registry.example.com/ascend-dra-driver:e2e-vnpu-softsplit
      imagePullPolicy: Never
      command: ["/bin/bash", "-c"]
      args:
        - |
          set -e
          trap 'exit 0' TERM INT
          while true; do sleep 3600 & wait $!; done
      resources:
        claims:
          - name: npu
  resourceClaims:
    - name: npu
      resourceClaimName: two-npu
EOF

kubectl apply -f "${TEST_ROOT}/two-device-e2e.yaml"
kubectl -n "${TEST_NAMESPACE}" wait \
  --for=jsonpath='{.status.allocation}' resourceclaim/two-npu \
  --timeout=5m
kubectl -n "${TEST_NAMESPACE}" wait \
  --for=condition=Ready pod/two-npu-workload \
  --timeout=5m

kubectl -n "${TEST_NAMESPACE}" get resourceclaim two-npu -o yaml \
  | tee "${TEST_ROOT}/final-claim.yaml"
kubectl -n "${TEST_NAMESPACE}" get pod two-npu-workload -o wide
```

检查 allocation 的 `results` 恰好包含两个不同设备、两个非空 `shareID`、相同 worker pool，以及每张卡的 `1Gi` memory 和 `50` aicore consumed capacity。

## 8. 验证 CDI 注入

```bash
kubectl -n "${TEST_NAMESPACE}" exec two-npu-workload -- bash -c '
  set -e
  tr "\0" "\n" < /proc/1/environ | grep -E "^(ASCEND_VISIBLE_DEVICES|NPU_MEM_QUOTA|NPU_PRIORITY|NPU_GLOBAL_SHM_PATH)="
  test -f /hami-vnpu-core/libvnpu.so
  test ! -e /hami-vnpu-core/limiter
  grep -Fx /hami-vnpu-core/libvnpu.so /etc/ld.so.preload
  test -w /hami-shared-region
  test -w /hami-vnpu-shmem
  test "$NPU_LOCAL_SHM_PATH" = /hami-vnpu-shmem/vnpu_local_shmem
  grep -F /hami-vnpu-core/libvnpu.so /proc/1/maps
  mount | grep -E "hami-vnpu-core|hami-shared-region|ld.so.preload|Ascend"
  test -c /dev/davinci0
  test -c /dev/davinci1
  test -c /dev/davinci_manager
  test -c /dev/devmm_svm
  test -c /dev/hisi_hdc
  test ! -e /dev/davinci2
  ls -l /dev/davinci0 /dev/davinci1 /dev/davinci_manager /dev/devmm_svm /dev/hisi_hdc
  cat /sys/fs/cgroup/devices/devices.list
'
```

预期关键值：

```text
ASCEND_VISIBLE_DEVICES=0,1
NPU_MEM_QUOTA=1024
NPU_PRIORITY=50
NPU_GLOBAL_SHM_PATH=/hami-shared-region/0_global_registry
NPU_LOCAL_SHM_PATH=/hami-vnpu-shmem/vnpu_local_shmem
```

在 worker 中保存并检查 claim CDI。文件名包含 ResourceClaim UID：

```bash
CLAIM_UID=$(kubectl -n "${TEST_NAMESPACE}" get resourceclaim two-npu -o jsonpath='{.metadata.uid}')
docker exec "${WORKER}" sh -c \
  "grep -R -l '${CLAIM_UID}' /var/run/cdi | xargs -r cat" \
  | tee "${TEST_ROOT}/final-claim-cdi.yaml"

docker exec "${WORKER}" cat \
  /var/lib/kubelet/plugins/npu.project-hami.io/checkpoint.json \
  | tee "${TEST_ROOT}/checkpoint-after-prepare.json"
```

确认五个环境变量各出现一次、七个公共 mount 各出现一次，并且所有 HostPath mount 均包含 `bind` option。设备 cgroup 应允许卡 `235:0`、`235:1` 及公共设备 `234:0`、`236:0`、`511:0`，但 workload 中不应出现未分配的 `/dev/davinci2`。

本轮验证中，Ascend runtime 已成功注入 `/dev/davinci0`、`/dev/davinci1`、`/dev/davinci_manager`、`/dev/devmm_svm` 和 `/dev/hisi_hdc`。当前 CANN 基础镜像中执行 `npu-smi info` 仍可能返回 DCMI `-8005`；同一错误在宿主直接使用 `docker --runtime=ascend` 时也能复现，因此不作为 Kind runtime 接入失败的判断条件，但真实 NPU 计算仍需单独验证。

### 8.1 验证同一物理卡的两个独立 Claim

跨卡验证完成后，可使用只选择 index 0 的 DeviceClass 创建两个独立 Claim。两个 Claim 都请求 `1Gi memory + 50 aicore`，调度结果应落在同一个 `npu-0-0`，但 `shareID` 必须不同。

```bash
kubectl delete -f "${TEST_ROOT}/two-device-e2e.yaml" --ignore-not-found --wait=true

cat > "${TEST_ROOT}/same-device-e2e.yaml" <<'EOF'
apiVersion: v1
kind: Namespace
metadata:
  name: ascend-dra-softsplit-e2e
---
apiVersion: resource.k8s.io/v1
kind: DeviceClass
metadata:
  name: ascend-vnpu-same-device-e2e
spec:
  selectors:
    - cel:
        expression: >-
          device.driver == "npu.project-hami.io" &&
          device.allowMultipleAllocations == true &&
          device.attributes["npu.project-hami.io"].type == "NPU" &&
          device.attributes["npu.project-hami.io"].index == 0
---
apiVersion: resource.k8s.io/v1
kind: ResourceClaim
metadata:
  namespace: ascend-dra-softsplit-e2e
  name: npu-share-a
spec:
  devices:
    requests:
      - name: npu
        exactly:
          deviceClassName: ascend-vnpu-same-device-e2e
          allocationMode: ExactCount
          count: 1
          capacity:
            requests:
              npu.project-hami.io/memory: 1Gi
              npu.project-hami.io/aicore: 50
---
apiVersion: resource.k8s.io/v1
kind: ResourceClaim
metadata:
  namespace: ascend-dra-softsplit-e2e
  name: npu-share-b
spec:
  devices:
    requests:
      - name: npu
        exactly:
          deviceClassName: ascend-vnpu-same-device-e2e
          allocationMode: ExactCount
          count: 1
          capacity:
            requests:
              npu.project-hami.io/memory: 1Gi
              npu.project-hami.io/aicore: 50
EOF

kubectl apply -f "${TEST_ROOT}/same-device-e2e.yaml"

for suffix in a b; do
  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  namespace: ascend-dra-softsplit-e2e
  name: npu-share-${suffix}
spec:
  runtimeClassName: ascend
  restartPolicy: Never
  nodeSelector:
    npu.project-hami.io/e2e-node: "true"
  containers:
    - name: workload
      image: ${DRIVER_IMAGE}
      imagePullPolicy: Never
      command: ["/bin/bash", "-c"]
      args:
        - |
          exec python3 -u -c '
          import ctypes, os, time
          runtime = ctypes.CDLL("libruntime.so")
          runtime.rtSetDevice.argtypes = [ctypes.c_int]
          runtime.rtSetDevice.restype = ctypes.c_ulong
          set_device_ret = runtime.rtSetDevice(0)
          free = ctypes.c_size_t()
          total = ctypes.c_size_t()
          probe = ctypes.CDLL(None).rtMemGetInfoEx
          probe.argtypes = [ctypes.c_ulong, ctypes.POINTER(ctypes.c_size_t), ctypes.POINTER(ctypes.c_size_t)]
          probe.restype = ctypes.c_ulong
          probe_ret = probe(0, ctypes.byref(free), ctypes.byref(total))
          print("set_device_ret=%d probe_ret=%d free=%d total=%d local=%s" %
                (set_device_ret, probe_ret, free.value, total.value,
                 os.environ["NPU_LOCAL_SHM_PATH"]), flush=True)
          while True:
              time.sleep(60)
          '
      resources:
        claims:
          - name: npu
  resourceClaims:
    - name: npu
      resourceClaimName: npu-share-${suffix}
EOF
done

kubectl -n ascend-dra-softsplit-e2e wait \
  --for=jsonpath='{.status.allocation}' resourceclaim/npu-share-a --timeout=5m
kubectl -n ascend-dra-softsplit-e2e wait \
  --for=jsonpath='{.status.allocation}' resourceclaim/npu-share-b --timeout=5m
kubectl -n ascend-dra-softsplit-e2e wait \
  --for=condition=Ready pod/npu-share-a --timeout=5m
kubectl -n ascend-dra-softsplit-e2e wait \
  --for=condition=Ready pod/npu-share-b --timeout=5m
```

保存两个 Claim 后，确认二者的 `device` 都是 `npu-0-0`、`shareID` 不同、consumed capacity 都是 `1Gi/50`：

```bash
kubectl -n ascend-dra-softsplit-e2e get resourceclaim \
  npu-share-a npu-share-b -o json > "${TEST_ROOT}/same-device-claims.json"

python3 - "${TEST_ROOT}/same-device-claims.json" <<'PY'
import json, sys
claims = json.load(open(sys.argv[1], encoding="utf-8"))["items"]
results = [claim["status"]["allocation"]["devices"]["results"][0] for claim in claims]
assert [result["device"] for result in results] == ["npu-0-0", "npu-0-0"]
assert len({result["shareID"] for result in results}) == 2
for result in results:
    assert result["consumedCapacity"]["npu.project-hami.io/memory"] == "1Gi"
    assert result["consumedCapacity"]["npu.project-hami.io/aicore"] == "50"
PY

for pod in npu-share-a npu-share-b; do
  kubectl -n ascend-dra-softsplit-e2e logs "${pod}" |
    grep -F 'set_device_ret=0 probe_ret=0 free=939524096 total=1073741824'
  kubectl -n ascend-dra-softsplit-e2e exec "${pod}" -- bash -c '
    test "$ASCEND_VISIBLE_DEVICES" = 0
    test "$NPU_MEM_QUOTA" = 1024
    test "$NPU_PRIORITY" = 50
    test "$NPU_LOCAL_SHM_PATH" = /hami-vnpu-shmem/vnpu_local_shmem
    test -f /hami-vnpu-shmem/vnpu_local_shmem
    test -c /dev/davinci0
    test ! -e /dev/davinci1
    grep -q /hami-vnpu-core/libvnpu.so /proc/1/maps
  '
done
```

`total=1073741824` 表示两个独立 manager 都观察到各自的 1Gi 配额。还应在 worker 中确认两个 Claim UID 对应不同的宿主目录：

```bash
UID_A=$(kubectl -n ascend-dra-softsplit-e2e get resourceclaim npu-share-a -o jsonpath='{.metadata.uid}')
UID_B=$(kubectl -n ascend-dra-softsplit-e2e get resourceclaim npu-share-b -o jsonpath='{.metadata.uid}')
test "${UID_A}" != "${UID_B}"
docker exec "${WORKER}" test -f \
  "/usr/local/hami-vnpu-core/containers/${UID_A}/vnpu_local_shmem"
docker exec "${WORKER}" test -f \
  "/usr/local/hami-vnpu-core/containers/${UID_B}/vnpu_local_shmem"
```

删除两个 Pod 后，等待对应 CDI spec 和 local shmem 目录消失，再删除剩余测试资源：

```bash
kubectl -n ascend-dra-softsplit-e2e delete pod npu-share-a npu-share-b --wait=true
for i in $(seq 1 60); do
  if ! docker exec "${WORKER}" grep -R -q "${UID_A}" /var/run/cdi &&
     ! docker exec "${WORKER}" grep -R -q "${UID_B}" /var/run/cdi &&
     ! docker exec "${WORKER}" test -e "/usr/local/hami-vnpu-core/containers/${UID_A}" &&
     ! docker exec "${WORKER}" test -e "/usr/local/hami-vnpu-core/containers/${UID_B}"; then
    break
  fi
  sleep 2
done
kubectl delete -f "${TEST_ROOT}/same-device-e2e.yaml" --ignore-not-found
```

## 9. 验证 Unprepare

删除消费 Claim 的 Pod，并等待 claim CDI 被移除：

```bash
kubectl -n "${TEST_NAMESPACE}" delete pod two-npu-workload --wait=true

for i in $(seq 1 60); do
  if ! docker exec "${WORKER}" grep -R -q "${CLAIM_UID}" /var/run/cdi; then
    break
  fi
  sleep 2
done

if docker exec "${WORKER}" grep -R -q "${CLAIM_UID}" /var/run/cdi; then
  echo "claim CDI still exists after Unprepare" >&2
  exit 1
fi

docker exec "${WORKER}" cat \
  /var/lib/kubelet/plugins/npu.project-hami.io/checkpoint.json \
  | tee "${TEST_ROOT}/checkpoint-after-unprepare.json"

kubectl get resourceslices -o yaml \
  | tee "${TEST_ROOT}/final-resourceslices.yaml"
kubectl -n "${DRIVER_NAMESPACE}" logs \
  daemonset/ascend-dra-driver-kubeletplugin -c plugin --tail=500 \
  | tee "${TEST_ROOT}/driver-after-unprepare.log"
```

预期 checkpoint 不再包含该 claim，claim CDI 被删除，公共 CDI 仍保留，ResourceSlice 中两张设备仍正常发布。

## 10. 清理和宿主验收

先保存诊断信息，再删除 Kubernetes 资源和 Kind 集群。测试依赖镜像继续保留，不执行 `docker system prune`。

```bash
kubectl delete -f "${TEST_ROOT}/two-device-e2e.yaml" --ignore-not-found
helm uninstall ascend-dra-driver -n "${DRIVER_NAMESPACE}" || true
kubectl delete namespace "${DRIVER_NAMESPACE}" --ignore-not-found --wait=true
kubectl delete -f "${TEST_ROOT}/ascend-runtimeclass.yaml" --ignore-not-found

KIND_EXPERIMENTAL_PROVIDER=docker kind delete cluster \
  --name "${KIND_CLUSTER_NAME}"

docker ps -a --format '{{.Names}}' | grep "^${KIND_CLUSTER_NAME}-" && exit 1 || true
docker network ls --format '{{.Name}}' | grep "${KIND_CLUSTER_NAME}" && exit 1 || true

docker image inspect "${KIND_NODE_IMAGE}" >/dev/null
docker image inspect "${BASE_CUSTOM_KIND_NODE_IMAGE}" >/dev/null
docker image inspect "${CUSTOM_KIND_NODE_IMAGE}" >/dev/null
docker image inspect "${DRIVER_IMAGE}" >/dev/null
docker image ls --no-trunc --digests \
  --format '{{.Repository}}:{{.Tag}} {{.ID}} {{.Digest}}' \
  | sort > "${TEST_ROOT}/docker-images.after"

comm -23 \
  "${TEST_ROOT}/docker-images.before" \
  "${TEST_ROOT}/docker-images.after" \
  | tee "${TEST_ROOT}/preexisting-images.missing"
test ! -s "${TEST_ROOT}/preexisting-images.missing"
```

最后的 `comm` 检查确认测试前已有镜像和标签没有丢失或被覆盖。新增的基础 Kind、定制 Kind 和驱动镜像属于预期结果，应保留供下一次测试复用。

## 通过标准

以下条件全部满足时，本轮 DRA 生命周期和注入测试通过：

- 两张不同物理设备被 ResourceClaim 分配，每张具有独立 share ID。
- Pod 进入 `Running`，且没有因 CDI mount 失败而重启。
- workload 使用 `RuntimeClass=ascend`，能看到 `/dev/davinci0`、`/dev/davinci1` 和三个公共设备节点，但看不到未分配的 `/dev/davinci2`。
- device cgroup 只允许两张已分配卡及所需公共设备。
- 五个 libvnpu 环境变量正确且各出现一次。
- libvnpu、preload、全局/Claim 级共享目录和 Ascend driver mount 有效。
- `/proc/1/maps` 能看到 `libvnpu.so`。
- 删除 Pod 后 claim CDI 和 checkpoint prepared claim 被清理。
- 驱动继续运行并维持 ResourceSlice 发布。
- Kind node 容器和测试网络被删除，Docker Daemon 未修改或重启。
- 测试前已有 Docker 镜像没有被删除，测试依赖镜像仍保留。

设备节点和 cgroup 注入已经验证通过，但 `npu-smi` 的 DCMI `-8005` 仍需结合更匹配的 CANN 运行时镜像继续排查。只有成功运行真实 NPU workload 后，才能进一步认定硬件执行路径和 libvnpu 配额限制也通过。
