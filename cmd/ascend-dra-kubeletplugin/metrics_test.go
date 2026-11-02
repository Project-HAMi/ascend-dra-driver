// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"ascend-common/devmanager/common"

	"github.com/stretchr/testify/require"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	drapb "k8s.io/kubelet/pkg/apis/dra/v1"
	"k8s.io/kubernetes/pkg/kubelet/checkpointmanager"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"

	"github.com/Project-HAMi/hami-dra-driver/pkg/consts"
)

func TestCoreCDIUsesPerCardRuntimeContract(t *testing.T) {
	edits, err := buildLibvNPUCDIContainerEdits(1<<30, 50, t.TempDir())
	require.NoError(t, err)
	require.Contains(t, edits.Env, "NPU_MEMORY_LIMIT_MB=1024")
	require.Contains(t, edits.Env, "NPU_COMPUTE_LIMIT=50")
	require.Contains(t, edits.Env, "NPU_SCHED_POLICY=dynamic")
	require.Contains(t, edits.Env, "NPU_GLOBAL_SHM_PATH=/hami-shared-region/global_registry")
	for _, env := range edits.Env {
		require.False(t, strings.HasPrefix(env, "NPU_PRIORITY=") || strings.HasPrefix(env, "NPU_MEM_QUOTA="))
	}
	quota, supported := preparedCoreQuota(PreparedDevices{{ContainerEdits: &cdiapi.ContainerEdits{ContainerEdits: edits}}})
	require.True(t, supported)
	require.Equal(t, uint64(1<<30), quota)
	for _, values := range [][2]int64{{0, 50}, {1024, 50}, {1 << 30, 0}, {1 << 30, 101}} {
		_, err := buildLibvNPUCDIContainerEdits(values[0], values[1], t.TempDir())
		require.Error(t, err)
	}
}

func checkpointState(t *testing.T) (*DeviceState, PreparedDevices) {
	t.Helper()
	root := t.TempDir()
	manager, err := checkpointmanager.NewCheckpointManager(root)
	require.NoError(t, err)
	cdi, err := NewCDIHandler(&Config{flags: &Flags{cdiRoot: filepath.Join(root, "cdi")}})
	require.NoError(t, err)
	edits, err := buildLibvNPUCDIContainerEdits(1<<30, 50, filepath.Join(root, "containers", "claim"))
	require.NoError(t, err)
	devices := PreparedDevices{{Device: drapb.Device{DeviceName: "npu-1-0", PoolName: "node"}, ContainerEdits: &cdiapi.ContainerEdits{ContainerEdits: edits}}}
	cp := newCheckpoint()
	cp.V1.PreparedClaims["claim"] = devices
	require.NoError(t, manager.CreateCheckpoint(DriverPluginCheckpointFile, cp))
	state := &DeviceState{checkpointManager: manager, cdi: cdi, libvNPUHostPath: root, prepared: cp.V1.PreparedClaims,
		allocatable: AllocatableDevices{"npu-1-0": resourceapi.Device{Name: "npu-1-0", Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
			consts.DeviceAttributePhysicalID: {IntValue: int64ptr(3)}, consts.DeviceAttributeUUID: {StringValue: strptr("uuid-3")},
		}}}}
	return state, devices
}

func int64ptr(n int64) *int64 { return &n }
func strptr(s string) *string { return &s }

func TestMetricsSnapshotRestoresPreparedUIDAndPhysicalIdentity(t *testing.T) {
	state, _ := checkpointState(t)
	snapshot, err := state.metricsSnapshot()
	require.NoError(t, err)
	require.Len(t, snapshot.Domains, 1)
	d := snapshot.Domains[0]
	require.Equal(t, "claim", d.ClaimUID)
	require.Equal(t, 3, d.PhysicalID)
	require.Equal(t, "uuid-3", d.UUID)
	require.Equal(t, uint64(1<<30), d.MemoryLimit)
	require.True(t, d.Supported)
}

func TestLegacyPreparedClaimIsRejectedButCanBeUnprepared(t *testing.T) {
	enableHAMivNPUCore(t)
	state, devices := checkpointState(t)
	devices[0].ContainerEdits.Env = append(devices[0].ContainerEdits.Env, "NPU_MEM_QUOTA=1024")
	cp := newCheckpoint()
	cp.V1.PreparedClaims["claim"] = devices
	require.NoError(t, state.checkpointManager.CreateCheckpoint(DriverPluginCheckpointFile, cp))
	require.NoError(t, state.cdi.CreateClaimSpecFile("claim", devices))
	_, err := state.Prepare(&resourceapi.ResourceClaim{ObjectMeta: metav1.ObjectMeta{UID: "claim"}})
	require.ErrorContains(t, err, "incompatible prepared core")
	snapshot, err := state.metricsSnapshot()
	require.NoError(t, err)
	require.False(t, snapshot.Domains[0].Supported)
	require.NoError(t, state.Unprepare("claim"))
	snapshot, err = state.metricsSnapshot()
	require.NoError(t, err)
	require.Empty(t, snapshot.Domains)
	require.Positive(t, state.monitorGeneration.Load())
}

type metricsManager struct {
	*stubDeviceManager
	utilErr error
}

func (m *metricsManager) GetDeviceMemoryInfo(int32) (*common.MemoryInfo, error) {
	return &common.MemoryInfo{MemorySize: 32768, MemoryAvailable: 31744}, nil
}
func (m *metricsManager) GetDeviceUtilizationRate(int32, common.DeviceType) (uint32, error) {
	return 73, m.utilErr
}
func (m *metricsManager) GetProductType(int32) (string, error) { return "910B", nil }

func TestHostMetricsUsesPhysicalHBMAndPreservesFailedQueryValidity(t *testing.T) {
	stub := newStubManager().mgr.(*stubDeviceManager)
	stub.deviceList = []int32{1}
	stub.physicIDMap = map[int32]int32{1: 3}
	mgr := &metricsManager{stubDeviceManager: stub}
	manager := &AscendManager{mgr: mgr}
	hbm := func(int32, int32) (*common.HbmInfo, error) {
		return &common.HbmInfo{MemorySize: 32768, Usage: 2048}, nil
	}
	devices, err := collectMetricsHost(context.Background(), manager, hbm)
	require.NoError(t, err)
	require.Equal(t, 1, devices[0].LogicalID)
	require.Equal(t, 3, devices[0].PhysicalID)
	require.Equal(t, uint64(2048<<20), devices[0].MemoryUsed)
	require.Equal(t, float64(73), devices[0].Utilization)
	mgr.utilErr = errors.New("query failed")
	devices, err = collectMetricsHost(context.Background(), manager, func(int32, int32) (*common.HbmInfo, error) { return nil, errors.New("HBM unavailable") })
	require.Error(t, err)
	require.Equal(t, uint64(1024<<20), devices[0].MemoryUsed, "DDR fallback uses total minus available")
	require.True(t, devices[0].MemoryValid)
	require.False(t, devices[0].UtilValid, "failure must not become a valid utilization zero")
}
