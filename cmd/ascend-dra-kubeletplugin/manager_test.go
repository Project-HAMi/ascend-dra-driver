/*
 * Copyright 2024 The HAMi Authors.
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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Project-HAMi/hami-dra-driver/pkg/common"

	"ascend-common/devmanager"
	npuCommon "ascend-common/devmanager/common"
)

// stubDeviceManager embeds the upstream mock and overrides only the methods
// required by the production code under test. This keeps the test fixture
// small while still satisfying the devmanager.DeviceInterface contract.
type stubDeviceManager struct {
	*devmanager.DeviceManagerMock

	deviceList       []int32
	virtualDeviceMap map[int32]npuCommon.VirtualDevInfo
	physicIDMap      map[int32]int32
	cardDeviceIDMap  map[int32][2]int32
	chipMap          map[int32]*npuCommon.ChipInfo
}

func (s *stubDeviceManager) GetDeviceList() (int32, []int32, error) {
	return int32(len(s.deviceList)), s.deviceList, nil
}

func (s *stubDeviceManager) GetPhysicIDFromLogicID(logicID int32) (int32, error) {
	if id, ok := s.physicIDMap[logicID]; ok {
		return id, nil
	}
	return logicID, nil
}

func (s *stubDeviceManager) GetCardIDDeviceID(logicID int32) (int32, int32, error) {
	if ids, ok := s.cardDeviceIDMap[logicID]; ok {
		return ids[0], ids[1], nil
	}
	return 0, 0, nil
}

func (s *stubDeviceManager) GetChipInfo(logicID int32) (*npuCommon.ChipInfo, error) {
	if chip, ok := s.chipMap[logicID]; ok {
		return chip, nil
	}
	return &npuCommon.ChipInfo{Name: "Ascend910A"}, nil
}

func (s *stubDeviceManager) GetVirtualDeviceInfo(logicID int32) (npuCommon.VirtualDevInfo, error) {
	if vdev, ok := s.virtualDeviceMap[logicID]; ok {
		return vdev, nil
	}
	return npuCommon.VirtualDevInfo{}, nil
}

func newStubManager() *AscendManager {
	return &AscendManager{
		mgr: &stubDeviceManager{
			DeviceManagerMock: &devmanager.DeviceManagerMock{},
			deviceList:        []int32{0},
			physicIDMap:       map[int32]int32{0: 0},
			chipMap: map[int32]*npuCommon.ChipInfo{
				0: {Name: "Ascend910A"},
			},
		},
		devs: []*Device{},
	}
}

func TestAssembleNPUDeviceStruct(t *testing.T) {
	am := &AscendManager{}
	dev := am.assembleNPUDeviceStruct("Ascend910A", "Ascend910A-0", common.DavinciDev{
		LogicID: 1,
		PhyID:   2,
		CardID:  3,
	})

	assert.Equal(t, "Ascend910A", dev.DevType)
	assert.Equal(t, "Ascend910A-0", dev.DeviceName)
	assert.Equal(t, int32(1), dev.LogicID)
	assert.Equal(t, int32(2), dev.PhyID)
	assert.Equal(t, int32(3), dev.CardID)
}

func TestAssembleSpecVirtualDevice(t *testing.T) {
	am := &AscendManager{}
	vdev := npuCommon.CgoVDevQueryStru{
		VDevID: 7,
		QueryInfo: npuCommon.CgoVDevQueryInfo{
			Name: "vir01",
			Computing: npuCommon.CgoComputingResource{
				Aic: 1,
			},
		},
	}

	devType, devName, err := am.assembleSpecVirtualDevice("Ascend310P", 3, vdev)
	assert.NoError(t, err)
	assert.Equal(t, "Ascend310P-Ascend310P-1c", devType)
	assert.Equal(t, "Ascend310P-Ascend310P-1c-7-3", devName)
}

func TestAssembleSpecVirtualDeviceInvalidAicore(t *testing.T) {
	am := &AscendManager{}
	vdev := npuCommon.CgoVDevQueryStru{
		VDevID: 7,
		QueryInfo: npuCommon.CgoVDevQueryInfo{
			Name:      "vir01",
			Computing: npuCommon.CgoComputingResource{Aic: 0},
		},
	}

	_, _, err := am.assembleSpecVirtualDevice("Ascend310P", 3, vdev)
	assert.Error(t, err)
}

func TestNewHwDevManagerPhysicalDevice(t *testing.T) {
	am := newStubManager()

	info, err := am.NewHwDevManager()
	assert.NoError(t, err)
	assert.Len(t, info.AllDevs, 1)
	assert.Equal(t, "Ascend910A-0", info.AllDevs[0].DeviceName)
	assert.Equal(t, "Ascend910A", info.AllDevs[0].DevType)
}

func TestEnumerateDevicesPublishesDiscoveredPhysicalID(t *testing.T) {
	am := newStubManager()
	stub := am.mgr.(*stubDeviceManager)
	stub.deviceList = []int32{0, 1}
	stub.physicIDMap = map[int32]int32{0: 0, 1: 2}
	stub.chipMap[1] = &npuCommon.ChipInfo{Name: "Ascend910A"}

	devices, err := enumerateDevices(am, nil, "test-node")
	require.NoError(t, err)
	require.Contains(t, devices, "npu-1-0")
	physicalID, ok := devices["npu-1-0"].Attributes[physicalIDAttributeName]
	require.True(t, ok)
	require.NotNil(t, physicalID.IntValue)
	assert.Equal(t, int64(2), *physicalID.IntValue)
}

func TestNewHwDevManagerVirtualDevice(t *testing.T) {
	am := newStubManager()
	stub := am.mgr.(*stubDeviceManager)
	stub.virtualDeviceMap = map[int32]npuCommon.VirtualDevInfo{
		0: {
			TotalResource: npuCommon.CgoSocTotalResource{
				VDevNum: 1,
			},
			VDevInfo: []npuCommon.CgoVDevQueryStru{
				{
					VDevID: 5,
					QueryInfo: npuCommon.CgoVDevQueryInfo{
						Name: "vir01",
						Computing: npuCommon.CgoComputingResource{
							Aic: 1,
						},
					},
				},
			},
		},
	}

	info, err := am.NewHwDevManager()
	assert.NoError(t, err)
	assert.Len(t, info.AllDevs, 1)
	assert.Equal(t, "Ascend910A-Ascend310P-1c-5-0", info.AllDevs[0].DeviceName)
}
