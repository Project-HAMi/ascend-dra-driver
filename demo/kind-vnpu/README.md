# HAMivNPUCore Kind Demo

本目录把仓库根目录 `test_dra.md` 中的手工 E2E 流程封装为可重复执行的 demo。目录结构参考 HAMi `k8s-dra-driver` 的 Kind demo：入口脚本负责阶段编排，`scripts/` 保存具体操作，`config/` 和 `templates/` 保存可审查的容器与 Kubernetes 配置。

该 demo 验证单设备与多设备 ResourceClaim，以及共享物理 Ascend NPU 的软切分：

- `npu-share-a` 分配一个设备。
- `npu-share-b` 分配两个不同设备，并与 A 共享其中一个设备。
- B 通过 `DeviceConstraint.distinctAttribute=npu.project-hami.io/index` 明确要求
  两个分配结果来自不同设备 index。
- 每个设备分配结果消费 `1Gi memory + 50 aicore`，并具有独立 `shareID`。
- Ascend Runtime 分别向两个 Pod 注入一个和两个 NPU 设备节点及必要公共设备。
- 两个容器分别获得 Claim 级 local shmem。
- `libvnpu.so` 实际拦截内存查询，并为两个容器分别返回 1Gi 总量。
- 删除 Pod 和 ResourceClaim 后，kubelet 执行 ResourceUnprepare 清理。

## 安全边界

- 不修改或重启宿主 Docker Daemon。
- 不执行 `docker system prune`、`docker image prune` 或批量删除镜像。
- 清理只删除固定命名的 Kind 集群和 Kubernetes 测试资源。
- 清理后保留 Kind node、CANN、Go builder 和驱动镜像，供后续复测。
- 测试开始时记录已有镜像；清理阶段确认这些镜像没有丢失。
- 测试开始时记录 device-share 状态；清理阶段恢复原状态。
- 任一步骤失败都会保留 Kind 集群，便于检查，不会自动销毁证据。
- 清理要求状态文件和 Kind node 容器 ID 所有权记录，不会仅凭同名集群执行删除。

## 前置条件

在 ARM64 Ascend 节点上执行。需要：

- Docker、kubectl、Helm、Go、Python 3 和 curl。
- 至少两个名称符合 `/dev/davinci<数字>` 的 NPU 字符设备节点，以及三个公共
  Ascend 设备节点。节点编号可以不连续；demo 会自动发现并挂载实际节点。
- Ascend Driver、DCMI、`npu-smi` 和 Ascend Docker Runtime。
- 本地 Docker 已有 `ascendai/cann:9.0.0-devel` 和 `golang:1.26.0`。
- 仓库的两个 submodule 可初始化。

demo 会根据 Docker 报告的 cgroup 版本自动选择 Kind 节点配置：cgroup v2
直接使用 `KIND_NODE_IMAGE` 及 Kind 的默认 systemd cgroup 配置；cgroup v1
使用 `Dockerfile.ascend` 从 `KIND_NODE_IMAGE` 直接构建包含 cgroupfs、CDI 和
Ascend runtime 的兼容镜像。两条路径都会开启 CDI 并注册隔离的 `ascend`
runtime。

demo 期间应独占 `DEVICE_SHARE_CARD_IDS` 指定的卡，避免其它任务并发修改 device-share。当前已验证的单芯片 card 使用 chip ID `0`；若目标机器不同，可设置 `DEVICE_SHARE_CHIP_ID`。

`DEVICE_SHARE_CARD_IDS` 使用 `npu-smi info` 中的物理 NPU ID；它不需要与
`/dev/davinciN` 的后缀相同。demo 会把宿主上所有实际存在的
`/dev/davinci<数字>` 字符设备动态写入 Kind `extraMounts`，workload 验证也只
按 Claim 的实际分配数量检查 Ascend Runtime 注入的设备，不假设具体设备节点编号。

若下载依赖需要代理，仅在执行 demo 前为当前 shell 配置相应的 `HTTP_PROXY`、`HTTPS_PROXY` 或 `GOPROXY`。不要修改 Docker Daemon 的代理。

## 快速演示

