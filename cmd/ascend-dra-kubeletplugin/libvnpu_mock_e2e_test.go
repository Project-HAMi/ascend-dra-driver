/*
 * Copyright 2026 The HAMi Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/kubernetes/pkg/kubelet/checkpointmanager"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"
	cdispec "tags.cncf.io/container-device-interface/specs-go"

	"github.com/Project-HAMi/hami-dra-driver/pkg/consts"
	"github.com/Project-HAMi/hami-dra-driver/pkg/featuregates"
)

type failingCreateCheckpointManager struct {
	checkpointmanager.CheckpointManager
}

func (m *failingCreateCheckpointManager) CreateCheckpoint(
	_ string,
	_ checkpointmanager.Checkpoint,
) error {
	return errors.New("injected checkpoint failure")
}

func TestMockDRALibvNPULifecycle(t *testing.T) {
	enableHAMivNPUCore(t)
	driver, publisher, cdiRoot := newMockE2EDriver(t)

	driver.syncAllocatable()
	published := publishedDevices(t, publisher.lastPublished(t))
	require.Len(t, published, 1)
	assert.Equal(t, "npu-0-0", published[0].Name)
	require.Contains(t, published[0].Attributes, physicalIDAttributeName)
	assert.Equal(t, int64(0), ptr.Deref(
		published[0].Attributes[physicalIDAttributeName].IntValue,
		int64(-1),
	))
	require.NotNil(t, published[0].AllowMultipleAllocations)
	assert.True(t, *published[0].AllowMultipleAllocations)
	require.Contains(t, published[0].Capacity, resourceapi.QualifiedName(consts.DeviceCapacityMemory))
	require.Contains(t, published[0].Capacity, resourceapi.QualifiedName(consts.DeviceCapacityCores))
	assert.NotContains(t, published[0].Capacity, resourceapi.QualifiedName(DriverDomain+consts.DeviceCapacityMemory))
	assert.NotContains(t, published[0].Capacity, resourceapi.QualifiedName(DriverDomain+"aicore"))
	for _, name := range []resourceapi.QualifiedName{
		consts.DeviceAttributeUUID,
		consts.DeviceAttributeProductName,
		consts.DeviceAttributeBrand,
		consts.DeviceAttributeType,
	} {
		require.Contains(t, published[0].Attributes, name)
	}
	assert.Equal(t, consts.DeviceTypeHAMivNPUCore, ptr.Deref(
		published[0].Attributes[consts.DeviceAttributeType].StringValue,
		"",
	))

	claim := allocatedClaim("claim-libvnpu", "uid-libvnpu", "npu-0-0", nil)
	claim.Status.Allocation.Devices.Results[0].ConsumedCapacity = libvNPUConsumedCapacity("1024Mi", 50)

	prepared, err := driver.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	require.NoError(t, err)
	require.NoError(t, prepared[claim.UID].Err)
	require.Len(t, prepared[claim.UID].Devices, 1)
	assert.Equal(t, "npu-0-0", prepared[claim.UID].Devices[0].DeviceName)
	assert.Empty(t, driver.state.vnpuManager.PhysicalNPUSnapshots())

	spec := claimSpecContent(t, cdiRoot, "uid-libvnpu")
	assert.Contains(t, spec, "ASCEND_VISIBLE_DEVICES=0")
	assert.Contains(t, spec, "NPU_MEM_QUOTA=1024")
	assert.Contains(t, spec, "NPU_PRIORITY=50")
	assert.Contains(t, spec, "NPU_GLOBAL_SHM_PATH=/hami-shared-region/0_global_registry")
	assert.Contains(t, spec, "NPU_LOCAL_SHM_PATH=/hami-vnpu-shmem/vnpu_local_shmem")
	assert.Contains(t, spec, "/hami-vnpu-core")
	assert.Contains(t, spec, "/etc/ld.so.preload")
	localShmemDir := filepath.Join(driver.state.libvNPUHostPath, "containers", "uid-libvnpu")
	require.DirExists(t, localShmemDir)

	unprepared, err := driver.UnprepareResourceClaims(context.Background(), []kubeletplugin.NamespacedObject{
		{
			NamespacedName: types.NamespacedName{Namespace: "default", Name: "claim-libvnpu"},
			UID:            claim.UID,
		},
	})
	require.NoError(t, err)
	require.NoError(t, unprepared[claim.UID])
	assert.False(t, claimSpecContains(t, cdiRoot, "uid-libvnpu"))
	assertPreparedClaimMissing(t, driver.state, "uid-libvnpu")
	assert.NoDirExists(t, localShmemDir)
	assert.ElementsMatch(t, []string{"npu-0-0"}, publishedDeviceNames(t, publisher.lastPublished(t)))
}

func TestMockDRALibvNPUTwoDeviceLifecycle(t *testing.T) {
	enableHAMivNPUCore(t)
	driver, publisher, cdiRoot := newMockE2EDriver(t)

	secondDevice := driver.state.allocatable["npu-0-0"]
	secondDevice.Name = "npu-1-0"
	secondDevice.Attributes = make(map[resourceapi.QualifiedName]resourceapi.DeviceAttribute, len(secondDevice.Attributes))
	for name, attribute := range driver.state.allocatable["npu-0-0"].Attributes {
		secondDevice.Attributes[name] = attribute
	}
	secondDevice.Attributes[resourceapi.QualifiedName(consts.DeviceAttributeIndex)] = resourceapi.DeviceAttribute{
		IntValue: ptr.To(int64(1)),
	}
	secondDevice.Attributes[physicalIDAttributeName] = resourceapi.DeviceAttribute{
		IntValue: ptr.To(int64(2)),
	}
	driver.state.allocatable[secondDevice.Name] = secondDevice
	driver.syncAllocatable()
	assert.ElementsMatch(t, []string{"npu-0-0", "npu-1-0"}, publishedDeviceNames(t, publisher.lastPublished(t)))

	claim := allocatedClaim("claim-libvnpu-two", "uid-libvnpu-two", "npu-0-0", nil)
	claim.Status.Allocation.Devices.Results[0].ConsumedCapacity = libvNPUConsumedCapacity("1024Mi", 50)
	claim.Status.Allocation.Devices.Results = append(claim.Status.Allocation.Devices.Results,
		resourceapi.DeviceRequestAllocationResult{
			Request:          "npu",
			Driver:           consts.DriverName,
			Pool:             mockE2ENodeName,
			Device:           "npu-1-0",
			ConsumedCapacity: libvNPUConsumedCapacity("1024Mi", 50),
		})

	prepared, err := driver.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	require.NoError(t, err)
	require.NoError(t, prepared[claim.UID].Err)
	require.Len(t, prepared[claim.UID].Devices, 2)
	assert.ElementsMatch(t, []string{"npu-0-0", "npu-1-0"}, []string{
		prepared[claim.UID].Devices[0].DeviceName,
		prepared[claim.UID].Devices[1].DeviceName,
	})

	spec := claimSpec(t, cdiRoot, "uid-libvnpu-two")
	require.Len(t, spec.Devices, 1)
	edits := spec.Devices[0].ContainerEdits
	assert.ElementsMatch(t, []string{
		"ASCEND_VISIBLE_DEVICES=0,2",
		"NPU_MEM_QUOTA=1024",
		"NPU_PRIORITY=50",
		"NPU_GLOBAL_SHM_PATH=/hami-shared-region/0_global_registry",
		"NPU_LOCAL_SHM_PATH=/hami-vnpu-shmem/vnpu_local_shmem",
	}, edits.Env)

	mountCounts := make(map[string]int)
	mountHostPaths := make(map[string]string)
	for _, mount := range edits.Mounts {
		mountCounts[mount.ContainerPath]++
		mountHostPaths[mount.ContainerPath] = mount.HostPath
		assert.Contains(t, mount.Options, "bind", "mount %s must be a bind mount", mount.ContainerPath)
	}
	for _, path := range []string{
		"/etc/ascend_install.info",
		"/usr/local/Ascend/driver/lib64/driver",
		"/usr/local/Ascend/driver/version.info",
		"/hami-vnpu-core",
		"/etc/ld.so.preload",
		"/hami-shared-region",
		"/hami-vnpu-shmem",
	} {
		assert.Equal(t, 1, mountCounts[path], "mount %s must be injected exactly once", path)
	}
	assert.Zero(t, mountCounts["/usr/local/bin/npu-smi"], "npu-smi is a plugin startup dependency")
	localShmemDir := filepath.Join(driver.state.libvNPUHostPath, "containers", "uid-libvnpu-two")
	assert.Equal(t, localShmemDir, mountHostPaths["/hami-vnpu-shmem"])
	require.DirExists(t, localShmemDir)

	preparedAgain, err := driver.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	require.NoError(t, err)
	require.NoError(t, preparedAgain[claim.UID].Err)
	assert.Equal(t, prepared[claim.UID].Devices, preparedAgain[claim.UID].Devices)

	unprepared, err := driver.UnprepareResourceClaims(context.Background(), []kubeletplugin.NamespacedObject{
		{
			NamespacedName: types.NamespacedName{Namespace: "default", Name: "claim-libvnpu-two"},
			UID:            claim.UID,
		},
	})
	require.NoError(t, err)
	require.NoError(t, unprepared[claim.UID])
	assert.False(t, claimSpecContains(t, cdiRoot, "uid-libvnpu-two"))
	assertPreparedClaimMissing(t, driver.state, "uid-libvnpu-two")
	assert.NoDirExists(t, localShmemDir)
}

func TestLibvNPUApplyConfigRejectsMismatchedCapacity(t *testing.T) {
	enableHAMivNPUCore(t)
	state := &DeviceState{}
	results := []*resourceapi.DeviceRequestAllocationResult{
		{
			Request:          "npu",
			Device:           "npu-0-0",
			ConsumedCapacity: libvNPUConsumedCapacity("1024Mi", 50),
		},
		{
			Request:          "npu",
			Device:           "npu-1-0",
			ConsumedCapacity: libvNPUConsumedCapacity("2048Mi", 50),
		},
	}

	_, err := state.applyLibvNPUConfig(results, t.TempDir())
	require.ErrorContains(t, err, "capacities must be same across devices")
}

func TestLibvNPUApplyConfigAcceptsLegacyQualifiedCapacityNames(t *testing.T) {
	enableHAMivNPUCore(t)
	driver, _, _ := newMockE2EDriver(t)
	result := &resourceapi.DeviceRequestAllocationResult{
		Request:          "npu",
		Device:           "npu-0-0",
		ConsumedCapacity: legacyLibvNPUConsumedCapacity("1024Mi", 50),
	}

	edits, err := driver.state.applyLibvNPUConfig(
		[]*resourceapi.DeviceRequestAllocationResult{result},
		t.TempDir(),
	)
	require.NoError(t, err)
	require.Contains(t, edits, "npu-0-0")
	assert.Contains(t, edits["npu-0-0"].Env, "NPU_MEM_QUOTA=1024")
	assert.Contains(t, edits["npu-0-0"].Env, "NPU_PRIORITY=50")
}

func TestLibvNPUClaimLocalShmemPathRejectsUnsafeUID(t *testing.T) {
	state := &DeviceState{libvNPUHostPath: t.TempDir()}

	_, err := state.libvNPUClaimLocalShmemDir("../other-claim")

	require.ErrorContains(t, err, "invalid claim UID")
}

func TestLibvNPUPrepareRollsBackCDIAndLocalShmemWhenCheckpointFails(t *testing.T) {
	enableHAMivNPUCore(t)
	driver, _, cdiRoot := newMockE2EDriver(t)
	claim := allocatedClaim("claim-libvnpu-failure", "uid-libvnpu-failure", "npu-0-0", nil)
	claim.Status.Allocation.Devices.Results[0].ConsumedCapacity = libvNPUConsumedCapacity("1024Mi", 50)
	driver.state.checkpointManager = &failingCreateCheckpointManager{
		CheckpointManager: driver.state.checkpointManager,
	}

	_, err := driver.state.Prepare(claim)

	require.ErrorContains(t, err, "injected checkpoint failure")
	assert.False(t, claimSpecContains(t, cdiRoot, string(claim.UID)))
	assert.NoDirExists(t, filepath.Join(
		driver.state.libvNPUHostPath,
		"containers",
		string(claim.UID),
	))
}

func TestDefaultModeUnprepareDoesNotRemoveLibvNPUClaimDirectory(t *testing.T) {
	require.NoError(t, featuregates.FeatureGates().Set("HAMivNPUCore=false"))
	driver, _, _ := newMockE2EDriver(t)
	claimUID := "uid-default-mode"
	localShmemDir, err := driver.state.ensureLibvNPUClaimLocalShmemDir(claimUID)
	require.NoError(t, err)

	require.NoError(t, driver.state.Unprepare(claimUID))

	assert.DirExists(t, localShmemDir)
}

func enableHAMivNPUCore(t *testing.T) {
	t.Helper()
	require.NoError(t, featuregates.FeatureGates().Set("HAMivNPUCore=true"))
	t.Cleanup(func() {
		require.NoError(t, featuregates.FeatureGates().Set("HAMivNPUCore=false"))
	})
}

func libvNPUConsumedCapacity(memory string, priority int64) map[resourceapi.QualifiedName]resource.Quantity {
	return map[resourceapi.QualifiedName]resource.Quantity{
		resourceapi.QualifiedName(consts.DeviceCapacityMemory): resource.MustParse(memory),
		resourceapi.QualifiedName(consts.DeviceCapacityCores):  *resource.NewQuantity(priority, resource.DecimalSI),
	}
}

func legacyLibvNPUConsumedCapacity(memory string, priority int64) map[resourceapi.QualifiedName]resource.Quantity {
	return map[resourceapi.QualifiedName]resource.Quantity{
		resourceapi.QualifiedName(DriverDomain + "memory"): resource.MustParse(memory),
		resourceapi.QualifiedName(DriverDomain + "aicore"): *resource.NewQuantity(priority, resource.DecimalSI),
	}
}

func claimSpecContent(t *testing.T, root, claimUID string) string {
	t.Helper()
	var matched string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		require.NoError(t, walkErr)
		if entry.IsDir() {
			return nil
		}
		content, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		if strings.Contains(string(content), claimUID) {
			matched = string(content)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, matched)
	return matched
}

func claimSpec(t *testing.T, root, claimUID string) cdispec.Spec {
	t.Helper()
	var spec cdispec.Spec
	require.NoError(t, yaml.Unmarshal([]byte(claimSpecContent(t, root, claimUID)), &spec))
	return spec
}

func publishedDevices(t *testing.T, resources resourceslice.DriverResources) []resourceapi.Device {
	t.Helper()
	pool, ok := resources.Pools[mockE2ENodeName]
	require.True(t, ok, "expected published resources to contain pool %q", mockE2ENodeName)
	require.Len(t, pool.Slices, 1)
	return pool.Slices[0].Devices
}
