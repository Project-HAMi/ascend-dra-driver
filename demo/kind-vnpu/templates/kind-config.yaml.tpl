kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
featureGates:
  DynamicResourceAllocation: true
  DRAConsumableCapacity: true
@@CONTAINERD_CONFIG_PATCHES@@
nodes:
  - role: control-plane
@@KUBELET_CGROUP_PATCH@@
  - role: worker
    labels:
      ascend.project-hami.io/e2e-node: "true"
    extraMounts:
      - hostPath: /usr/local/Ascend
        containerPath: /usr/local/Ascend
        readOnly: true
      - hostPath: /usr/local/dcmi
        containerPath: /usr/local/dcmi
        readOnly: true
      - hostPath: @@ASCEND_RUNTIME_WRAPPER_HOST_PATH@@
        containerPath: /usr/local/bin/ascend-docker-runtime-wrapper
        readOnly: true
      - hostPath: @@NPU_SMI_HOST_PATH@@
        containerPath: /usr/local/bin/npu-smi
        readOnly: true
      - hostPath: /etc/ascend_install.info
        containerPath: /etc/ascend_install.info
        readOnly: true
      - hostPath: /etc/ascend-docker-runtime.d
        containerPath: /etc/ascend-docker-runtime.d
        readOnly: true
@@DAVINCI_DEVICE_MOUNTS@@
      - hostPath: /dev/davinci_manager
        containerPath: /dev/davinci_manager
      - hostPath: /dev/devmm_svm
        containerPath: /dev/devmm_svm
      - hostPath: /dev/hisi_hdc
        containerPath: /dev/hisi_hdc
