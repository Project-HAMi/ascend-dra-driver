apiVersion: v1
kind: Namespace
metadata:
  name: @@TEST_NAMESPACE@@
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
          device.attributes["npu.project-hami.io"].type == "NPU"
