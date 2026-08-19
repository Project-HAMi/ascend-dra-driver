# Ascend DRA Driver

The Ascend DRA Driver (`ascend.project-hami.io`) exposes Huawei Ascend NPUs through Kubernetes [Dynamic Resource Allocation (DRA)](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/).

## Support Status

The driver provides two allocation modes:

- **HAMivNPUCore sharing**, which has been validated and requires the `DRAConsumableCapacity` feature gate.
- **Full-card and traditional vNPU allocation**, which remains under development and is not production-ready.

## Prerequisites

- A Linux node with supported Huawei Ascend hardware
- Ascend Driver, DCMI, `npu-smi`, and Ascend Docker Runtime
- A container runtime with CDI enabled
- Kubernetes 1.34 or later with `DynamicResourceAllocation` enabled
- Helm 3 and kubectl for deployment
- Docker, GNU Make, and Go 1.26 for building

## Installation

The Helm chart is in [`deployments/helm/ascend-dra-driver`](deployments/helm/ascend-dra-driver). The chart enables HAMivNPUCore by default.

To opt into the experimental full-card and traditional vNPU modes instead:

```bash
helm upgrade --install ascend-dra-driver \
  deployments/helm/ascend-dra-driver \
  --namespace ascend-dra-driver \
  --create-namespace \
  --set kubeletPlugin.fullCardAndTraditionalVNPU.enabled=true
```

This setting disables the `HAMivNPUCore` feature gate.

## Demo

The Kind demo shows how to deploy the driver and allocate Ascend NPUs with a `ResourceClaim`. On an ARM64 Ascend host, configure the physical card IDs in [`demo/kind-vnpu/demo.env`](demo/kind-vnpu/demo.env), then run:

```bash
cd demo/kind-vnpu
./demo.sh all
```

See the [demo documentation](demo/kind-vnpu/README.md) for ResourceClaim examples, configuration, additional commands, and troubleshooting.

## Development

Clone the repository with its submodules:

```bash
git clone --recurse-submodules https://github.com/4pdOss/hami-dra-driver.git
cd hami-dra-driver
```

Run the checks:

```bash
make assert-fmt
make vet
go test ./...
go test ./cmd/ascend-dra-kubeletplugin \
  -run 'TestMockDRA|TestMockDRALibvNPU' -v
make verify-helm-chart
```

Build libvnpu in an environment with the required Ascend development libraries, then build the driver image:

```bash
make libvnpu-artifacts
make image \
  IMAGE_NAME=example.com/ascend-dra-driver \
  VERSION=0.1.0 \
  BASE_IMAGE=ubuntu:20.04
```

The default image repository is `projecthami/ascend-dra-driver`. `IMAGE_NAME`, `VERSION`, `BASE_IMAGE`, and `GOPROXY` can be overridden.

## Contributing

Contributions are welcome. Changes to the allocation lifecycle should include tests for device discovery, published resources, Prepare and Unprepare behavior, CDI edits, checkpoint state, and error cleanup.

Contributions require a [Developer Certificate of Origin (DCO)](https://github.com/Project-HAMi/HAMi/blob/master/CONTRIBUTING.md).

## Support

Open a GitHub issue for questions, bugs, or feature requests. Include driver logs, relevant `ResourceClaim` and `ResourceSlice` objects, Kubernetes version, Ascend hardware model, and Ascend driver version.
