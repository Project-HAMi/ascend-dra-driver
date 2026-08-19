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

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/Project-HAMi/hami-dra-driver/pkg/consts"
)

// RawDeviceInfo captures extra diagnostic details for each NPU device that do
// not fit into the standard ResourceSlice Device attributes.
type RawDeviceInfo struct {
	LogicID      int32  `json:"logicID"`
	PhyID        int32  `json:"phyID"`
	CardID       int32  `json:"cardID"`
	RawDevType   string `json:"rawDevType"`
	AICore       int64  `json:"aiCore"`
	Memory       int64  `json:"memory"`
	VirtualCount uint32 `json:"virtualCount,omitempty"`
	TemplateName string `json:"templateName,omitempty"`
}

// DiscoveredDevicesResult is the diagnostic output of the tester.
type DiscoveredDevicesResult struct {
	NodeName  string                     `json:"nodeName"`
	Timestamp time.Time                  `json:"timestamp"`
	Raw       []RawDeviceInfo            `json:"raw,omitempty"`
	Slice     *resourceapi.ResourceSlice `json:"resourceSlice"`
}

// fetchAICore attempts to retrieve the total number of AI Cores on the card.
func fetchAICore(mgr *AscendManager) (int, error) {
	aICoreCount, err := mgr.GetChipAICoreCount()
	if err == nil {
		return int(aICoreCount), nil
	}
	return 0, err
}

// fetchMemory attempts to retrieve total memory from the card.
func fetchMemory(mgr *AscendManager) (int, error) {
	memSize, err := mgr.GetChipMem()
	if err == nil {
		return int(memSize), nil
	}
	return 0, err
}

// getDeviceResources returns the maximum AI Core and memory for a device.
// It mirrors the corresponding helper in ascend-dra-kubeletplugin.
func getDeviceResources(mgr *AscendManager) (int, int) {
	aiCores, errCore := fetchAICore(mgr)
	if errCore != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch AI Core count: %v\n", errCore)
	}
	mem, errMem := fetchMemory(mgr)
	if errMem != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch memory size: %v\n", errMem)
	}
	return aiCores, mem
}

// DiscoverNPUDevices enumerates Ascend NPU devices and produces a
// ResourceSlice-like representation plus raw metadata for cross-checking.
func DiscoverNPUDevices(nodeName string) (*DiscoveredDevicesResult, error) {
	mgr, err := NewAscendManager()
	if err != nil {
		return nil, fmt.Errorf("failed to create ascend manager: %w", err)
	}

	allInfo, err := mgr.NewHwDevManager()
	if err != nil {
		return nil, fmt.Errorf("failed to enumerate devices: %w", err)
	}

	aiCores, mem := getDeviceResources(mgr)

	var raw []RawDeviceInfo
	var devices []resourceapi.Device
	seen := make(map[string]bool)

	for _, dev := range allInfo.AllDevs {
		deviceName := fmt.Sprintf("%s%d-0", consts.NPUPrefix, dev.LogicID)
		uuidStr := fmt.Sprintf("%s-%d", nodeName, dev.LogicID)

		devAttributes := map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
			consts.DeviceAttributeIndex:       {IntValue: ptr.To(int64(dev.LogicID))},
			consts.DeviceAttributeUUID:        {StringValue: ptr.To(uuidStr)},
			consts.DeviceAttributeModel:       {StringValue: ptr.To(dev.DevType)},
			consts.DeviceAttributeProductName: {StringValue: ptr.To(dev.DevType)},
			consts.DeviceAttributeBrand:       {StringValue: ptr.To(consts.DeviceBrandHuawei)},
			consts.DeviceAttributeType:        {StringValue: ptr.To(consts.DeviceTypeNPU)},
			consts.DeviceAttributeCores:       {IntValue: ptr.To(int64(aiCores))},
			consts.DeviceAttributeMemory:      {IntValue: ptr.To(int64(mem))},
		}

		device := resourceapi.Device{
			Name:       deviceName,
			Attributes: devAttributes,
		}
		devices = append(devices, device)
		if seen[deviceName] {
			fmt.Fprintf(os.Stderr, "Warning: duplicate device name %s produced by logicID %d (device %s)\n",
				deviceName, dev.LogicID, dev.DeviceName)
		}
		seen[deviceName] = true

		rdi := RawDeviceInfo{
			LogicID:    dev.LogicID,
			PhyID:      dev.PhyID,
			CardID:     dev.CardID,
			RawDevType: dev.DevType,
			AICore:     int64(aiCores),
			Memory:     int64(mem),
		}
		vDevInfos, _ := mgr.getVirtualDevice(dev.LogicID)
		if vDevInfos.TotalResource.VDevNum > 0 {
			rdi.VirtualCount = vDevInfos.TotalResource.VDevNum
		}
		raw = append(raw, rdi)

		fmt.Fprintf(os.Stderr, "Discovered NPU device: %s, Type: NPU, Model: %s\n", deviceName, dev.DevType)
	}

	poolName := fmt.Sprintf("%s-pool-%s", consts.NPUPrefix[:len(consts.NPUPrefix)-1], nodeName)
	resourceSlice := &resourceapi.ResourceSlice{
		TypeMeta: metav1.TypeMeta{
			APIVersion: resourceapi.SchemeGroupVersion.String(),
			Kind:       "ResourceSlice",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: poolName,
			Annotations: map[string]string{
				consts.DriverName + "/generated-by": "ascend-dra-tester",
				consts.DriverName + "/timestamp":    time.Now().UTC().Format(time.RFC3339),
			},
		},
		Spec: resourceapi.ResourceSliceSpec{
			Driver:   consts.DriverName,
			Pool:     resourceapi.ResourcePool{Name: poolName},
			NodeName: &nodeName,
			Devices:  devices,
		},
	}

	if len(raw) > 0 {
		rawJSON, err := json.Marshal(raw)
		if err == nil {
			resourceSlice.Annotations[consts.DriverName+"/raw-devices"] = string(rawJSON)
		}
	}

	return &DiscoveredDevicesResult{
		NodeName:  nodeName,
		Timestamp: time.Now(),
		Raw:       raw,
		Slice:     resourceSlice,
	}, nil
}
