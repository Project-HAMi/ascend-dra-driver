// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ascend-common/devmanager/common"

	"github.com/stretchr/testify/require"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	drapb "k8s.io/kubelet/pkg/apis/dra/v1"
	"k8s.io/kubernetes/pkg/kubelet/checkpointmanager"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"

	"github.com/Project-HAMi/hami-dra-driver/internal/monitor"
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
	state := &DeviceState{checkpointManager: manager, cdi: cdi, libvNPUHostPath: root,
		allocatable: AllocatableDevices{"npu-1-0": resourceapi.Device{Name: "npu-1-0", Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
			consts.DeviceAttributePhysicalID: {IntValue: int64ptr(3)}, consts.DeviceAttributeUUID: {StringValue: strptr("uuid-3")},
		}}}}
	state.setPreparedClaims(cp.V1.PreparedClaims)
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
	require.NotZero(t, d.Generation)
	require.Equal(t, state.metricsGeneration("claim"), d.Generation)
}

func TestClaimMetricLifetimesDoNotInterruptOtherClaims(t *testing.T) {
	enableHAMivNPUCore(t)
	driver, _, _ := newMockE2EDriver(t)
	state := driver.state
	claimA := allocatedClaim("a", "claim-a", "npu-0-0", nil)
	claimB := allocatedClaim("b", "claim-b", "npu-0-0", nil)
	for _, claim := range []*resourceapi.ResourceClaim{claimA, claimB} {
		claim.Status.Allocation.Devices.Results[0].ConsumedCapacity = libvNPUConsumedCapacity("1Gi", 50)
		_, err := state.Prepare(claim)
		require.NoError(t, err)
	}
	a, b := state.metricsGeneration("claim-a"), state.metricsGeneration("claim-b")
	require.NotZero(t, a)
	require.NotZero(t, b)
	require.NotEqual(t, a, b)
	workloads := make(map[string][]monitor.Consumer)
	for _, uid := range []string{"claim-a", "claim-b"} {
		// Fixed core ABI fixture; this test uses real lifecycle/checkpoint/CDI
		// and HTTP, but performs no hardware or Kubernetes API operation.
		data := make([]byte, 6224)
		binary.LittleEndian.PutUint64(data, 1<<30)
		binary.LittleEndian.PutUint32(data[1080:], 1)
		binary.LittleEndian.PutUint64(data[1088:], 64<<20)
		binary.LittleEndian.PutUint32(data[1152:], 1)
		binary.LittleEndian.PutUint32(data[6220:], 1)
		path := filepath.Join(state.libvNPUHostPath, "containers", uid, "vnpu_local_shmem.device-0")
		require.NoError(t, os.WriteFile(path, data, 0600))
		workloads[uid] = []monitor.Consumer{{Namespace: "ns", Pod: uid, Container: "app", Devices: map[monitor.DeviceRef]bool{{Pool: mockE2ENodeName, Device: "npu-0-0"}: true}}}
	}
	// Keep sampling idle after the initial observation. Every following request
	// must validate the cached metrics against the actual per-Claim tokens.
	m, err := monitor.Start(context.Background(), monitor.Config{
		BindAddress: "127.0.0.1:0", ClaimsRoot: filepath.Join(state.libvNPUHostPath, "containers"), Interval: time.Hour, MaxAge: 2 * time.Hour,
	}, monitor.Sources{
		Prepared: state.metricsSnapshot, Generation: state.metricsGeneration,
		Workloads: func(context.Context) (map[string][]monitor.Consumer, error) { return workloads, nil },
		Host: func(context.Context) ([]monitor.HostDevice, error) {
			return []monitor.HostDevice{{PhysicalID: 0, UUID: "npu-uuid-0", MemoryValid: true, UtilValid: true}}, nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, m.Close(ctx))
	})
	client := &http.Client{Timeout: time.Second}
	fetch := func() (string, error) {
		response, err := client.Get("http://" + m.Address() + "/metrics")
		if err != nil {
			return "", err
		}
		defer func() { _ = response.Body.Close() }()
		data, err := io.ReadAll(response.Body)
		return string(data), err
	}
	require.Eventually(t, func() bool {
		body, err := fetch()
		return err == nil && strings.Contains(body, `pod="claim-a"`) && strings.Contains(body, `pod="claim-b"`)
	}, time.Second, time.Millisecond)
	scrape := func() string {
		body, err := fetch()
		require.NoError(t, err)
		return body
	}
	before := scrape()
	_, err = state.Prepare(claimA) // Idempotent retry changes neither lifetime.
	require.NoError(t, err)
	require.Equal(t, before, scrape())
	_, err = state.Prepare(&resourceapi.ResourceClaim{ObjectMeta: metav1.ObjectMeta{UID: "unallocated"}})
	require.ErrorContains(t, err, "not yet allocated")
	require.Zero(t, state.metricsGeneration("unallocated"))
	require.Equal(t, before, scrape())

	// A cached Prepare can fail without changing its durable prepared state.
	path := state.libvNPUHostPath
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blocked, []byte("blocked"), 0600))
	state.libvNPUHostPath = blocked
	_, err = state.Prepare(claimA)
	state.libvNPUHostPath = path
	require.Error(t, err)
	require.Equal(t, before, scrape())

	manager := state.checkpointManager
	state.checkpointManager = &failingCreateCheckpointManager{CheckpointManager: manager}
	claimC := allocatedClaim("c", "claim-c", "npu-0-0", nil)
	claimC.Status.Allocation.Devices.Results[0].ConsumedCapacity = libvNPUConsumedCapacity("1Gi", 50)
	_, err = state.Prepare(claimC)
	require.ErrorContains(t, err, "injected checkpoint failure")
	require.Zero(t, state.metricsGeneration("claim-c"))
	require.Equal(t, before, scrape())
	err = state.Unprepare("claim-a")
	state.checkpointManager = manager
	require.ErrorContains(t, err, "injected checkpoint failure")
	require.Equal(t, a, state.metricsGeneration("claim-a"))
	require.Equal(t, b, state.metricsGeneration("claim-b"))
	require.Equal(t, before, scrape())
	// The failed operation may already have removed the CDI file; restore it
	// before retrying so this test isolates versioning from cleanup behavior.
	require.NoError(t, state.cdi.CreateClaimSpecFile("claim-a", state.prepared["claim-a"]))
	require.NoError(t, state.Unprepare("claim-a"))
	require.Zero(t, state.metricsGeneration("claim-a"))
	afterRemoval := scrape()
	require.NotContains(t, afterRemoval, `pod="claim-a"`)
	require.Contains(t, afterRemoval, `pod="claim-b"`)
	require.Contains(t, afterRemoval, `ascend_dra_monitor_domains{state="ready"} 1`)
	require.NoError(t, state.Unprepare("claim-a")) // Idempotent Unprepare.
	require.Equal(t, afterRemoval, scrape())
	_, err = state.Prepare(claimA) // Same UID gets a fresh token, avoiding ABA.
	require.NoError(t, err)
	require.NotEqual(t, a, state.metricsGeneration("claim-a"))
	require.Equal(t, b, state.metricsGeneration("claim-b"))
	require.NotContains(t, scrape(), `pod="claim-a"`, "old samples cannot reappear for a new lifetime")
	require.Contains(t, scrape(), `pod="claim-b"`)

	// Scrapes must not acquire the lock held during Prepare/checkpoint I/O.
	func() {
		state.Lock()
		defer state.Unlock()
		done := make(chan error, 1)
		go func() { _, err := fetch(); done <- err }()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("scrape waited for lifecycle I/O lock")
		}
	}()
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
	require.Zero(t, state.metricsGeneration("claim"))
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
