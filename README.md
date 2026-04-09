# 昇腾DRA驱动

本仓库包含用于Kubernetes [动态资源分配(DRA)](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)功能的示例资源驱动。

本项目旨在展示如何构建DRA资源驱动并将其封装在[helm chart](https://helm.sh/)中的最佳实践。它可以作为实现您自己资源集驱动的起点。

## 里程碑
- [x] 支持NPU整卡分配
- [x] 支持vNPU动态分配（vNPU分配后重新计算剩余设备，并更新device列表）
- [ ] 整理代码文件中的K8s定义，GPU->NPU
- [ ] 实现多节点多卡调度分配
- [ ] 实现基本故障处理
- [ ] 实现基本运行时动态分配（可行性分析）
- [ ] 实现全面故障处理

## 快速开始和演示

在深入了解该示例驱动程序构建细节之前，通过快速演示了解其运行情况是很有用的。

驱动本身提供对一组NPU设备的访问，本演示将介绍构建和安装驱动程序，然后运行消耗这些NPU的工作负载的过程。

以下步骤已在Linux上测试并验证。

### 前置条件

* [GNU Make 3.81+](https://www.gnu.org/software/make/)
* [GNU Tar 1.34+](https://www.gnu.org/software/tar/)
* [docker v20.10+ (包括buildx)](https://docs.docker.com/engine/install/) 或 [Podman v4.9+](https://podman.io/docs/installation)
* [minikube v1.32.0+](https://minikube.sigs.k8s.io/docs/start/)
* [helm v3.7.0+](https://helm.sh/docs/intro/install/)
* [kubectl v1.18+](https://kubernetes.io/docs/reference/kubectl/)
* 其他二进制依赖 参考： [.gitkeep](dev/tools/.gitkeep)
  - 注意环境是arm还是amd

### 基础环境搭建

首先克隆此仓库并进入目录。此演示中使用的所有脚本和示例Pod规范都包含在这里：
```
git clone https://github.com/kubernetes-sigs/ascend-dra-driver.git
cd ascend-dra-driver
```

1. 创建minikube单机集群：
```bash
./demo/create-cluster.sh
```

集群创建成功后，仔细检查一切是否按预期启动：
```console
$ kubectl get pod -A
NAMESPACE            NAME                                                              READY   STATUS    RESTARTS   AGE
kube-system          coredns-5d78c9869d-6jrx9                                          1/1     Running   0          1m
kube-system          coredns-5d78c9869d-dpr8p                                          1/1     Running   0          1m
kube-system          etcd-ascend-dra-driver-cluster-control-plane                      1/1     Running   0          1m
kube-system          kube-apiserver-ascend-dra-driver-cluster-control-plane            1/1     Running   0          1m
kube-system          kube-controller-manager-ascend-dra-driver-cluster-control-plane   1/1     Running   0          1m
kube-system          kube-proxy-kgz4z                                                  1/1     Running   0          1m
kube-system          kube-proxy-x6fnd                                                  1/1     Running   0          1m
kube-system          kube-scheduler-ascend-dra-driver-cluster-control-plane            1/1     Running   0          1m
local-path-storage   local-path-provisioner-7dbf974f64-9jmc7                           1/1     Running   0          1m
```

2. 安装NPU引擎插件

需要在集群内安装 [ascend-docker-runtime](https://gitee.com/ascend/mind-cluster/tree/branch_v6.0.0/component/ascend-docker-runtime) 引擎插件，提供对 Ascend NPU 的容器化支持。
先进入集群：
```bash
minikube ssh 
```
按照链接教程生成二进制run包后，执行安装命令：
```bash
./Ascend-docker-runtime_6.0.0.SPC1_linux-aarch64.run --install --install-scene=containerd
```



3. 编译和安装DRA驱动程序：
```bash
# 构建驱动镜像
./demo/build-driver.sh

# 安装驱动到集群
./demo/install-dra-driver.sh
```

检查驱动程序组件是否已成功启动：
```console
$ kubectl get pod -n ascend-dra-driver
NAME                                             READY   STATUS    RESTARTS   AGE
ascend-dra-driver-kubeletplugin-qwmbl           1/1     Running   0          1m
```

并显示工作节点上可用NPU设备的初始状态：
```
$ kubectl get resourceslice -o yaml
```

### 功能测试

接下来，部署五个示例应用程序，演示如何以各种方式使用`ResourceClaim`、`ResourceClaimTemplate`和自定义`NpuConfig`对象来选择和配置资源：
```bash
kubectl apply --filename=demo/npu-test{1,2,3,4,5}.yaml
```
**注意**：您需要使用华为NPU环境做上述测试

并验证它们是否成功启动：
```console
$ kubectl get pod -A
NAMESPACE   NAME   READY   STATUS              RESTARTS   AGE
...
npu-test1   pod0   0/1     Pending             0          2s
npu-test1   pod1   0/1     Pending             0          2s
npu-test2   pod0   0/2     Pending             0          2s
npu-test3   pod0   0/1     ContainerCreating   0          2s
npu-test3   pod1   0/1     ContainerCreating   0          2s
npu-test4   pod0   0/1     Pending             0          2s
npu-test5   pod0   0/4     Pending             0          2s
...
```

使用您喜欢的编辑器查看每个`npu-test{1,2,3,4,5}.yaml`文件，了解它们的功能。

在这个示例资源驱动程序中，在每个容器中设置了一组环境变量，以指示真实资源驱动程序*会*注入哪些NPU以及它们*会*如何配置。

您可以使用这些环境变量中设置的NPU ID以及NPU共享设置来验证它们是否以与图中所示语义一致的方式分发。

验证一切正常运行后，删除所有示例应用程序：
```bash
kubectl delete --wait=false --filename=demo/npu-test{1,2,3,4,5}.yaml
```

### 开发和调试环境

如果您需要进行开发和调试，可以按照以下步骤设置环境：

1. 编译并启动开发版dra驱动
```bash
# 编译dra驱动
cd ./dev/dra
./build_dra.sh

# 同步开发编译版dra驱动及调试工具到dra驱动容器
./all_cp.sh

# 进入dra驱动容器
./pod_into_dra.sh

# 进入/root目录
cd

# 启动调试
./start_debug.sh

# 在本地开发环境使用远程调试配置连接
# zjknps.jieshi.space:9341
```

2. （可选）替换k8s组件，以调度器为案例。 参考： [K8s远程调试，你的姿势对了吗？](https://cloud.tencent.com/developer/article/1624638)
```bash
# 复制调试工具及可调试版本二进制
cd ./dev/node
./all_cp.sh

# 进入主node节点
./pod_into_node.sh

# 进入/root路径
cd 

# 禁用默认调度器实例
./disable_schedule.sh

# 杀掉调度器实例
./kill_process.sh

# 启动调试版本调度器
./start_debug.sh

# 使用远程调试配置连接
zjknps.jieshi.space:9523
```

### 清理环境

完成测试后，您可以运行以下命令清理环境并删除minikube集群：
```bash
./demo/delete-cluster.sh
```

## 参考资料

有关Kubernetes DRA功能和开发自定义资源驱动程序的更多信息，请参阅以下资源：

* [Kubernetes中的动态资源分配](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)

## 社区、讨论、贡献和支持
待定
