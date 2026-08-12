/*
 * Copyright 2025 The HAMi Authors.
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

// ascend-dra-tester is a standalone diagnostic tool that mirrors the device
// discovery path of ascend-dra-kubeletplugin and prints the discovered NPU
// devices as a Kubernetes ResourceSlice for manual verification.

package main

import (
	"fmt"
	"strconv"
	"strings"

	"ascend-common/devmanager"
	npuCommon "ascend-common/devmanager/common"

	"github.com/Project-HAMi/hami-dra-driver/pkg/common"
)

// Device mirrors the per-device view used internally by the kubelet plugin.
type Device struct {
	UUID     string
	LogicID  int32
	PhyID    int32
	CardID   int32
	DeviceID int32
	Memory   int64
	AICore   int32
	Health   bool
}

// AscendManager wraps the Huawei devmanager DCMI client.
type AscendManager struct {
	mgr  devmanager.DeviceInterface
	devs []*Device
}

// NewAscendManager initializes the DCMI device manager.
// It uses the same AutoInit("") call as the kubelet plugin.
func NewAscendManager() (*AscendManager, error) {
	mgr, err := devmanager.AutoInit("", 0)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ascend device manager: %w", err)
	}
	return &AscendManager{
		mgr:  mgr,
		devs: []*Device{},
	}, nil
}

func (am *AscendManager) getAICoreCount(cgoVDevInfo npuCommon.VirtualDevInfo) (int32, error) {
	chipAICore := cgoVDevInfo.TotalResource.Computing.Aic
	if chipAICore < common.MinAICoreNum || chipAICore > common.MaxAICoreNum {
		return 0, fmt.Errorf("invalid ai core num %f", chipAICore)
	}
	return int32(chipAICore), nil
}

func (am *AscendManager) getMemorySize(cgoVDevInfo npuCommon.VirtualDevInfo) (int32, error) {
	memorySize := cgoVDevInfo.TotalResource.Computing.MemorySize
	memorySize = memorySize / 1024
	if memorySize <= 0 || memorySize > 1024 {
		return 0, fmt.Errorf("invalid memory size %d", memorySize)
	}
	return int32(memorySize), nil
}

// GetChipMem fetches the total memory (in GB) from the first available logic ID.
func (am *AscendManager) GetChipMem() (int32, error) {
	_, logicIDs, err := am.mgr.GetDeviceList()
	if err != nil {
		return 0, err
	}
	if len(logicIDs) < 1 {
		return 0, fmt.Errorf("not found logicIDs")
	}
	cgoVDevInfo, err := am.mgr.GetVirtualDeviceInfo(logicIDs[0])
	if err != nil && strings.Contains(err.Error(), strconv.Itoa(common.DeviceNotSupport)) {
		return common.DeviceNotSupport, nil
	}
	if err != nil {
		return 32, nil
	}
	return am.getMemorySize(cgoVDevInfo)
}

// GetChipAICoreCount fetches the total AI Core count from the first available logic ID.
func (am *AscendManager) GetChipAICoreCount() (int32, error) {
	_, logicIDs, err := am.mgr.GetDeviceList()
	if err != nil {
		return 0, err
	}
	if len(logicIDs) < 1 {
		return 0, fmt.Errorf("not found logicIDs")
	}
	cgoVDevInfo, err := am.mgr.GetVirtualDeviceInfo(logicIDs[0])
	if err != nil && strings.Contains(err.Error(), strconv.Itoa(common.DeviceNotSupport)) {
		return common.DeviceNotSupport, nil
	}
	if err != nil {
		return common.DefaultAICoreNum, nil
	}
	return am.getAICoreCount(cgoVDevInfo)
}

func (am *AscendManager) getDavinciDev(logicID int32) (common.DavinciDev, error) {
	phyID, err := am.mgr.GetPhysicIDFromLogicID(logicID)
	if err != nil {
		return common.DavinciDev{}, err
	}
	cardID, _, err := am.mgr.GetCardIDDeviceID(logicID)
	if err != nil {
		return common.DavinciDev{}, err
	}
	return common.DavinciDev{
		LogicID: logicID,
		PhyID:   phyID,
		CardID:  cardID,
	}, nil
}

func (am *AscendManager) getVirtualDevice(logicID int32) (npuCommon.VirtualDevInfo, error) {
	virtualDevInfos, err := am.mgr.GetVirtualDeviceInfo(logicID)
	if err != nil {
		return npuCommon.VirtualDevInfo{}, fmt.Errorf("query virtual device info failure: %s", err)
	}
	return virtualDevInfos, nil
}

func (am *AscendManager) assemblePhyDevices(devType string, davinciDev common.DavinciDev,
	devices *[]common.NPUDevice,
) {
	deviceName := fmt.Sprintf("%s-%d", devType, davinciDev.PhyID)
	device := am.assembleNPUDeviceStruct(devType, deviceName, davinciDev)
	*devices = append(*devices, device)
}

func (am *AscendManager) assembleNPUDeviceStruct(deviType, deviceName string,
	davinciDev common.DavinciDev) common.NPUDevice {

	return common.NPUDevice{
		DevType:    deviType,
		DeviceName: deviceName,
		LogicID:    davinciDev.LogicID,
		PhyID:      davinciDev.PhyID,
		CardID:     davinciDev.CardID,
	}
}

func (am *AscendManager) assembleVirtualDevices(chipType string, davinciDev common.DavinciDev,
	vDevInfos npuCommon.VirtualDevInfo,
	devices *[]common.NPUDevice) {
	for _, subVDevInfo := range vDevInfos.VDevInfo {
		vDeviType, deviceName, err := am.assembleSpecVirtualDevice(chipType, davinciDev.PhyID, subVDevInfo)
		if err != nil {
			continue
		}
		device := am.assembleNPUDeviceStruct(vDeviType, deviceName, davinciDev)
		*devices = append(*devices, device)
	}
}

func (am *AscendManager) assembleSpecVirtualDevice(chipType string, phyID int32,
	vDevInfo npuCommon.CgoVDevQueryStru) (string,
	string, error) {
	coreNum := int32(vDevInfo.QueryInfo.Computing.Aic)
	if coreNum <= 0 {
		return "", "", fmt.Errorf("invalid vdev info, ai core is 0")
	}
	vDeviType, exist := common.GetTemplateName2DeviceTypeMap()[vDevInfo.QueryInfo.Name]
	if !exist {
		return "", "", fmt.Errorf("check templatename failed, templatename is %s", vDevInfo.QueryInfo.Name)
	}
	vDeviType = fmt.Sprintf("%s-%s", chipType, vDeviType)
	devID := fmt.Sprintf("%s-%d-%d", vDeviType, vDevInfo.VDevID, phyID)
	return vDeviType, devID, nil
}

// NewHwDevManager enumerates all physical and virtual NPU devices.
// This replicates the discovery implemented in ascend-dra-kubeletplugin.
func (am *AscendManager) NewHwDevManager() (common.NPUAllInfo, error) {
	devNum, devList, err := am.mgr.GetDeviceList()
	if err != nil {
		return common.NPUAllInfo{}, err
	}
	var allDevices []common.NPUDevice
	var chipType = ""
	for i := int32(0); i < devNum; i++ {
		davinciDev, err := am.getDavinciDev(devList[i])
		if err != nil {
			return common.NPUAllInfo{}, err
		}
		if chipType == "" {
			chipInfo, err := am.mgr.GetChipInfo(davinciDev.LogicID)
			if err != nil {
				return common.NPUAllInfo{}, nil
			}
			chipType = chipInfo.Name
		}
		vDevInfos, err := am.getVirtualDevice(devList[i])
		if err != nil {
			am.assemblePhyDevices(chipType, davinciDev, &allDevices)
			continue
		}
		if vDevInfos.TotalResource.VDevNum > common.MaxVirtualDeviceNum {
			return common.NPUAllInfo{}, fmt.Errorf("invalid virtual device count")
		}
		if vDevInfos.TotalResource.VDevNum == 0 {
			am.assemblePhyDevices(chipType, davinciDev, &allDevices)
			continue
		}
		am.assembleVirtualDevices(chipType, davinciDev, vDevInfos, &allDevices)
	}
	return common.NPUAllInfo{AllDevs: allDevices}, nil
}
