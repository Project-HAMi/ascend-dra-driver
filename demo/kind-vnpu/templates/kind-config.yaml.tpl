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
      - hostPath: @@NPU_SMI_HOST_PATH@@
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
      - hostPath: /dev/davinci_manager
        containerPath: /dev/davinci_manager
      - hostPath: /dev/devmm_svm
        containerPath: /dev/devmm_svm
      - hostPath: /dev/hisi_hdc
        containerPath: /dev/hisi_hdc
