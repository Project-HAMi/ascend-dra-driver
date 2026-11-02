// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0

package monitor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	podresources "k8s.io/kubelet/pkg/apis/podresources/v1"

	"github.com/Project-HAMi/hami-dra-driver/pkg/consts"
)

type podResourcesStub struct {
	podresources.PodResourcesListerClient
	response *podresources.ListPodResourcesResponse
	err      error
	calls    int
}

func (s *podResourcesStub) List(ctx context.Context, _ *podresources.ListPodResourcesRequest, _ ...grpc.CallOption) (*podresources.ListPodResourcesResponse, error) {
	s.calls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.response, s.err
}

func consumerRow(namespace, pod, container, uid string) *podresources.PodResources {
	return &podresources.PodResources{Name: pod, Namespace: namespace, Containers: []*podresources.ContainerResources{{
		Name: container,
		DynamicResources: []*podresources.DynamicResource{{
			ClaimNamespace: namespace, ClaimName: "claim",
			ClaimResources: []*podresources.ClaimResource{{
				DriverName: consts.DriverName, PoolName: "node", DeviceName: "npu-1-0",
				CdiDevices: []*podresources.CDIDevice{{Name: "k8s." + consts.DriverName + "/npu=common"}, {Name: "k8s." + consts.DriverName + "/npu=" + uid}},
			}},
		}},
	}}}
}

func workloadFixture() (*WorkloadResolver, *podResourcesStub) {
	pr := &podResourcesStub{response: &podresources.ListPodResourcesResponse{PodResources: []*podresources.PodResources{consumerRow("ns", "pod", "app", "claim-uid")}}}
	return &WorkloadResolver{pr: pr}, pr
}

func TestResolveDRAConsumerWithoutKubernetesAPI(t *testing.T) {
	r, pr := workloadFixture()
	container := pr.response.PodResources[0].Containers[0]
	resource := container.DynamicResources[0]
	otherCard := &podresources.ClaimResource{
		DriverName: consts.DriverName, PoolName: "node", DeviceName: "npu-0-0",
		CdiDevices: resource.ClaimResources[0].CdiDevices,
	}
	resource.ClaimResources = append(resource.ClaimResources, otherCard)
	// Multiple requests/cards still belong to the same Container.
	container.DynamicResources = append(container.DynamicResources, resource)
	result, err := r.Resolve(context.Background())
	require.NoError(t, err)
	require.Len(t, result, 1, "the common CDI device is not a Claim")
	consumers := uniqueConsumers(result["claim-uid"])
	require.Len(t, consumers, 1)
	require.Equal(t, "ns", consumers[0].Namespace)
	require.Equal(t, "pod", consumers[0].Pod)
	require.Equal(t, "app", consumers[0].Container)
	require.Equal(t, map[DeviceRef]bool{{Pool: "node", Device: "npu-1-0"}: true, {Pool: "node", Device: "npu-0-0"}: true}, consumers[0].Devices)
	require.Equal(t, 1, pr.calls)
}

func TestResolveRejectsForeignOrMissingCDIIdentity(t *testing.T) {
	for _, change := range []string{"driver", "namespace", "foreign-cdi", "empty-uid", "common-only", "pod-name", "pod-namespace", "container"} {
		t.Run(change, func(t *testing.T) {
			r, pr := workloadFixture()
			resource := pr.response.PodResources[0].Containers[0].DynamicResources[0]
			device := resource.ClaimResources[0]
			switch change {
			case "driver":
				device.DriverName = "other.example"
			case "namespace":
				resource.ClaimNamespace = "other"
			case "foreign-cdi":
				device.CdiDevices[1].Name = "k8s.other.example/npu=claim-uid"
			case "empty-uid":
				device.CdiDevices[1].Name = "k8s." + consts.DriverName + "/npu="
			case "common-only":
				device.CdiDevices = device.CdiDevices[:1]
			case "pod-name":
				pr.response.PodResources[0].Name = ""
			case "pod-namespace":
				pr.response.PodResources[0].Namespace = ""
				resource.ClaimNamespace = ""
			case "container":
				pr.response.PodResources[0].Containers[0].Name = ""
			}
			result, err := r.Resolve(context.Background())
			require.NoError(t, err)
			require.Empty(t, result)
		})
	}
}

