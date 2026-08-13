# HAMivNPUCore Kind Demo

This demo creates a Kind cluster on an ARM64 Ascend host and validates Kubernetes DRA with HAMivNPUCore.

It runs two workloads:

- `npu-share-a` requests one NPU device.
- `npu-share-b` requests two different NPU devices.

Each allocation consumes `1Gi` of memory and `50` AI cores. The verification checks ResourceClaims, device injection, CDI files, checkpoint state, and the libvnpu memory quota.

## Requirements

Run the demo on an ARM64 Ascend host with:

- Docker, kubectl, Helm, Go, Python 3, and curl.
- Ascend Driver, DCMI, `npu-smi`, and Ascend Docker Runtime.
- At least two `/dev/davinci<number>` character devices. Device numbers may be non-contiguous.
- The configured CANN and Go builder images available locally.
- Repository submodules available for initialization.

The demo automatically discovers the host's Davinci device nodes and the DCMI LogicID-to-PhyID mapping. Do not manually map card IDs to `/dev/davinciN` numbers.

## Configuration

Edit [`demo.env`](demo.env) before running the demo. Each setting is documented in that file.

At minimum, configure `DEVICE_SHARE_CARD_IDS` with the physical NPU IDs shown in the first `NPU` column of `npu-smi info`:

```bash
npu-smi info
```

```bash
: "${DEVICE_SHARE_CARD_IDS:=4 5}"
```

Card IDs are not LogicIDs and do not need to match `/dev/davinciN` suffixes.

Exported environment variables override values from `demo.env` for one command:

```bash
DRIVER_IMAGE_TAG=my-test-tag ./demo.sh setup
```

Set `SKIP_EXISTING_IMAGE_BUILDS=true` to reuse the exact configured image reference, or `false` to rebuild the driver image.

## Usage

Run commands from this directory:

```bash
cd demo/kind-vnpu
```

For an interactive workflow:

```bash
./demo.sh setup
./demo.sh prepare
./demo.sh verify
./demo.sh status
```

When finished:

```bash
./demo.sh unprepare
./demo.sh cleanup
```

For an unattended full run:

```bash
./demo.sh all
```

`all` executes `setup → prepare → verify → unprepare → cleanup`. A successful run removes the Kind cluster but retains downloaded and built images.

## Commands

| Command | Description |
| --- | --- |
| `setup` | Checks the host, prepares images, creates Kind, installs the DRA driver, imports the workload image, and creates the Namespace and DeviceClass. It does not create ResourceClaims or Pods. |
| `prepare` | Creates the two ResourceClaims and workload Pods. |
| `verify` | Waits for allocation and Pod readiness, then verifies DRA, CDI, device visibility, checkpoint state, and quotas. Run `prepare` first. |
| `unprepare` | Deletes only the demo Pods and ResourceClaims. The cluster and driver remain available for another run. |
| `status` | Shows the cluster, driver, ResourceClaims, and Pods. |
| `cleanup` | Deletes the demo Kubernetes resources and Kind cluster, restores device-share state, and retains images. |
| `all` | Runs the complete workflow automatically. |

## Inspecting the Demo

Generated YAML, logs, kubeconfig, CDI evidence, and checkpoint snapshots are stored under `DEMO_STATE_DIR`. The default is:

```text
/tmp/hami-dra-kind-vnpu-demo
```

To use kubectl directly:

```bash
export KUBECONFIG=/tmp/hami-dra-kind-vnpu-demo/kubeconfig
kubectl -n ascend-dra-vnpu-demo get resourceclaims,pods -o wide
kubectl get resourceslices -o yaml
kubectl -n ascend-dra-demo logs \
  daemonset/ascend-dra-driver-kubeletplugin \
  -c plugin --tail=300
```

Do not edit the generated `runtime.env`; change `demo.env` instead.

## Failure Handling

If a command fails, the cluster is intentionally retained for diagnosis:

```bash
./demo.sh status
```

After collecting the required information, remove the environment with:

```bash
./demo.sh cleanup
```

The cleanup process does not run Docker image pruning and does not restart or reconfigure the host Docker daemon.
