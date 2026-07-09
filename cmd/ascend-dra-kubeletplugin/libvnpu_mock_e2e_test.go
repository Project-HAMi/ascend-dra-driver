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

	"github.com/Project-HAMi/hami-dra-driver/pkg/featuregates"
)

func TestMockDRALibvNPULifecycle(t *testing.T) {
	enableHAMivNPUCore(t)
	driver, publisher, cdiRoot := newMockE2EDriver(t)

	driver.syncAllocatable()
	published := publishedDevices(t, publisher.lastPublished(t))
	require.Len(t, published, 1)
	assert.Equal(t, "npu-0-0", published[0].Name)
	require.NotNil(t, published[0].AllowMultipleAllocations)
	assert.True(t, *published[0].AllowMultipleAllocations)
	require.Contains(t, published[0].Capacity, resourceapi.QualifiedName(DriverDomain+"memory"))
	require.Contains(t, published[0].Capacity, resourceapi.QualifiedName(DriverDomain+"aicore"))

	claim := allocatedClaim("claim-libvnpu", "uid-libvnpu", "npu-0-0", nil)
	claim.Status.Allocation.Devices.Results[0].ConsumedCapacity = libvNPUConsumedCapacity("1024Mi", 50)

	prepared, err := driver.PrepareResourceClaims(context.Background(), []*resourceapi.ResourceClaim{claim})
	require.NoError(t, err)
	require.NoError(t, prepared[claim.UID].Err)
	require.Len(t, prepared[claim.UID].Devices, 1)
	assert.Equal(t, "npu-0-0", prepared[claim.UID].Devices[0].DeviceName)
	assert.Empty(t, driver.state.vnpuManager.PhysicalNPUs)

	spec := claimSpecContent(t, cdiRoot, "uid-libvnpu")
	assert.Contains(t, spec, "ASCEND_VISIBLE_DEVICES=0")
	assert.Contains(t, spec, "NPU_MEM_QUOTA=1024")
	assert.Contains(t, spec, "NPU_PRIORITY=50")
	assert.Contains(t, spec, "NPU_GLOBAL_SHM_PATH=/hami-shared-region/0_global_registry")
	assert.Contains(t, spec, "/hami-vnpu-core")
	assert.Contains(t, spec, "/etc/ld.so.preload")

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
	assert.ElementsMatch(t, []string{"npu-0-0"}, publishedDeviceNames(t, publisher.lastPublished(t)))
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

	_, err := state.applyLibvNPUConfig(results)
	require.ErrorContains(t, err, "capacities must be same across devices")
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

func publishedDevices(t *testing.T, resources resourceslice.DriverResources) []resourceapi.Device {
	t.Helper()
	pool, ok := resources.Pools[mockE2ENodeName]
	require.True(t, ok, "expected published resources to contain pool %q", mockE2ENodeName)
	require.Len(t, pool.Slices, 1)
	return pool.Slices[0].Devices
}