func TestResolveKeepsConsumersInDifferentPodsNamespacesOrContainersDistinct(t *testing.T) {
	for _, identity := range [][3]string{{"ns", "other-pod", "app"}, {"other-ns", "pod", "app"}, {"ns", "pod", "other-container"}} {
		t.Run(fmt.Sprint(identity), func(t *testing.T) {
			r, pr := workloadFixture()
			pr.response.PodResources = append(pr.response.PodResources, consumerRow(identity[0], identity[1], identity[2], "claim-uid"))
			result, err := r.Resolve(context.Background())
			require.NoError(t, err)
			require.Len(t, uniqueConsumers(result["claim-uid"]), 2, "never hide an observed violation of the unique-consumer contract")
		})
	}
}

func TestResolveManyClaimsUsesOneLocalRPCPerRefresh(t *testing.T) {
	for _, count := range []int{36, 128} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			r, pr := workloadFixture()
			pr.response.PodResources = nil
			for i := range count {
				pr.response.PodResources = append(pr.response.PodResources, consumerRow("ns", fmt.Sprintf("pod-%d", i), "app", fmt.Sprintf("claim-%d", i)))
			}
			for cycle := range 3 {
				ctx, cancel := context.WithTimeout(context.Background(), DefaultInterval)
				result, err := r.Resolve(ctx)
				cancel()
				require.NoError(t, err)
				require.Len(t, result, count)
				require.Equal(t, cycle+1, pr.calls)
			}
		})
	}
}

func TestResolvePropagatesPodResourcesFailure(t *testing.T) {
	r, pr := workloadFixture()
	pr.err = errors.New("kubelet unavailable")
	result, err := r.Resolve(context.Background())
	require.ErrorContains(t, err, "kubelet unavailable")
	require.Nil(t, result, "a failed source must not be treated as an empty successful snapshot")
}

type podResourcesServer struct {
	podresources.UnimplementedPodResourcesListerServer
	response *podresources.ListPodResourcesResponse
	calls    atomic.Int32
}

func (s *podResourcesServer) List(context.Context, *podresources.ListPodResourcesRequest) (*podresources.ListPodResourcesResponse, error) {
	s.calls.Add(1)
	return s.response, nil
}

func TestBackgroundResolveManyClaims(t *testing.T) {
	root := t.TempDir()
	socket := filepath.Join(root, "kubelet.sock")
	listener, err := net.Listen("unix", socket)
	require.NoError(t, err)
	server := grpc.NewServer()
	pr := &podResourcesServer{response: &podresources.ListPodResourcesResponse{}}
	podresources.RegisterPodResourcesListerServer(server, pr)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	resolver, err := NewWorkloadResolver(socket)
	require.NoError(t, err)
	prepared := PreparedSnapshot{}
	for i := range 36 {
		uid := fmt.Sprintf("claim-%d", i)
		pr.response.PodResources = append(pr.response.PodResources, consumerRow("ns", fmt.Sprintf("pod-%d", i), "app", uid))
		domain := Domain{ClaimUID: uid, Generation: 1, Pool: "node", Device: "npu-1-0", PhysicalID: 3, UUID: "uuid", MemoryLimit: 1 << 30, Supported: true}
		prepared.Domains = append(prepared.Domains, domain)
		writeDomain(t, root, domain, coreFixture())
	}
	cfg := Config{BindAddress: "127.0.0.1:0", ClaimsRoot: root, Interval: 50 * time.Millisecond, MaxAge: 150 * time.Millisecond}
	started := time.Now()
	m, err := Start(context.Background(), cfg, Sources{
		Prepared: func() (PreparedSnapshot, error) { return prepared, nil }, Generation: func(string) uint64 { return 1 },
		Workloads: resolver.Resolve, Close: resolver.Close,
		Host: func(context.Context) ([]HostDevice, error) {
			return []HostDevice{{LogicalID: 1, PhysicalID: 3, UUID: "uuid", MemoryValid: true, UtilValid: true}}, nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, m.Close(ctx))
	})
	// No HTTP scrape drives this collection, and no API client/cache is provided.
	// All 36 domains must remain observable well beyond the cache's maximum age.
	require.Eventually(t, func() bool {
		m.c.RLock()
		defer m.c.RUnlock()
		return time.Since(started) > 2*cfg.MaxAge && pr.calls.Load() >= 8 &&
			countObservedDomains(m.c.current, "ready") == 36 && m.c.current.ok["workload"]
	}, 3*time.Second, 10*time.Millisecond)
	require.Contains(t, gather(t, m.c), "hami_vgpu_memory_used_bytes")
}