先编辑 [`demo.env`](demo.env) 设置当前环境的参数，至少填写
`DEVICE_SHARE_CARD_IDS`。调用命令时显式导出的同名环境变量优先级高于该文件，
适合做一次性覆盖。

先从 `npu-smi info` 确认物理 card ID。它不一定与 `/dev/davinciN` 的编号相同。

```bash
cd /path/to/hami-dra-driver
npu-smi info

export DEVICE_SHARE_CARD_IDS="CARD_ID_A CARD_ID_B"  # replace both placeholders
./demo/kind-vnpu/demo.sh all
```

镜像是否复用由 `demo.env` 中的 `SKIP_EXISTING_IMAGE_BUILDS` 控制，也可以通过
同名环境变量进行一次性覆盖：

```bash
SKIP_EXISTING_IMAGE_BUILDS=true ./demo/kind-vnpu/demo.sh setup
```

复用驱动镜像时，应通过 `DRIVER_IMAGE_REPOSITORY` 和 `DRIVER_IMAGE_TAG` 指向
已有的明确标签。设置 `SKIP_EXISTING_IMAGE_BUILDS=false` 会重新构建驱动镜像。

`all` 用于无人值守验证，会依次执行 setup、prepare、verify、unprepare 和
cleanup，完成后不会保留 Kind 集群。需要人工观察时应分阶段执行：

```bash
./demo/kind-vnpu/demo.sh setup
./demo/kind-vnpu/demo.sh prepare
./demo/kind-vnpu/demo.sh verify
./demo/kind-vnpu/demo.sh status

export KUBECONFIG=/tmp/hami-dra-kind-vnpu-demo/kubeconfig
kubectl -n ascend-dra-vnpu-demo get resourceclaims,pods -o wide
kubectl -n ascend-dra-vnpu-demo logs npu-share-a
kubectl -n ascend-dra-vnpu-demo logs npu-share-b
```

删除 ResourceClaim 和 Pod，然后清理完整 Kind 环境：

```bash
./demo/kind-vnpu/demo.sh unprepare
./demo/kind-vnpu/demo.sh cleanup
```

## 分阶段执行

以下入口便于人工控制演示节奏：

```bash
# 预检、构建镜像、创建集群、部署驱动、导入 workload 镜像并创建基础资源
./demo/kind-vnpu/setup.sh

# 只创建两个 ResourceClaim 和两个 Pod
./demo/kind-vnpu/prepare.sh

# 查看现场状态
./demo/kind-vnpu/demo.sh status

# 执行 workload、DRA、CDI 和配额断言
./demo/kind-vnpu/demo.sh verify

# 删除两个 ResourceClaim 和两个 Pod
./demo/kind-vnpu/demo.sh unprepare

# 删除固定命名的测试资源和 Kind 集群
./demo/kind-vnpu/cleanup.sh
```

所有生成的 kubeconfig、YAML、日志、CDI 和 checkpoint 证据默认保存在：

```text
/tmp/hami-dra-kind-vnpu-demo
```

用户配置集中在仓库内的 `demo/kind-vnpu/demo.env`。脚本会把设备发现、
Kind 实际路径和 Claim UID 等运行期状态写入 `DEMO_STATE_DIR/runtime.env`，因此
各阶段不需要在同一个 shell 中运行。不要手工编辑 `runtime.env`。

## 常用覆盖变量

以下配置均可直接写入 `demo/kind-vnpu/demo.env`；`export` 形式仅用于临时覆盖：

```bash
# 远端物理 card ID；非交互执行时必须设置
export DEVICE_SHARE_CARD_IDS="CARD_ID_A CARD_ID_B"  # replace both placeholders

# 改用独立状态目录和集群名
export DEMO_STATE_DIR=/tmp/my-hami-demo
export KIND_CLUSTER_NAME=my-hami-demo

# 复用已构建的明确驱动 tag
export DRIVER_IMAGE_TAG=e2e-vnpu-softsplit
export SKIP_EXISTING_IMAGE_BUILDS=true

# 跳过 Go 测试门禁，仅适用于已完成同一 commit 验证的演示
export RUN_GO_TESTS=false

# 覆盖本地已有的构建镜像
export CANN_IMAGE=ascendai/cann:9.0.0-devel
export GOLANG_VERSION=1.26.0

# NPU 测试 Pod 使用的运行时镜像，默认与 CANN_IMAGE 相同
export WORKLOAD_IMAGE=ascendai/cann:9.0.0-devel

# 目标机器的 device-share chip ID 不是 0 时覆盖
export DEVICE_SHARE_CHIP_ID=0

# 默认 auto：Docker cgroup v1 使用 cgroupfs，v2 使用原生 systemd 配置
# 仅在自动探测与实际环境不符时显式覆盖为 cgroupfs 或 systemd
export KIND_CGROUP_MODE=auto
```

