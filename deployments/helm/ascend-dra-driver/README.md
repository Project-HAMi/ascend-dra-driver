# HAMi core metrics

Chart 0.2.0 defaults to `projecthami/ascend-dra-driver:v0.2.0` for both init and
plugin containers. This version supplies the asset installer and metrics CLI
flags. Disabling metrics still requires the new image; it does not make this
chart compatible with v0.1.0. Custom images must provide the same interfaces.

For a release, publish the matching driver image through the existing version-tag
CI (`v0.2.0` for this release), then publish the chart. Main-branch CI publishes
`latest`, which does not satisfy the versioned default. The Helm release workflow
checks the default image manifest before any release writes; if it ran before
the image build completed, rerun it after that build succeeds. The chart release
does not publish the driver image itself.

The chart enables the driver's built-in `/metrics` endpoint on port 9395 in
HAMivNPUCore mode. No NPU Exporter is required. Disable it with
`--set kubeletPlugin.metrics.enabled=false`. Traditional full-card/vNPU mode
does not start this collector.

```yaml
kubeletPlugin:
  metrics:
    enabled: true
    port: 9395
    collectionInterval: 5s
    maxAge: 15s
    serviceMonitor:
      enabled: false
      labels: {}
      interval: 15s
```

For Prometheus Operator, install its CRDs and enable `serviceMonitor.enabled`.
Set `serviceMonitor.labels` to match your Prometheus instance's selector. The
monitor preserves workload `pod` and `namespace` labels with `honorLabels: true`
and adds the target's node label. Without the Operator, discover the headless
metrics Service's individual Pod endpoints; do not scrape its VIP as one node.

Example native Prometheus discovery (replace the namespace if overridden):

```yaml
scrape_configs:
- job_name: ascend-dra
  honor_labels: true
  kubernetes_sd_configs:
  - role: endpoints
    namespaces:
      names: [ascend-dra-driver]
  relabel_configs:
  - source_labels: [__meta_kubernetes_service_name]
    regex: ascend-dra-driver-metrics
    action: keep
  - source_labels: [__meta_kubernetes_endpoint_port_name]
    regex: metrics
    action: keep
  - source_labels: [__meta_kubernetes_pod_node_name]
    target_label: node
```

The CLI also exposes `--metrics-bind-address` (empty disables),
`--metrics-collection-interval`, `--metrics-max-age` and `--pod-resources-socket`.
The kubelet PodResources DRA reporting must be enabled. The chart mounts the
kubelet PodResources socket; collection makes no Kubernetes API requests and
needs no Pod read permissions. Ownership joins the node's prepared Claim UIDs
and pool/device references with this driver's CDI identities in PodResources.
Pod/container labels come from that local snapshot, without Device Plugin
annotations or Pod/Claim API lookups. API deletion timestamps or terminal phases
do not suppress a still-prepared, observed consumer; Unprepare invalidates its
container samples. Missing/uninitialized SHM and source freshness checks still
apply.

## Metric meanings

The eight `hami_*` gauges keep the Ascend Device Plugin's names, HELP and labels:

| Metric | Meaning |
| --- | --- |
| `hami_host_gpu_memory_used_bytes` | Physical HBM used; falls back to DDR when HBM is unavailable |
| `hami_host_gpu_utilization_ratio` | Whole-device AICore utilization, **0–100** |
| `hami_vgpu_memory_used_bytes` | Sum of active process slots in the Claim's physical-card core domain |
| `hami_vgpu_memory_limit_bytes` | Runtime memory quota read from the initialized core domain |
| `hami_container_device_utilization_ratio` | The container's device's **whole-device** utilization, not independent container utilization |
| `hami_vgpu_memory_context_bytes` | Compatibility placeholder, always zero |
| `hami_vgpu_memory_module_bytes` | Compatibility placeholder, always zero |
| `hami_vgpu_memory_buffer_bytes` | Compatibility placeholder, always zero |

Host `device_index` is a logical ID; container `vdevice_index` is a physical ID.
Join by `device_uuid`, not by equality of these indices. Container labels are
`namespace`, `pod`, `container`, `vdevice_index`, `device_uuid`.

Core usage includes hook accounting and may briefly include pending allocation
reservations. It need not equal physical HBM usage or the core's separate global
admission ledger. A Claim can contain several NPUs; each card has an independent
quota and file. Only cards actually initialized by the application produce
samples. A missing/uninitialized file is not an observed zero.

The project supports **one consumer Container per Claim**. Multiple processes
and repeated requests in that Container are aggregated/deduplicated. If the
node's observations show multiple consumers, the collector suppresses that
Claim's container samples and logs the conflict. It does not enforce admission;
the planned Webhook remains separate. Conflicting label sets from different
Claims are also suppressed instead of producing duplicate Prometheus samples.

`ascend_dra_monitor_source_success{source="devices|shmem|workload"}` reports
collection success; `ascend_dra_monitor_last_success_timestamp_seconds` records
the last successful refresh. `ascend_dra_monitor_domains` counts states
`pending`, `ready`, `unmapped`, `ambiguous`, `invalid`, `stale`, `unsupported`.
Pending initialization is normal; read/association errors are not converted to
zero usage. Samples expire after `maxAge`. A successful HTTP response, an
initialized domain, or a `ready` observation does not prove complete Hook
coverage or continued resource-isolation health.

## Runtime upgrade and rollback

This release pins core `98ec21f47d6c42c9ef092e7e7e55f10e7f838072` and installs
verified assets into `/usr/local/hami-vnpu-core/releases/<coreSHA>` without
overwriting an existing mapped library. A changed build under the same SHA is
rejected. Build assets with `make libvnpu-artifacts`; revision, layout and library
checksum files must accompany `libvnpu.so` in the image.

Before upgrading from the legacy runtime:

1. Stop scheduling onto affected nodes, stop their NPU workloads and allow the
   old driver to Unprepare Claims. Confirm old core participants have exited.
2. Deploy the new image and chart, then recreate/Prepare workloads. Do not
   truncate active SHM or edit checkpoints to make running Claims appear migrated.
3. Verify CDI uses `NPU_MEMORY_LIMIT_MB` (MiB), `NPU_COMPUTE_LIMIT` (1–100),
   explicit `NPU_SCHED_POLICY=dynamic`, and the common global base
   `/hami-shared-region/global_registry`. Local files are now
   `containers/<claimUID>/vnpu_local_shmem.device-<physicalID>`.
4. Verify quotas and metrics before resuming scheduling. Old prepared edits are
   rejected with an explicit error; their Unprepare path remains available.

The core upgrade does not silently switch workloads to Spatial. Existing
allocations with `cores=0` must be recreated with a supported value. All selected
cards retain the existing equal-quota requirement. Rollback also requires stopping
participants and restoring matching driver/core/CDI/SHM conventions together.

Real NPU and e2e validation must be performed after code review; unit tests do
not establish hardware compatibility or measured isolation performance.