func TestPreparedStateGatesCDIIdentityAndMetricLifetime(t *testing.T) {
	m, prepared, _, now := sampleMonitor(t)
	r, pr := workloadFixture()
	m.src.Workloads = r.Resolve
	device := pr.response.PodResources[0].Containers[0].DynamicResources[0].ClaimResources[0]
	domain := prepared.Domains[0]
	prepared.Domains = nil

	// Even an initialized file plus a PodResources row is insufficient before
	// Prepare. No Pod/Claim API object or informer is required at any point.
	device.CdiDevices[1].Name = "k8s." + consts.DriverName + "/npu=claim"
	m.refresh(context.Background(), now)
	require.NotContains(t, gather(t, m.c), "hami_vgpu_")

	prepared.Domains = []Domain{domain}
	prepared.Domains[0].Generation++
	m.refresh(context.Background(), now)
	require.Contains(t, gather(t, m.c), "hami_vgpu_memory_used_bytes")

	// A terminating or API-deleted Pod still has this node's prepared Claim
	// and actual kubelet row: collection must not expire after maxAge just
	// because Kubernetes API objects are unavailable.
	later := now.Add(2 * m.config.MaxAge)
	m.host.at = later
	m.last["devices"] = later
	m.c.now = func() time.Time { return later }
	m.refresh(context.Background(), later)
	require.Contains(t, gather(t, m.c), "hami_vgpu_memory_used_bytes")

	// A same-name Claim with a new UID cannot consume the old CDI identity.
	prepared.Domains[0].ClaimUID = "replacement"
	prepared.Domains[0].Generation++
	writeDomain(t, m.config.ClaimsRoot, prepared.Domains[0], coreFixture())
	m.refresh(context.Background(), later)
	require.Equal(t, 1, countObservedDomains(m.c.current, "unmapped"))
	require.NotContains(t, gather(t, m.c), "hami_vgpu_")
	device.CdiDevices[1].Name = "k8s." + consts.DriverName + "/npu=replacement"
	device.PoolName = "another-node"
	m.refresh(context.Background(), later)
	require.NotContains(t, gather(t, m.c), "hami_vgpu_", "UID alone must not bypass the prepared pool/device match")
	device.PoolName = "node"
	m.refresh(context.Background(), later)
	require.Contains(t, gather(t, m.c), "hami_vgpu_memory_used_bytes")

	// Unprepare invalidates every old container sample immediately, even if
	// kubelet still returns the previous row and the SHM file remains.
	prepared.Domains = nil
	output := gather(t, m.c)
	require.NotContains(t, output, "hami_vgpu_")
	require.NotContains(t, output, "hami_container_")
	require.Contains(t, output, "hami_host_gpu_memory_used_bytes")
	m.refresh(context.Background(), later)
	require.Empty(t, m.c.current.domains)
}
