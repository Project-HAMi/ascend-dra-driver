# Ascend DRA Driver

## Overview

The Ascend DRA Driver exposes Huawei Ascend NPUs through Kubernetes [Dynamic Resource Allocation (DRA)](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/).

The kubelet plugin discovers physical NPUs and existing vNPUs, publishes them as `ResourceSlice` objects, and handles the DRA Prepare and Unprepare lifecycle. It generates CDI specifications for allocated devices and persists prepared claims in a checkpoint.

The currently validated allocation mode is **HAMivNPUCore sharing**. The alpha `HAMivNPUCore` feature gate publishes shareable `memory` and `aicore` capacity. CDI injects libvnpu so multiple claims can consume isolated capacity on the same physical NPU.

**Full-card and traditional vNPU allocation are still under development and testing. They are not currently claimed as supported or ready for production use. These modes will be documented and released after their implementation and end-to-end validation are complete.**

The driver name is `npu.project-hami.io`.

## Installation

### Prerequisites

An Ascend node requires:

- A Linux node with supported Huawei Ascend hardware.
- Ascend Driver, DCMI, `npu-smi`, and Ascend Docker Runtime.
- A container runtime with CDI enabled.
- Kubernetes with the DRA APIs and feature gates required by the selected allocation mode.
- Kubernetes 1.34 or later for the included HAMivNPUCore consumable-capacity demo.
- Helm 3, kubectl, Docker, GNU Make, and Go 1.26 for building and deployment.

The Helm chart is located at [`deployments/helm/ascend-dra-driver`](deployments/helm/ascend-dra-driver). The recommended validated installation path is the [Kind HAMivNPUCore demo](demo/kind-vnpu/README.md), which prepares the Ascend runtime, installs the chart, and verifies the complete DRA lifecycle.

The chart enables HAMivNPUCore by default. Full-card and traditional vNPU allocation must be explicitly enabled for development or testing:

```bash
helm upgrade --install ascend-dra-driver \
  deployments/helm/ascend-dra-driver \
  --namespace ascend-dra-driver \
  --create-namespace \
  --set kubeletPlugin.fullCardAndTraditionalVNPU.enabled=true
```

This opt-in setting disables the `HAMivNPUCore` feature gate. These modes are not currently claimed as supported or production-ready.

### Ascend Driver and CANN Compatibility

HAMi-vNPU-core does not currently enforce a numeric minimum Ascend Kernel Driver version. Its compatibility requirement is ABI-based:

- `libvnpu.so` links to the host-provided `libdcmi.so` and `libruntime.so`.
- The DCMI library must export `dcmi_init`, `dcmi_get_card_list`, and `dcmi_get_device_resource_info`.
- The CANN Runtime must provide the `rt*` APIs intercepted or called by libvnpu, including stream, event, device, kernel-launch, and memory-management APIs.
- The CANN development libraries used to build libvnpu, the CANN userspace in the workload image, and the host Ascend HDK/Driver must be mutually compatible.

There is therefore no supported universal rule such as "Driver >= X" in this repository. Select the Ascend HDK/Driver version from Huawei's compatibility matrix for the exact CANN release used by the workload. For example, see the official [CANN 9.0.0 release notes and driver mapping](https://www.hiascend.com/document/detail/en/CANNCommunityEdition/900/releasenote/release-notes.md).

The current Kind demo has been validated on Ascend 310P3 with Ascend Driver/DCMI `25.5.1` and the `ascendai/cann:9.0.0-devel` image. This is a tested combination, not a minimum-version guarantee.

Check a host before deployment with:

```bash
npu-smi info
cat /usr/local/Ascend/driver/version.info

# The exact paths may differ for a non-default Ascend installation.
nm -D /usr/local/Ascend/driver/lib64/driver/libdcmi.so | \
  grep -E 'dcmi_(init|get_card_list|get_device_resource_info)'
```

A missing `libdcmi.so`/`libruntime.so` or required symbol can prevent `libvnpu.so` from loading or fail when an intercepted API is first called. The driver currently does not provide forward or backward compatibility for such missing ABI symbols.

### Build from Source

Initialize the submodules first:

```bash
git clone --recurse-submodules https://github.com/4pdOss/hami-dra-driver.git
cd hami-dra-driver
```

Run the Go checks:

```bash
make test
```

The image expects prebuilt libvnpu assets under `dist/hami-vnpu-core`. Build them in an environment that provides the required Ascend development libraries, then build the driver image:

```bash
make libvnpu-artifacts
make image
```

The default image repository is `projecthami/ascend-dra-driver`. Override `IMAGE_NAME`, `VERSION`, `BASE_IMAGE`, or `GOPROXY` when required:

```bash
make image \
  IMAGE_NAME=example.com/ascend-dra-driver \
  VERSION=0.1.0 \
  BASE_IMAGE=ubuntu:20.04
```

## Demo

The recommended demo creates a Kubernetes 1.34 Kind cluster on an ARM64 Ascend host and validates both one-device and two-device ResourceClaims.

Configure the physical card IDs in [`demo/kind-vnpu/demo.env`](demo/kind-vnpu/demo.env), then run:

```bash
cd demo/kind-vnpu
./demo.sh all
```

For an interactive run that retains the environment for inspection:

```bash
./demo.sh setup
./demo.sh prepare
./demo.sh verify
./demo.sh status
```

Clean up when finished:

```bash
./demo.sh unprepare
./demo.sh cleanup
```

See the [demo documentation](demo/kind-vnpu/README.md) for prerequisites, configuration, command semantics, and troubleshooting.

## ResourceClaim Example

The following claim requests two different Ascend NPU devices. Each allocation consumes 1 GiB of memory and 50 AI cores:

```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceClaim
metadata:
  name: two-ascend-npus
spec:
  devices:
    requests:
      - name: npu
        exactly:
          deviceClassName: ascend-vnpu
          allocationMode: ExactCount
          count: 2
          capacity:
            requests:
              npu.project-hami.io/memory: 1Gi
              npu.project-hami.io/aicore: 50
    constraints:
      - requests:
          - npu
        distinctAttribute: npu.project-hami.io/index
```

The DeviceClass name depends on the deployed configuration. The Kind demo creates its own demo-specific DeviceClass.

## Development

Useful commands include:

```bash
make assert-fmt
make vet
go test ./...
go test ./cmd/ascend-dra-kubeletplugin \
  -run 'TestMockDRA|TestMockDRALibvNPU' -v
make verify-helm-chart
```

The `ascend-dra-tester` command can enumerate devices and render discovered resources without starting the kubelet plugin. This is useful for checking a real Ascend host before cluster deployment.

## Contributing

Contributions are welcome. Please include tests for device discovery, published resources, Prepare and Unprepare behavior, CDI edits, checkpoint state, and error cleanup when changing the allocation lifecycle.

Contributions to Project HAMi require a [Developer Certificate of Origin (DCO)](https://github.com/Project-HAMi/HAMi/blob/master/CONTRIBUTING.md).

## Support

Please open a GitHub issue for questions, bug reports, and feature requests. Include the driver logs, relevant `ResourceClaim` and `ResourceSlice` objects, Kubernetes version, Ascend hardware model, and Ascend driver version when reporting a problem.
