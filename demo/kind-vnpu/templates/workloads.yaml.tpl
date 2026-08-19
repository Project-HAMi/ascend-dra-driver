apiVersion: resource.k8s.io/v1
kind: ResourceClaim
metadata:
  namespace: @@TEST_NAMESPACE@@
  name: npu-share-a
spec:
  devices:
    requests:
      - name: npu
        exactly:
          deviceClassName: ascend-vnpu-same-device-e2e
          allocationMode: ExactCount
          count: 1
          capacity:
            requests:
              memory: 1Gi
              cores: 50
---
apiVersion: resource.k8s.io/v1
kind: ResourceClaim
metadata:
  namespace: @@TEST_NAMESPACE@@
  name: npu-share-b
spec:
  devices:
    requests:
      - name: npu
        exactly:
          deviceClassName: ascend-vnpu-same-device-e2e
          allocationMode: ExactCount
          count: 2
          capacity:
            requests:
              memory: 1Gi
              cores: 50
    constraints:
      - requests:
          - npu
        distinctAttribute: ascend.project-hami.io/index
---
apiVersion: v1
kind: Pod
metadata:
  namespace: @@TEST_NAMESPACE@@
  name: npu-share-a
spec:
  runtimeClassName: ascend
  restartPolicy: Never
  nodeSelector:
    ascend.project-hami.io/e2e-node: "true"
  containers:
    - name: workload
      image: @@WORKLOAD_IMAGE@@
      imagePullPolicy: Never
      command: ["/bin/bash", "-c"]
      args:
        - |
          exec python3 -u -c '
          import ctypes
          import os
          import time
          runtime = ctypes.CDLL("libruntime.so")
          runtime.rtSetDevice.argtypes = [ctypes.c_int]
          runtime.rtSetDevice.restype = ctypes.c_ulong
          set_device_ret = runtime.rtSetDevice(0)
          free = ctypes.c_size_t()
          total = ctypes.c_size_t()
          probe = ctypes.CDLL(None).rtMemGetInfoEx
          probe.argtypes = [
              ctypes.c_ulong,
              ctypes.POINTER(ctypes.c_size_t),
              ctypes.POINTER(ctypes.c_size_t),
          ]
          probe.restype = ctypes.c_ulong
          probe_ret = probe(0, ctypes.byref(free), ctypes.byref(total))
          print(
              "set_device_ret=%d probe_ret=%d free=%d total=%d local=%s"
              % (
                  set_device_ret,
                  probe_ret,
                  free.value,
                  total.value,
                  os.environ["NPU_LOCAL_SHM_PATH"],
              ),
              flush=True,
          )
          while True:
              time.sleep(60)
          '
      resources:
        claims:
          - name: npu
  resourceClaims:
    - name: npu
      resourceClaimName: npu-share-a
---
apiVersion: v1
kind: Pod
metadata:
  namespace: @@TEST_NAMESPACE@@
  name: npu-share-b
spec:
  runtimeClassName: ascend
  restartPolicy: Never
  nodeSelector:
    ascend.project-hami.io/e2e-node: "true"
  containers:
    - name: workload
      image: @@WORKLOAD_IMAGE@@
      imagePullPolicy: Never
      command: ["/bin/bash", "-c"]
      args:
        - |
          exec python3 -u -c '
          import ctypes
          import os
          import time
          runtime = ctypes.CDLL("libruntime.so")
          runtime.rtSetDevice.argtypes = [ctypes.c_int]
          runtime.rtSetDevice.restype = ctypes.c_ulong
          set_device_ret = runtime.rtSetDevice(0)
          free = ctypes.c_size_t()
          total = ctypes.c_size_t()
          probe = ctypes.CDLL(None).rtMemGetInfoEx
          probe.argtypes = [
              ctypes.c_ulong,
              ctypes.POINTER(ctypes.c_size_t),
              ctypes.POINTER(ctypes.c_size_t),
          ]
          probe.restype = ctypes.c_ulong
          probe_ret = probe(0, ctypes.byref(free), ctypes.byref(total))
          print(
              "set_device_ret=%d probe_ret=%d free=%d total=%d local=%s"
              % (
                  set_device_ret,
                  probe_ret,
                  free.value,
                  total.value,
                  os.environ["NPU_LOCAL_SHM_PATH"],
              ),
              flush=True,
          )
          while True:
              time.sleep(60)
          '
      resources:
        claims:
          - name: npu
  resourceClaims:
    - name: npu
      resourceClaimName: npu-share-b
