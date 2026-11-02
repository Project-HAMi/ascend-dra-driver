// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0

package monitor

import (
	"context"
	"fmt"
	"net/url"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	podresources "k8s.io/kubelet/pkg/apis/podresources/v1"

	"github.com/Project-HAMi/hami-dra-driver/pkg/consts"
)

// WorkloadResolver maps a Claim UID to observed consumers; the sampler enforces
// the project's unique-consumer assumption rather than duplicating group usage.
type WorkloadResolver struct {
	client kubernetes.Interface
	pods   corelisters.PodLister
	pr     podresources.PodResourcesListerClient
	node   string
	synced func() bool
	cancel context.CancelFunc
	conn   *grpc.ClientConn
}

// NewWorkloadResolver starts a node-scoped cache without blocking plugin startup.
func NewWorkloadResolver(ctx context.Context, client kubernetes.Interface, node, socket string) (*WorkloadResolver, error) {
	conn, err := grpc.NewClient((&url.URL{Scheme: "unix", Path: socket}).String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("pod resources client: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	factory := informers.NewSharedInformerFactoryWithOptions(client, 0,
		informers.WithTweakListOptions(func(o *metav1.ListOptions) { o.FieldSelector = "spec.nodeName=" + node }))
	pi := factory.Core().V1().Pods()
	r := &WorkloadResolver{client: client, pods: pi.Lister(), pr: podresources.NewPodResourcesListerClient(conn), node: node, synced: pi.Informer().HasSynced, cancel: cancel, conn: conn}
	factory.Start(ctx.Done())
	return r, nil
}

// Close cancels cache watchers and outstanding RPCs.
func (r *WorkloadResolver) Close() {
	r.cancel()
	_ = r.conn.Close()
}

// Resolve fetches one node snapshot and verifies actual Pod/Claim identities.
// Claim GETs are deduplicated per refresh, not cached by reusable names across it.
func (r *WorkloadResolver) Resolve(ctx context.Context) (map[string][]Consumer, error) {
	if !r.synced() {
		return nil, fmt.Errorf("pod cache not yet synchronized")
	}
	response, err := r.pr.List(ctx, &podresources.ListPodResourcesRequest{})
	if err != nil {
		return nil, fmt.Errorf("list pod resources: %w", err)
	}
	claims := make(map[[2]string]*resourceapi.ResourceClaim)
	result := make(map[string][]Consumer)
	for _, row := range response.PodResources {
		pod, err := r.pods.Pods(row.Namespace).Get(row.Name)
		if err != nil || pod.Spec.NodeName != r.node || pod.DeletionTimestamp != nil || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, container := range row.Containers {
			for _, resource := range container.DynamicResources {
				if !containerUsesClaim(pod, container.Name, resource.ClaimNamespace, resource.ClaimName) {
					continue
				}
				if !slices.ContainsFunc(resource.ClaimResources, func(device *podresources.ClaimResource) bool {
					return device.DriverName == consts.DriverName
				}) {
					continue
				}
				key := [2]string{resource.ClaimNamespace, resource.ClaimName}
				claim, found := claims[key]
				if !found {
					claim, err = r.client.ResourceV1().ResourceClaims(key[0]).Get(ctx, key[1], metav1.GetOptions{})
					if err != nil {
						// Failing the refresh prevents a partial API response from
						// hiding a second consumer of an allegedly unique Claim.
						return nil, fmt.Errorf("resolve claim %s/%s: %w", key[0], key[1], err)
					}
					claims[key] = claim
				}
				if claim.UID == "" || claim.DeletionTimestamp != nil || claim.Status.Allocation == nil {
					continue
				}
				// Our CDI device includes the immutable Claim UID. An old
				// PodResources row must not attach to a recreated Claim name.
				expected := "k8s." + consts.DriverName + "/npu=" + string(claim.UID)
				actual := make(map[DeviceRef]bool)
				for _, device := range resource.ClaimResources {
					if device.DriverName != consts.DriverName {
						continue
					}
					for _, cdi := range device.CdiDevices {
						if cdi.Name == expected {
							actual[DeviceRef{Pool: device.PoolName, Device: device.DeviceName}] = true
						}
					}
				}
				if len(actual) == 0 {
					continue
				}
				result[string(claim.UID)] = append(result[string(claim.UID)], Consumer{
					Namespace: pod.Namespace, Pod: pod.Name, PodUID: string(pod.UID), Container: container.Name,
					Devices: actual,
				})
			}
		}
	}
	return result, nil
}

func containerUsesClaim(pod *corev1.Pod, container, namespace, name string) bool {
	if pod.Namespace != namespace {
		return false
	}
	var requests []corev1.ResourceClaim
	for _, c := range slices.Concat(pod.Spec.Containers, pod.Spec.InitContainers) {
		if c.Name == container {
			requests = c.Resources.Claims
			break
		}
	}
	for _, request := range requests {
		for _, pc := range pod.Spec.ResourceClaims {
			if pc.Name != request.Name {
				continue
			}
			if pc.ResourceClaimName != nil && *pc.ResourceClaimName == name {
				return true
			}
			if pc.ResourceClaimTemplateName != nil {
				for _, status := range pod.Status.ResourceClaimStatuses {
					if status.Name == pc.Name && status.ResourceClaimName != nil && *status.ResourceClaimName == name {
						return true
					}
				}
			}
		}
	}
	return false
}
