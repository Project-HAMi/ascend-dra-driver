// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0

package monitor

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	podresources "k8s.io/kubelet/pkg/apis/podresources/v1"

	"github.com/Project-HAMi/hami-dra-driver/pkg/consts"
)

// WorkloadResolver reads the node's actual consumers. The monitor joins CDI
// Claim UIDs and device references with prepared state before emitting samples.
type WorkloadResolver struct {
	pr   podresources.PodResourcesListerClient
	conn *grpc.ClientConn
}

// NewWorkloadResolver opens a persistent connection to kubelet PodResources.
func NewWorkloadResolver(socket string) (*WorkloadResolver, error) {
	conn, err := grpc.NewClient((&url.URL{Scheme: "unix", Path: socket}).String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("pod resources client: %w", err)
	}
	return &WorkloadResolver{pr: podresources.NewPodResourcesListerClient(conn), conn: conn}, nil
}

// Close cancels outstanding RPCs.
func (r *WorkloadResolver) Close() { _ = r.conn.Close() }

// Resolve performs one local RPC, with no Kubernetes API requests. Under the
// project's single-consumer-per-Claim contract, PodResources supplies the labels
// and our CDI device supplies the immutable Claim UID. API deletion timestamps
// and phases must not hide consumers that are still prepared on this node.
func (r *WorkloadResolver) Resolve(ctx context.Context) (map[string][]Consumer, error) {
	response, err := r.pr.List(ctx, &podresources.ListPodResourcesRequest{})
	if err != nil {
		return nil, fmt.Errorf("list pod resources: %w", err)
	}
	result := make(map[string][]Consumer)
	const prefix = "k8s." + consts.DriverName + "/npu="
	for _, pod := range response.PodResources {
		if pod.Namespace == "" || pod.Name == "" {
			continue
		}
		for _, container := range pod.Containers {
			if container.Name == "" {
				continue
			}
			for _, resource := range container.DynamicResources {
				if resource.ClaimNamespace != pod.Namespace {
					continue
				}
				claims := make(map[string]map[DeviceRef]bool)
				for _, device := range resource.ClaimResources {
					if device.DriverName != consts.DriverName {
						continue
					}
					for _, cdi := range device.CdiDevices {
						uid, found := strings.CutPrefix(cdi.Name, prefix)
						if !found || uid == "" || uid == "common" {
							continue
						}
						if claims[uid] == nil {
							claims[uid] = make(map[DeviceRef]bool)
						}
						claims[uid][DeviceRef{Pool: device.PoolName, Device: device.DeviceName}] = true
					}
				}
				for uid, devices := range claims {
					result[uid] = append(result[uid], Consumer{
						Namespace: pod.Namespace, Pod: pod.Name, Container: container.Name, Devices: devices,
					})
				}
			}
		}
	}
	return result, nil
}
