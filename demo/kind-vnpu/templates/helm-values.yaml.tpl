fullnameOverride: ascend-dra-driver
image:
  repository: @@DRIVER_IMAGE_REPOSITORY@@
  tag: @@DRIVER_IMAGE_TAG@@
  pullPolicy: Never
kubeletPlugin:
  fullCardAndTraditionalVNPU:
    enabled: false
  npuSmiHostPath: /usr/local/bin/npu-smi
  nodeSelector:
    ascend.project-hami.io/e2e-node: "true"
