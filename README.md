<<<<<<< HEAD
# hami-dra-driver
||||||| parent of 98d1176 (init)
# Example Resource Driver for Dynamic Resource Allocation (DRA)

This repository contains an example resource driver for use with the [Dynamic
Resource Allocation
(DRA)](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
feature of Kubernetes.

It is intended to demonstrate best-practices for how to construct a DRA
resource driver and wrap it in a [helm chart](https://helm.sh/). It can be used
as a starting point for implementing a driver for your own set of resources.

## Quickstart and Demo

Before diving into the details of how this example driver is constructed, it's
useful to run through a quick demo of it in action.

The driver itself provides access to a set of mock GPU devices, and this demo
walks through the process of building and installing the driver followed by
running a set of workloads that consume these GPUs.

The procedure below has been tested and verified on both Linux and Mac.

### Prerequisites

* [GNU Make 3.81+](https://www.gnu.org/software/make/)
* [GNU Tar 1.34+](https://www.gnu.org/software/tar/)
* [docker v20.10+ (including buildx)](https://docs.docker.com/engine/install/) or [Podman v4.9+](https://podman.io/docs/installation)
* [kind v0.17.0+](https://kind.sigs.k8s.io/docs/user/quick-start/)
* [helm v3.7.0+](https://helm.sh/docs/intro/install/)
* [kubectl v1.18+](https://kubernetes.io/docs/reference/kubectl/)

### Demo
We start by first cloning this repository and `cd`ing into it. All of the
scripts and example Pod specs used in this demo are contained here, so take a
moment to browse through the various files and see what's available:
```
git clone https://github.com/kubernetes-sigs/dra-example-driver.git
cd dra-example-driver
```

**Note**: The scripts will automatically use either `docker`, or `podman` as the container tool command, whichever
can be found in the PATH. To override this behavior, set `CONTAINER_TOOL` environment variable either by calling
`export CONTAINER_TOOL=docker`, or by prepending `CONTAINER_TOOL=docker` to a script
(e.g. `CONTAINER_TOOL=docker ./path/to/script.sh`). Keep in mind that building Kind images currently requires Docker.

From here we will build the image for the example resource driver:
```bash
./demo/build-driver.sh
```

And create a `kind` cluster to run it in:
```bash
./demo/create-cluster.sh
```

Once the cluster has been created successfully, double check everything is
coming up as expected:
```console
$ kubectl get pod -A
NAMESPACE            NAME                                                               READY   STATUS    RESTARTS   AGE
kube-system          coredns-5d78c9869d-6jrx9                                           1/1     Running   0          1m
kube-system          coredns-5d78c9869d-dpr8p                                           1/1     Running   0          1m
kube-system          etcd-dra-example-driver-cluster-control-plane                      1/1     Running   0          1m
kube-system          kindnet-g88bv                                                      1/1     Running   0          1m
kube-system          kindnet-msp95                                                      1/1     Running   0          1m
kube-system          kube-apiserver-dra-example-driver-cluster-control-plane            1/1     Running   0          1m
kube-system          kube-controller-manager-dra-example-driver-cluster-control-plane   1/1     Running   0          1m
kube-system          kube-proxy-kgz4z                                                   1/1     Running   0          1m
kube-system          kube-proxy-x6fnd                                                   1/1     Running   0          1m
kube-system          kube-scheduler-dra-example-driver-cluster-control-plane            1/1     Running   0          1m
local-path-storage   local-path-provisioner-7dbf974f64-9jmc7                            1/1     Running   0          1m
```

And then install the example resource driver via `helm`.
```bash
helm upgrade -i \
  --create-namespace \
  --namespace dra-example-driver \
  dra-example-driver \
  deployments/helm/dra-example-driver
```

Double check the driver components have come up successfully:
```console
$ kubectl get pod -n dra-example-driver
NAME                                             READY   STATUS    RESTARTS   AGE
dra-example-driver-kubeletplugin-qwmbl           1/1     Running   0          1m
```

And show the initial state of available GPU devices on the worker node:
```
$ kubectl get resourceslice -o yaml
apiVersion: v1
items:
- apiVersion: resource.k8s.io/v1beta1
  kind: ResourceSlice
  metadata:
    creationTimestamp: "2024-12-09T16:17:09Z"
    generateName: dra-example-driver-cluster-worker-gpu.example.com-
    generation: 1
    name: dra-example-driver-cluster-worker-gpu.example.com-rf2f7
    ownerReferences:
    - apiVersion: v1
      controller: true
      kind: Node
      name: dra-example-driver-cluster-worker
      uid: 6633c2e1-d947-40c3-ba1f-78f3c9aad05c
    resourceVersion: "530"
    uid: d13fd8bd-0a71-43e1-ba79-ebd2fae4847a
  spec:
    driver: gpu.example.com
    nodeName: dra-example-driver-cluster-worker
    pool:
      generation: 0
      name: dra-example-driver-cluster-worker
      resourceSliceCount: 1
    devices:
    - basic:
        attributes:
          driverVersion:
            version: 1.0.0
          index:
            int: 0
          model:
            string: LATEST-GPU-MODEL
          uuid:
            string: gpu-18db0e85-99e9-c746-8531-ffeb86328b39
        capacity:
          memory:
            value: 80Gi
      name: gpu-0
    - basic:
        attributes:
          driverVersion:
            version: 1.0.0
          index:
            int: 1
          model:
            string: LATEST-GPU-MODEL
          uuid:
            string: gpu-93d37703-997c-c46f-a531-755e3e0dc2ac
        capacity:
          memory:
            value: 80Gi
      name: gpu-1
    - basic:
        attributes:
          driverVersion:
            version: 1.0.0
          index:
            int: 2
          model:
            string: LATEST-GPU-MODEL
          uuid:
            string: gpu-ee3e4b55-fcda-44b8-0605-64b7a9967744
        capacity:
          memory:
            value: 80Gi
      name: gpu-2
    - basic:
        attributes:
          driverVersion:
            version: 1.0.0
          index:
            int: 3
          model:
            string: LATEST-GPU-MODEL
          uuid:
            string: gpu-9ede7e32-5825-a11b-fa3d-bab6d47e0243
        capacity:
          memory:
            value: 80Gi
      name: gpu-3
    - basic:
        attributes:
          driverVersion:
            version: 1.0.0
          index:
            int: 4
          model:
            string: LATEST-GPU-MODEL
          uuid:
            string: gpu-e7b42cb1-4fd8-91b2-bc77-352a0c1f5747
        capacity:
          memory:
            value: 80Gi
      name: gpu-4
    - basic:
        attributes:
          driverVersion:
            version: 1.0.0
          index:
            int: 5
          model:
            string: LATEST-GPU-MODEL
          uuid:
            string: gpu-f11773a1-5bfb-e48b-3d98-1beb5baaf08e
        capacity:
          memory:
            value: 80Gi
      name: gpu-5
    - basic:
        attributes:
          driverVersion:
            version: 1.0.0
          index:
            int: 6
          model:
            string: LATEST-GPU-MODEL
          uuid:
            string: gpu-0159f35e-99ee-b2b5-74f1-9d18df3f22ac
        capacity:
          memory:
            value: 80Gi
      name: gpu-6
    - basic:
        attributes:
          driverVersion:
            version: 1.0.0
          index:
            int: 7
          model:
            string: LATEST-GPU-MODEL
          uuid:
            string: gpu-657bd2e7-f5c2-a7f2-fbaa-0d1cdc32f81b
        capacity:
          memory:
            value: 80Gi
      name: gpu-7
kind: List
metadata:
  resourceVersion: ""
```

Next, deploy four example apps that demonstrate how `ResourceClaim`s,
`ResourceClaimTemplate`s, and custom `GpuConfig` objects can be used to
select and configure resources in various ways:
```bash
kubectl apply --filename=demo/gpu-test{1,2,3,4,5}.yaml
```

And verify that they are coming up successfully:
```console
$ kubectl get pod -A
NAMESPACE   NAME   READY   STATUS              RESTARTS   AGE
...
gpu-test1   pod0   0/1     Pending             0          2s
gpu-test1   pod1   0/1     Pending             0          2s
gpu-test2   pod0   0/2     Pending             0          2s
gpu-test3   pod0   0/1     ContainerCreating   0          2s
gpu-test3   pod1   0/1     ContainerCreating   0          2s
gpu-test4   pod0   0/1     Pending             0          2s
gpu-test5   pod0   0/4     Pending             0          2s
...
```

Use your favorite editor to look through each of the `gpu-test{1,2,3,4,5}.yaml`
files and see what they are doing. The semantics of each match the figure
below:

![Demo Apps Figure](demo/demo-apps.png?raw=true "Semantics of the applications requesting resources from the example DRA resource driver.")

Then dump the logs of each app to verify that GPUs were allocated to them
according to these semantics:
```bash
for example in $(seq 1 5); do \
  echo "gpu-test${example}:"
  for pod in $(kubectl get pod -n gpu-test${example} --output=jsonpath='{.items[*].metadata.name}'); do \
    for ctr in $(kubectl get pod -n gpu-test${example} ${pod} -o jsonpath='{.spec.containers[*].name}'); do \
      echo "${pod} ${ctr}:"
      if [ "${example}" -lt 3 ]; then
        kubectl logs -n gpu-test${example} ${pod} -c ${ctr}| grep -E "GPU_DEVICE_[0-9]+=" | grep -v "RESOURCE_CLAIM"
      else
        kubectl logs -n gpu-test${example} ${pod} -c ${ctr}| grep -E "GPU_DEVICE_[0-9]+" | grep -v "RESOURCE_CLAIM"
      fi
    done
  done
  echo ""
done
```

This should produce output similar to the following:
```bash
gpu-test1:
pod0 ctr0:
declare -x GPU_DEVICE_6="gpu-6"
pod1 ctr0:
declare -x GPU_DEVICE_7="gpu-7"

gpu-test2:
pod0 ctr0:
declare -x GPU_DEVICE_0="gpu-0"
declare -x GPU_DEVICE_1="gpu-1"

gpu-test3:
pod0 ctr0:
declare -x GPU_DEVICE_2="gpu-2"
declare -x GPU_DEVICE_2_SHARING_STRATEGY="TimeSlicing"
declare -x GPU_DEVICE_2_TIMESLICE_INTERVAL="Default"
pod0 ctr1:
declare -x GPU_DEVICE_2="gpu-2"
declare -x GPU_DEVICE_2_SHARING_STRATEGY="TimeSlicing"
declare -x GPU_DEVICE_2_TIMESLICE_INTERVAL="Default"

gpu-test4:
pod0 ctr0:
declare -x GPU_DEVICE_3="gpu-3"
declare -x GPU_DEVICE_3_SHARING_STRATEGY="TimeSlicing"
declare -x GPU_DEVICE_3_TIMESLICE_INTERVAL="Default"
pod1 ctr0:
declare -x GPU_DEVICE_3="gpu-3"
declare -x GPU_DEVICE_3_SHARING_STRATEGY="TimeSlicing"
declare -x GPU_DEVICE_3_TIMESLICE_INTERVAL="Default"

gpu-test5:
pod0 ts-ctr0:
declare -x GPU_DEVICE_4="gpu-4"
declare -x GPU_DEVICE_4_SHARING_STRATEGY="TimeSlicing"
declare -x GPU_DEVICE_4_TIMESLICE_INTERVAL="Long"
pod0 ts-ctr1:
declare -x GPU_DEVICE_4="gpu-4"
declare -x GPU_DEVICE_4_SHARING_STRATEGY="TimeSlicing"
declare -x GPU_DEVICE_4_TIMESLICE_INTERVAL="Long"
pod0 sp-ctr0:
declare -x GPU_DEVICE_5="gpu-5"
declare -x GPU_DEVICE_5_PARTITION_COUNT="10"
declare -x GPU_DEVICE_5_SHARING_STRATEGY="SpacePartitioning"
pod0 sp-ctr1:
declare -x GPU_DEVICE_5="gpu-5"
declare -x GPU_DEVICE_5_PARTITION_COUNT="10"
declare -x GPU_DEVICE_5_SHARING_STRATEGY="SpacePartitioning"
```

In this example resource driver, no "actual" GPUs are made available to any
containers. Instead, a set of environment variables are set in each container
to indicate which GPUs *would* have been injected into them by a real resource
driver and how they *would* have been configured.

You can use the IDs of the GPUs as well as the GPU sharing settings set in
these environment variables to verify that they were handed out in a way
consistent with the semantics shown in the figure above.

Once you have verified everything is running correctly, delete all of the
example apps:
```bash
kubectl delete --wait=false --filename=demo/gpu-test{1,2,3,4,5}.yaml
```

And wait for them to terminate:
```console
$ kubectl get pod -A
NAMESPACE   NAME   READY   STATUS        RESTARTS   AGE
...
gpu-test1   pod0   1/1     Terminating   0          31m
gpu-test1   pod1   1/1     Terminating   0          31m
gpu-test2   pod0   2/2     Terminating   0          31m
gpu-test3   pod0   1/1     Terminating   0          31m
gpu-test3   pod1   1/1     Terminating   0          31m
gpu-test4   pod0   1/1     Terminating   0          31m
gpu-test5   pod0   4/4     Terminating   0          31m
...
```

Finally, you can run the following to cleanup your environment and delete the
`kind` cluster started previously:
```bash
./demo/delete-cluster.sh
```

## Anatomy of a DRA resource driver

TBD

## Code Organization

TBD

## Best Practices

TBD

## References

For more information on the DRA Kubernetes feature and developing custom resource drivers, see the following resources:

* [Dynamic Resource Allocation in Kubernetes](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
* TBD

## Community, discussion, contribution, and support

Learn how to engage with the Kubernetes community on the [community page](http://kubernetes.io/community/).

You can reach the maintainers of this project at:

- [Slack](https://slack.k8s.io/)
- [Mailing List](https://groups.google.com/a/kubernetes.io/g/dev)

### Code of conduct

Participation in the Kubernetes community is governed by the [Kubernetes Code of Conduct](code-of-conduct.md).

[owners]: https://git.k8s.io/community/contributors/guide/owners.md
[Creative Commons 4.0]: https://git.k8s.io/website/LICENSE
=======
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

2. 编译和安装DRA驱动程序：
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
>>>>>>> 98d1176 (init)
