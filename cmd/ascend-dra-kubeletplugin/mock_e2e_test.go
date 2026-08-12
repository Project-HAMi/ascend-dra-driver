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
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/kubernetes/pkg/kubelet/checkpointmanager"

	configapi "github.com/Project-HAMi/hami-dra-driver/api/project-hami.io/resource/npu/v1alpha1"
	"github.com/Project-HAMi/hami-dra-driver/pkg/consts"
)

const mockE2ENodeName = "test-node"

type fakePluginHelper struct {
	published []resourceslice.DriverResources
}

func (f *fakePluginHelper) PublishResources(_ context.Context, resources resourceslice.DriverResources) error {
	f.published = append(f.published, resources)
	return nil
}

func (f *fakePluginHelper) Stop() {}

func (f *fakePluginHelper) lastPublished(t *testing.T) resourceslice.DriverResources {
	t.Helper()
	require.NotEmpty(t, f.published)
	return f.published[len(f.published)-1]
}

func TestMockDRAFullCardLifecycle(t *testing.T) {
	driver, publisher, cdiRoot := newMockE2EDriver(t)

	driver.syncAllocatable()
	assert.ElementsMatch(t, []string{"npu-0-0"}, publishedDeviceNames(t, publisher.lastPublished(t)))

	claim := allocatedClaim("claim-full", "uid-full", "npu-0-0", nil)
	prepared, err := driver.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	require.NoError(t, err)
	require.Contains(t, prepared, claim.UID)
	require.NoError(t, prepared[claim.UID].Err)
	require.Len(t, prepared[claim.UID].Devices, 1)

	device := prepared[claim.UID].Devices[0]
	assert.Equal(t, []string{"npu"}, device.Requests)
	assert.Equal(t, mockE2ENodeName, device.PoolName)
	assert.Equal(t, "npu-0-0", device.DeviceName)
	assert.ElementsMatch(t, []string{
		"k8s.npu.project-hami.io/npu=common",
		"k8s.npu.project-hami.io/npu=uid-full",
	}, device.CDIDeviceIDs)
	assert.True(t, claimSpecContains(t, cdiRoot, "uid-full"))
	assertPreparedClaimExists(t, driver.state, "uid-full")
	assert.ElementsMatch(t, []string{"npu-0-0"}, publishedDeviceNames(t, publisher.lastPublished(t)))

	preparedAgain, err := driver.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	require.NoError(t, err)
	require.NoError(t, preparedAgain[claim.UID].Err)
	assert.Equal(t, prepared[claim.UID].Devices, preparedAgain[claim.UID].Devices)

	unprepared, err := driver.UnprepareResourceClaims(context.Background(), []kubeletplugin.NamespacedObject{
		{
			NamespacedName: types.NamespacedName{Namespace: "default", Name: "claim-full"},
			UID:            claim.UID,
		},
	})
	require.NoError(t, err)
	require.Contains(t, unprepared, claim.UID)
	require.NoError(t, unprepared[claim.UID])
	assert.False(t, claimSpecContains(t, cdiRoot, "uid-full"))
	assertPreparedClaimMissing(t, driver.state, "uid-full")
	assert.ElementsMatch(t, []string{"npu-0-0"}, publishedDeviceNames(t, publisher.lastPublished(t)))

	unpreparedAgain, err := driver.UnprepareResourceClaims(context.Background(), []kubeletplugin.NamespacedObject{
		{
			NamespacedName: types.NamespacedName{Namespace: "default", Name: "claim-full"},
			UID:            claim.UID,
		},
	})
	require.NoError(t, err)
	require.NoError(t, unpreparedAgain[claim.UID])
}

func TestMockDRAVNPULifecyclePublishesRemainingSlice(t *testing.T) {
	driver, publisher, _ := newMockE2EDriver(t)

	driver.syncAllocatable()
	assert.ElementsMatch(t, []string{"npu-0-0"}, publishedDeviceNames(t, publisher.lastPublished(t)))

	claim := allocatedClaim("claim-vnpu", "uid-vnpu", "npu-0-0", []resourceapi.DeviceAllocationConfiguration{
		classNpuConfig(t, "vir01"),
	})
	prepared, err := driver.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	require.NoError(t, err)
	require.NoError(t, prepared[claim.UID].Err)
	require.Len(t, prepared[claim.UID].Devices, 1)
	assert.Equal(t, "npu-0-0", prepared[claim.UID].Devices[0].DeviceName)

	physical := driver.state.vnpuManager.PhysicalNPUs["npu-0-0"]
	require.NotNil(t, physical)
	require.Len(t, physical.AllocatedSlices, 1)
	assert.Equal(t, "vir01", physical.AllocatedSlices[0].TemplateName)
	assert.ElementsMatch(t, []string{"npu-0-0", "npu-0-1"}, publishedDeviceNames(t, publisher.lastPublished(t)))

	unprepared, err := driver.UnprepareResourceClaims(context.Background(), []kubeletplugin.NamespacedObject{
		{
			NamespacedName: types.NamespacedName{Namespace: "default", Name: "claim-vnpu"},
			UID:            claim.UID,
		},
	})
	require.NoError(t, err)
	require.NoError(t, unprepared[claim.UID])
	assert.Empty(t, physical.AllocatedSlices)
	assert.ElementsMatch(t, []string{"npu-0-0"}, publishedDeviceNames(t, publisher.lastPublished(t)))
}

