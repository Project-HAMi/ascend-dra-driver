# Ascend DRA demos

本目录包含两组演示：

- [`kind-vnpu/`](kind-vnpu/)：当前推荐的 HAMivNPUCore Kind E2E demo。它覆盖同一物理 NPU 上的双 Claim 软切分、Ascend Runtime 设备注入、libvnpu 配额探针、Prepare/Unprepare 和安全清理。
- 根目录下的 `create-cluster.sh`、`npu-vnpu-test*.yaml`：早期 Minikube 与传统 vNPU 模板示例，仍保留用于兼容已有流程。

运行当前 Kind demo：

```bash
npu-smi info
./demo/kind-vnpu/demo.sh all
```

交互式运行会提示输入两张测试卡的物理 card ID；非交互运行时应提前设置 `DEVICE_SHARE_CARD_IDS`。

完整前置条件、分阶段入口和故障诊断见 [`kind-vnpu/README.md`](kind-vnpu/README.md)。