同一个 `DEMO_STATE_DIR` 会保留首次运行的变量。若要以一组全新参数重新开始，请在已完成 `cleanup` 后使用新的状态目录；不要直接删除仍有关联集群的状态目录。

## 阶段说明

`setup` 顺序执行：

1. 记录宿主 Docker 镜像、daemon 配置和 device-share 基线。
2. 检查 Ascend runtime、DCMI、NPU 设备节点和 card ID。
3. 初始化 submodule，并默认执行 Go 测试与 `make build`。
4. 按 Docker cgroup 版本选择节点配置；仅 cgroup v1 构建 cgroupfs 兼容镜像。
5. 开启 CDI，并为 NPU worker 注册隔离的 `ascend` runtime handler。
6. 构建当前源码对应的驱动镜像。
7. 创建 control-plane + NPU worker Kind 集群。
8. 仅向 worker 导入驱动镜像并部署 DRA 插件。
9. 验证 ResourceSlice 与物理卡 device-share 状态。
10. 验证并向 worker 导入 `WORKLOAD_IMAGE`。
11. 创建测试 Namespace 和 `ascend-vnpu-same-device-e2e` DeviceClass。

`setup` 到此结束，不创建 ResourceClaim 或 workload Pod。

`prepare` 只执行：

1. 创建申请一个设备的 `npu-share-a` ResourceClaim。
2. 创建申请两个设备的 `npu-share-b` ResourceClaim，并通过 `DeviceConstraint`
   要求 `npu.project-hami.io/index` 两两不同。
3. 创建两个使用 `RuntimeClass=ascend` 的 workload Pod。

`verify` 等待 Claim 分配和 Pod Ready，然后断言 A 分配一个设备、B 分配两个
设备，每个设备分配结果具有不同 shareID；同时检查环境、设备、CDI、preload、
local shmem、checkpoint 和 1Gi 内存配额。如果 ResourceClaim 或 Pod 不存在，
它会列出缺失资源并提示先执行 `demo.sh prepare`。

`unprepare` 只删除两个 Pod 和两个 ResourceClaim，保留 setup 创建的 Namespace、
DeviceClass、DRA driver 和 Kind 集群，以便再次执行 `prepare`。

`cleanup` 先核验 Kind node 容器 ID 的所有权，再删除本 demo 的 workload、Helm release、RuntimeClass 和 Kind 集群；随后恢复测试前的 device-share 状态，保留所有测试依赖镜像，并检查宿主原有镜像与 Docker runtime/cgroup 配置未被改变。

`all` 自动执行 `setup → prepare → verify → unprepare → cleanup` 完整闭环。

## 故障诊断

失败后先保留集群：

```bash
./demo/kind-vnpu/demo.sh status

export KUBECONFIG=/tmp/hami-dra-kind-vnpu-demo/kubeconfig
kubectl get pods -A -o wide
kubectl get resourceslices -o yaml
kubectl -n ascend-dra-demo logs \
  daemonset/ascend-dra-driver-kubeletplugin \
  -c plugin --tail=1000
docker logs hami-dra-vnpu-demo-worker --tail=500
```

当前 CANN 基础镜像执行 `npu-smi info` 可能返回 DCMI `-8005`，相同问题在宿主直接使用 `docker --runtime=ascend` 时也可能复现，因此不作为 demo 失败门禁。demo 验证的是设备注入、DRA 生命周期和内存配额拦截，不代表真实 AI workload 的吞吐隔离或持续 aicore 限流已经通过。