func TestMockDRAPrepareRejectsUnallocatedClaim(t *testing.T) {
	driver, _, cdiRoot := newMockE2EDriver(t)

	claim := &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "claim-unallocated",
			UID:       types.UID("uid-unallocated"),
		},
	}
	prepared, err := driver.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	require.NoError(t, err)
	require.Contains(t, prepared, claim.UID)
	require.ErrorContains(t, prepared[claim.UID].Err, "claim not yet allocated")
	assert.False(t, claimSpecContains(t, cdiRoot, "uid-unallocated"))
	assertPreparedClaimMissing(t, driver.state, "uid-unallocated")
}

func newMockE2EDriver(t *testing.T) (*driver, *fakePluginHelper, string) {
	t.Helper()
	t.Setenv("NODE_NAME", mockE2ENodeName)

	vnpuManager := &VNPUManager{
		PhysicalNPUs: make(map[string]*PhysicalNPUState),
		Templates:    createDefaultTemplates(),
	}
	allocatable, err := enumerateDevices(newStubManager(), vnpuManager, mockE2ENodeName)
	require.NoError(t, err)

	pluginRoot := t.TempDir()
	cdiRoot := t.TempDir()
	config := &Config{
		flags: &Flags{
			cdiRoot:                     cdiRoot,
			kubeletPluginsDirectoryPath: pluginRoot,
		},
	}
	require.NoError(t, os.MkdirAll(config.DriverPluginPath(), 0750))

	cdi, err := NewCDIHandler(config)
	require.NoError(t, err)
	require.NoError(t, cdi.CreateCommonSpecFile())

	cpManager, err := checkpointmanager.NewCheckpointManager(config.DriverPluginPath())
	require.NoError(t, err)
	require.NoError(t, cpManager.CreateCheckpoint(DriverPluginCheckpointFile, newCheckpoint()))

	state := &DeviceState{
		cdi:               cdi,
		allocatable:       allocatable,
		checkpointManager: cpManager,
		vnpuManager:       vnpuManager,
		libvNPUHostPath:   filepath.Join(t.TempDir(), "hami-vnpu-core"),
	}
	vnpuManager.SetDeviceUpdateCallback(func(deviceName string, physicalNpu *PhysicalNPUState) {
		state.UpdateAllocatableDevice(deviceName, physicalNpu)
	})

	publisher := &fakePluginHelper{}
	driver := &driver{
		helper:   publisher,
		state:    state,
		nodeName: mockE2ENodeName,
	}
	return driver, publisher, cdiRoot
}

func allocatedClaim(name, uid, deviceName string, configs []resourceapi.DeviceAllocationConfiguration) *resourceapi.ResourceClaim {
	return &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
			UID:       types.UID(uid),
		},
		Status: resourceapi.ResourceClaimStatus{
			Allocation: &resourceapi.AllocationResult{
				Devices: resourceapi.DeviceAllocationResult{
					Results: []resourceapi.DeviceRequestAllocationResult{
						{
							Request: "npu",
							Driver:  consts.DriverName,
							Pool:    mockE2ENodeName,
							Device:  deviceName,
						},
					},
					Config: configs,
				},
			},
		},
	}
}

func classNpuConfig(t *testing.T, templateName string) resourceapi.DeviceAllocationConfiguration {
	t.Helper()
	raw, err := json.Marshal(&configapi.NpuConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: configapi.GroupName + "/" + configapi.Version,
			Kind:       configapi.NpuConfigKind,
		},
		VNPUSpec: &configapi.VNPUSpec{TemplateName: templateName},
	})
	require.NoError(t, err)
	return resourceapi.DeviceAllocationConfiguration{
		Source: resourceapi.AllocationConfigSourceClass,
		DeviceConfiguration: resourceapi.DeviceConfiguration{
			Opaque: &resourceapi.OpaqueDeviceConfiguration{
				Driver: consts.DriverName,
				Parameters: runtime.RawExtension{
					Raw: raw,
				},
			},
		},
	}
}

func publishedDeviceNames(t *testing.T, resources resourceslice.DriverResources) []string {
	t.Helper()
	pool, ok := resources.Pools[mockE2ENodeName]
	require.True(t, ok, "expected published resources to contain pool %q", mockE2ENodeName)
	require.Len(t, pool.Slices, 1)

	names := make([]string, 0, len(pool.Slices[0].Devices))
	for _, device := range pool.Slices[0].Devices {
		names = append(names, device.Name)
	}
	return names
}

func claimSpecContains(t *testing.T, root, claimUID string) bool {
	t.Helper()
	found := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		require.NoError(t, walkErr)
		if entry.IsDir() {
			return nil
		}
		content, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		if strings.Contains(string(content), claimUID) {
			found = true
		}
		return nil
	})
	require.NoError(t, err)
	return found
}

func assertPreparedClaimExists(t *testing.T, state *DeviceState, claimUID string) {
	t.Helper()
	checkpoint := newCheckpoint()
	require.NoError(t, state.checkpointManager.GetCheckpoint(DriverPluginCheckpointFile, checkpoint))
	require.Contains(t, checkpoint.V1.PreparedClaims, claimUID)
}

func assertPreparedClaimMissing(t *testing.T, state *DeviceState, claimUID string) {
	t.Helper()
	checkpoint := newCheckpoint()
	require.NoError(t, state.checkpointManager.GetCheckpoint(DriverPluginCheckpointFile, checkpoint))
	require.NotContains(t, checkpoint.V1.PreparedClaims, claimUID)
}
