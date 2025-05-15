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
	"Ascend-dra-driver/pkg/common"
	"fmt"
	"strconv"
	"strings"

	"huawei.com/npu-exporter/v5/devmanager"
	npuCommon "huawei.com/npu-exporter/v5/devmanager/common"
)

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

type AscendManager struct {
	mgr *devmanager.DeviceManager
	//nodeName string
	devs []*Device
}

func NewAscendManager() (*AscendManager, error) {
	mgr, err := devmanager.AutoInit("")
	if err != nil {
		return nil, err
	}
	return &AscendManager{
		mgr:  mgr,
		devs: []*Device{},
	}, nil
}

func (am *AscendManager) getAiCoreCount(cgoVDevInfo npuCommon.VirtualDevInfo) (int32, error) {
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

// GetChipMem get chip memory size
func (am *AscendManager) GetChipMem() (int32, error) {
	_, logicIDs, err := am.mgr.GetDeviceList()
	if err != nil {
		return 0, err
	}
	if len(logicIDs) < 1 {
		return 0, fmt.Errorf("not found logicIDs")
	}
	for _, logicID := range logicIDs {
		cgoVDevInfo, err := am.mgr.GetVirtualDeviceInfo(logicID)
		if err != nil && strings.Contains(err.Error(), strconv.Itoa(common.DeviceNotSupport)) {
			return common.DeviceNotSupport, nil
		}
		if err != nil {
			// if not support found memory size, setting a default value
			return 32, nil
		}
		return am.getMemorySize(cgoVDevInfo)
	}
	return 0, fmt.Errorf("not get memory size")
}

// GetChipAiCoreCount get chip aicore count
func (am *AscendManager) GetChipAiCoreCount() (int32, error) {
	_, logicIDs, err := am.mgr.GetDeviceList()
	if err != nil {
		return 0, err
	}
	if len(logicIDs) < 1 {
		return 0, fmt.Errorf("not found logicIDs")
	}
	for _, logicID := range logicIDs {
		cgoVDevInfo, err := am.mgr.GetVirtualDeviceInfo(logicID)
		if err != nil && strings.Contains(err.Error(), strconv.Itoa(common.DeviceNotSupport)) {
			return common.DeviceNotSupport, nil
		}
		if err != nil {
			// if not support found aicore number, setting a default value

			return common.DefaultAiCoreNum, nil
		}
		return am.getAiCoreCount(cgoVDevInfo)
	}
	return 0, fmt.Errorf("not get aicore count")
}

func (am *AscendManager) getDavinCiDev(logicID int32) (common.DavinCiDev, error) {
	phyID, err := am.mgr.GetPhysicIDFromLogicID(logicID)
	if err != nil {
		return common.DavinCiDev{}, err
	}
	cardID, _, err := am.mgr.GetCardIDDeviceID(logicID)
	if err != nil {
		return common.DavinCiDev{}, err
	}
	return common.DavinCiDev{
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

func (am *AscendManager) assemblePhyDevices(devType string, davinCiDev common.DavinCiDev,
	devices *[]common.NpuDevice,
) {
	deviceName := fmt.Sprintf("%s-%d", devType, davinCiDev.PhyID)
	device := am.assembleNpuDeviceStruct(devType, deviceName, davinCiDev)
	*devices = append(*devices, device)
}

func (am *AscendManager) assembleNpuDeviceStruct(deviType, deviceName string,
	davinCiDev common.DavinCiDev) common.NpuDevice {

	return common.NpuDevice{
		DevType:    deviType,
		DeviceName: deviceName,
		LogicID:    davinCiDev.LogicID,
		PhyID:      davinCiDev.PhyID,
		CardID:     davinCiDev.CardID,
	}
}

func (am *AscendManager) assembleVirtualDevices(chipType string, davinCiDev common.DavinCiDev,
	vDevInfos npuCommon.VirtualDevInfo,
	devices *[]common.NpuDevice) {
	for _, subVDevInfo := range vDevInfos.VDevInfo {
		vDeviType, deviceName, err := am.assembleSpecVirtualDevice(chipType, davinCiDev.PhyID, subVDevInfo)
		if err != nil {
			continue
		}
		device := am.assembleNpuDeviceStruct(vDeviType, deviceName, davinCiDev)
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

func (am *AscendManager) NewHwDevManager() (common.NpuAllInfo, error) {
	devNum, devList, err := am.mgr.GetDeviceList()
	if err != nil {
		return common.NpuAllInfo{}, err
	}
	var allDevices []common.NpuDevice
	var chipType = ""
	for i := int32(0); i < devNum; i++ {
		davinCiDev, err := am.getDavinCiDev(devList[i])
		if err != nil {
			return common.NpuAllInfo{}, err
		}
		if chipType == "" {
			chipInfo, err := am.mgr.GetChipInfo(davinCiDev.LogicID)
			if err != nil {
				return common.NpuAllInfo{}, nil
			}
			chipType = chipInfo.Name
		}
		vDevInfos, err := am.getVirtualDevice(devList[i])
		if err != nil {

		}
		if vDevInfos.TotalResource.VDevNum > common.MaxVirtualDeviceNum {
			return common.NpuAllInfo{}, fmt.Errorf("invalid virtual device count")
		}
		if vDevInfos.TotalResource.VDevNum == 0 {
			am.assemblePhyDevices(chipType, davinCiDev, &allDevices)
			continue
		}
		am.assembleVirtualDevices(chipType, davinCiDev, vDevInfos, &allDevices)
	}
	return common.NpuAllInfo{AllDevs: allDevices}, nil
}
