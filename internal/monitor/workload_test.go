// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0

package monitor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	podresources "k8s.io/kubelet/pkg/apis/podresources/v1"
	"k8s.io/utils/ptr"

	"github.com/Project-HAMi/hami-dra-driver/pkg/consts"
)

type podResourcesStub struct {
	podresources.PodResourcesListerClient
	response *podresources.ListPodResourcesResponse
}

func (s podResourcesStub) List(context.Context, *podresources.ListPodResourcesRequest, ...grpc.CallOption) (*podresources.ListPodResourcesResponse, error) {
	return s.response, nil
}

func workloadFixture(t *testing.T) (*WorkloadResolver, *podresources.ListPodResourcesResponse, *corev1.Pod) {
	t.Helper()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns", UID: "pod-uid"},
		Spec: corev1.PodSpec{NodeName: "node", ResourceClaims: []corev1.PodResourceClaim{{Name: "npu", ResourceClaimName: ptr.To("claim")}},
			Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Claims: []corev1.ResourceClaim{{Name: "npu"}}}}}},
	}
	claim := &resourceapi.ResourceClaim{ObjectMeta: metav1.ObjectMeta{Name: "claim", Namespace: "ns", UID: types.UID("claim-uid")}, Status: resourceapi.ResourceClaimStatus{Allocation: &resourceapi.AllocationResult{}}}
	response := &podresources.ListPodResourcesResponse{PodResources: []*podresources.PodResources{{Name: "pod", Namespace: "ns", Containers: []*podresources.ContainerResources{{Name: "app", DynamicResources: []*podresources.DynamicResource{{ClaimName: "claim", ClaimNamespace: "ns", ClaimResources: []*podresources.ClaimResource{{DriverName: consts.DriverName, PoolName: "node", DeviceName: "npu-1-0", CdiDevices: []*podresources.CDIDevice{{Name: "k8s." + consts.DriverName + "/npu=claim-uid"}}}}}}}}}}}
	index := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, index.Add(pod))
	r := &WorkloadResolver{client: fake.NewSimpleClientset(claim), pods: corelisters.NewPodLister(index), node: "node", synced: func() bool { return true }, pr: podResourcesStub{response: response}}
	return r, response, pod
}

func TestResolveDRAConsumerWithoutDevicePluginAnnotations(t *testing.T) {
	r, response, _ := workloadFixture(t)
	// Multiple references to the same Claim/card should remain one consumer.
	resources := response.PodResources[0].Containers[0].DynamicResources
	response.PodResources[0].Containers[0].DynamicResources = append(resources, resources[0])
	result, err := r.Resolve(context.Background())
	require.NoError(t, err)
	consumers := uniqueConsumers(result["claim-uid"])
	require.Len(t, consumers, 1)
	require.Equal(t, "pod-uid", consumers[0].PodUID)
	require.True(t, consumers[0].Devices[DeviceRef{Pool: "node", Device: "npu-1-0"}])
	gets := 0
	for _, action := range r.client.(*fake.Clientset).Actions() {
		if action.GetVerb() == "get" {
			gets++
		}
	}
	require.Equal(t, 1, gets, "claim lookups must be shared by all references")
}

func TestResolveRejectsRecreatedClaimAndOtherDriver(t *testing.T) {
	for _, change := range []string{"uid", "driver", "node", "reference"} {
		t.Run(change, func(t *testing.T) {
			r, response, pod := workloadFixture(t)
			device := response.PodResources[0].Containers[0].DynamicResources[0].ClaimResources[0]
			switch change {
			case "uid":
				device.CdiDevices[0].Name = "k8s." + consts.DriverName + "/npu=old-uid"
			case "driver":
				device.DriverName = "other.example"
			case "node":
				pod.Spec.NodeName = "other"
			case "reference":
				pod.Spec.Containers[0].Resources.Claims = nil
			}
			result, err := r.Resolve(context.Background())
			require.NoError(t, err)
			require.Empty(t, result)
		})
	}
}

func TestResolveClaimTemplateAndDetectMultipleConsumers(t *testing.T) {
	r, response, pod := workloadFixture(t)
	pod.Spec.ResourceClaims[0].ResourceClaimName = nil
	pod.Spec.ResourceClaims[0].ResourceClaimTemplateName = ptr.To("template")
	pod.Status.ResourceClaimStatuses = []corev1.PodResourceClaimStatus{{Name: "npu", ResourceClaimName: ptr.To("claim")}}
	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: "other", Resources: pod.Spec.Containers[0].Resources})
	first := response.PodResources[0].Containers[0]
	response.PodResources[0].Containers = append(response.PodResources[0].Containers, &podresources.ContainerResources{Name: "other", DynamicResources: first.DynamicResources})
	result, err := r.Resolve(context.Background())
	require.NoError(t, err)
	require.Len(t, uniqueConsumers(result["claim-uid"]), 2, "do not pick an arbitrary consumer and hide the constraint violation")
}

func TestResolveRegularAndInitContainerAcrossRestart(t *testing.T) {
	for _, initContainer := range []bool{false, true} {
		name := "regular"
		if initContainer {
			name = "init"
		}
		t.Run(name, func(t *testing.T) {
			r, _, pod := workloadFixture(t)
			statuses := &pod.Status.ContainerStatuses
			if initContainer {
				pod.Spec.InitContainers, pod.Spec.Containers = pod.Spec.Containers, nil
				statuses = &pod.Status.InitContainerStatuses
			}
			*statuses = []corev1.ContainerStatus{{Name: "app", ContainerID: "containerd://before"}}
			before, err := r.Resolve(context.Background())
			require.NoError(t, err)
			require.Len(t, uniqueConsumers(before["claim-uid"]), 1)
			(*statuses)[0].ContainerID = "containerd://after"
			after, err := r.Resolve(context.Background())
			require.NoError(t, err)
			require.Equal(t, before, after, "restart must retain the Claim's Pod UID/container-name identity")
		})
	}
}
