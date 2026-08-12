# HAMivNPUCore Kind Demo

本目录把仓库根目录 `test_dra.md` 中的手工 E2E 流程封装为可重复执行的 demo。目录结构参考 HAMi `k8s-dra-driver` 的 Kind demo：入口脚本负责阶段编排，`scripts/` 保存具体操作，`config/` 和 `templates/` 保存可审查的容器与 Kubernetes 配置。

该 demo 验证两个独立 ResourceClaim 在同一张物理 Ascend NPU 上进行软切分：

- 两个 Claim 都分配到 `npu-0-0`，但具有不同 `shareID`。
- 每个 Claim 消费 `1Gi memory + 50 aicore`。
- Ascend Runtime 仅注入 `/dev/davinci0` 和必要公共设备。
- 两个容器分别获得 Claim 级 local shmem。
- `libvnpu.so` 实际拦截内存查询，并为两个容器分别返回 1Gi 总量。
- 删除 Pod 后，Claim CDI、checkpoint 和 local shmem 被清理。

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
- 至少 `/dev/davinci0`、`/dev/davinci1` 及三个公共 Ascend 设备节点。
- Ascend Driver、DCMI、`npu-smi` 和 Ascend Docker Runtime。
- 本地 Docker 已有 `ascendai/cann:9.0.0-devel` 和 `golang:1.26.0`。
- 仓库的两个 submodule 可初始化。

demo 期间应独占 `DEVICE_SHARE_CARD_IDS` 指定的卡，避免其它任务并发修改 device-share。当前已验证的单芯片 card 使用 chip ID `0`；若目标机器不同，可设置 `DEVICE_SHARE_CHIP_ID`。

若下载依赖需要代理，仅在执行 demo 前为当前 shell 配置相应的 `HTTP_PROXY`、`HTTPS_PROXY` 或 `GOPROXY`。不要修改 Docker Daemon 的代理。

## 快速演示

先从 `npu-smi info` 确认物理 card ID。它不一定与 `/dev/davinciN` 的编号相同。

```bash
cd /path/to/hami-dra-driver
npu-smi info

export DEVICE_SHARE_CARD_IDS="CARD_ID_A CARD_ID_B"  # replace both placeholders
./demo/kind-vnpu/demo.sh all
```

`all` 完成后保留两个 Running workload，适合现场展示：

```bash
./demo/kind-vnpu/demo.sh status

export KUBECONFIG=/tmp/hami-dra-kind-vnpu-demo/kubeconfig
kubectl -n ascend-dra-vnpu-demo get resourceclaims,pods -o wide
kubectl -n ascend-dra-vnpu-demo logs npu-share-a
kubectl -n ascend-dra-vnpu-demo logs npu-share-b
```

演示 ResourceUnprepare 并清理：

```bash
./demo/kind-vnpu/demo.sh unprepare
./demo/kind-vnpu/demo.sh cleanup
```

也可以一次完成：

```bash
./demo/kind-vnpu/demo.sh finish
```

## 分阶段执行

以下入口便于人工控制演示节奏：

```bash
# 预检、测试、构建镜像、创建集群并部署驱动
./demo/kind-vnpu/setup.sh

# 创建两个 Claim/Pod，并执行全部 Prepare 与配额断言
./demo/kind-vnpu/run-demo.sh

# 查看现场状态
./demo/kind-vnpu/demo.sh status

# 重新执行只读的 workload 断言
./demo/kind-vnpu/demo.sh verify

# 单独演示 Unprepare
./demo/kind-vnpu/demo.sh unprepare

# 删除固定命名的测试资源和 Kind 集群
./demo/kind-vnpu/cleanup.sh
```

所有生成的 kubeconfig、YAML、日志、CDI 和 checkpoint 证据默认保存在：

```text
/tmp/hami-dra-kind-vnpu-demo
```

脚本会把本轮镜像 tag、Kind 路径和 Claim UID 写入该目录的 `demo-env.sh`，因此各阶段不需要在同一个 shell 中运行。

## 常用覆盖变量

```bash
# 远端物理 card ID；非交互执行时必须设置
export DEVICE_SHARE_CARD_IDS="CARD_ID_A CARD_ID_B"  # replace both placeholders

# 改用独立状态目录和集群名
export DEMO_STATE_DIR=/tmp/my-hami-demo
export KIND_CLUSTER_NAME=my-hami-demo

# 复用已构建的明确驱动 tag
export DRIVER_IMAGE_TAG=e2e-vnpu-softsplit
export REUSE_DRIVER_IMAGE=true

# 跳过 Go 测试门禁，仅适用于已完成同一 commit 验证的演示
export RUN_GO_TESTS=false

# 覆盖本地已有的构建镜像
export CANN_IMAGE=ascendai/cann:9.0.0-devel
export GOLANG_VERSION=1.26.0

# 目标机器的 device-share chip ID 不是 0 时覆盖
export DEVICE_SHARE_CHIP_ID=0
```

同一个 `DEMO_STATE_DIR` 会保留首次运行的变量。若要以一组全新参数重新开始，请在已完成 `cleanup` 后使用新的状态目录；不要直接删除仍有关联集群的状态目录。

## 阶段说明

`setup` 顺序执行：

1. 记录宿主 Docker 镜像、daemon 配置和 device-share 基线。
2. 检查 Ascend runtime、DCMI、NPU 设备节点和 card ID。
3. 初始化 submodule，并默认执行 Go 测试与 `make build`。
4. 构建带 `cgroupfs + CDI` 的 Kind node 镜像。
5. 在派生 node 镜像中注册隔离的 `ascend` runtime handler。
6. 构建当前源码对应的驱动镜像。
7. 创建 control-plane + NPU worker Kind 集群。
8. 仅向 worker 导入驱动镜像并部署 DRA 插件。
9. 验证 ResourceSlice 与物理卡 device-share 状态。

`run` 顺序执行：

1. 创建固定选择 `index == 0` 的 DeviceClass。
2. 创建两个独立的 1Gi/50-aicore Claim。
3. 创建两个使用 `RuntimeClass=ascend` 的 workload。
4. 断言两个 Claim 使用同一设备、不同 shareID。
5. 断言环境、设备、CDI、preload、local shmem 和 checkpoint。
6. 断言两个运行时探针都观察到 1Gi 内存总量。

`unprepare` 删除两个 Pod，但保留 Claim，随后验证 Claim CDI、checkpoint 项和 Claim local shmem 已消失。

`cleanup` 先核验 Kind node 容器 ID 的所有权，再删除本 demo 的 workload、Helm release、RuntimeClass 和 Kind 集群；随后恢复测试前的 device-share 状态，保留所有测试依赖镜像，并检查宿主原有镜像与 Docker runtime/cgroup 配置未被改变。

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
