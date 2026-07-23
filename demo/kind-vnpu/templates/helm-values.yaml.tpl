fullnameOverride: ascend-dra-driver
image:
  repository: @@DRIVER_IMAGE_REPOSITORY@@
  tag: @@DRIVER_IMAGE_TAG@@
  pullPolicy: Never
kubeletPlugin:
  npuSmiHostPath: /usr/local/bin/npu-smi
  nodeSelector:
    npu.project-hami.io/e2e-node: "true"
