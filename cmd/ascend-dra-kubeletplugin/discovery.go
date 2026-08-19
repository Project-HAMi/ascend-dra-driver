package main

import (
	"fmt"
	"log"
	"os"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	"github.com/Project-HAMi/hami-dra-driver/pkg/consts"
	"github.com/Project-HAMi/hami-dra-driver/pkg/featuregates"
)

// fetchAICore attempts to retrieve the total number of AI Cores on the card.
func fetchAICore(mgr *AscendManager) (int, error) {
	aICoreCount, err := mgr.GetChipAICoreCount()
	if err == nil {
		return int(aICoreCount), nil
	}
	return 0, err
}

// fetchMemory attempts to retrieve total memory from the card.
func fetchMemory(hdm *AscendManager) (int, error) {
	memSize, err := hdm.GetChipMem()
	if err == nil {
		return int(memSize), nil
	}
	return 0, err
}

func fetchDeviceMemoryMiB(mgr *AscendManager, logicID int32) (int64, error) {
	memInfo, err := mgr.GetDeviceMemoryInfo(logicID)
	if err == nil && memInfo != nil && memInfo.MemorySize > 0 {
		return int64(memInfo.MemorySize), nil
	}

	memGiB, fallbackErr := fetchMemory(mgr)
	if fallbackErr != nil {
		if err != nil {
			return 0, err
		}
		return 0, fallbackErr
	}
	if memGiB <= 0 {
		return 0, fmt.Errorf("invalid memory size %dGiB", memGiB)
	}
	return int64(memGiB) * 1024, nil
}

// getDeviceResources returns the maximum AI Core and memory for a device
// depending on whether it has been split into vNPUs or not.
func getDeviceResources(mgr *AscendManager, devType string, vnpuManager *VNPUManager, deviceName string) (int, int) {
	if vnpuManager == nil {
		return 0, 0
	}
	physicalNpu := vnpuManager.PhysicalNPUs[deviceName]
	if physicalNpu == nil {
		return 0, 0
	}

	// If the device has not been split yet, return the full card resources
	if len(physicalNpu.AllocatedSlices) == 0 {
		aiCores, errCore := fetchAICore(mgr)
		if errCore != nil {
			log.Printf("Failed to fetch AI Core count: %v", errCore)
		}
		mem, errMem := fetchMemory(mgr)
		if errMem != nil {
			log.Printf("Failed to fetch memory size: %v", errMem)
		}
		return aiCores, mem
	}

	// If the device has already been split, find the largest remaining
	// AI Core and memory values from the available templates
	maxAICore, maxMemory := 0, 0
	for _, tpl := range physicalNpu.SupportTemplates {
		if tpl.Attributes.AICORE > maxAICore {
			maxAICore = tpl.Attributes.AICORE
		}
		if tpl.Attributes.Memory > maxMemory {
			maxMemory = tpl.Attributes.Memory
		}
	}
	return maxAICore, maxMemory
}

// enumerateAllPossibleDevices initializes the devmanager, creates a vNPU manager if possible,
// and enumerates all possible devices to produce an AllocatableDevices map.
func enumerateAllPossibleDevices() (AllocatableDevices, *VNPUManager, error) {
	mgr, err := NewAscendManager()
	if err != nil {
		return nil, nil, err
	}

	var vnpuManager *VNPUManager
	if featuregates.Enabled(featuregates.HAMivNPUCore) {
		runner, err := newNPUSmiDeviceShareRunner()
		if err != nil {
			return nil, nil, fmt.Errorf("initialize device-share: %w", err)
		}
		if err := enableHAMivNPUDeviceShare(mgr, runner); err != nil {
			return nil, nil, fmt.Errorf("initialize device-share: %w", err)
		}
	} else {
		vnpuManager, err = NewVNPUManager()
		if err != nil {
			log.Printf("Failed to initialize vNPU manager: %v. Only full-card allocation is supported.", err)
			vnpuManager = nil
		}
	}

	alldevices, err := enumerateDevices(mgr, vnpuManager, os.Getenv("NODE_NAME"))
	if err != nil {
		return nil, nil, err
	}
	return alldevices, vnpuManager, nil
}

func enumerateDevices(mgr *AscendManager, vnpuManager *VNPUManager, nodeName string) (AllocatableDevices, error) {
	allInfo, err := mgr.NewHwDevManager()
	if err != nil {
		return nil, err
	}

	hamiVNPUCoreEnabled := featuregates.Enabled(featuregates.HAMivNPUCore)
	deviceType := consts.DeviceTypeNPU
	if hamiVNPUCoreEnabled {
		deviceType = consts.DeviceTypeHAMivNPUCore
	}

	alldevices := make(AllocatableDevices)
	for _, dev := range allInfo.AllDevs {
		deviceName := fmt.Sprintf("%s%d-0", consts.NPUPrefix, dev.LogicID)
		uuidStr := fmt.Sprintf("%s-%d", nodeName, dev.LogicID)

		devAttributes := map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
			consts.DeviceAttributeIndex:       {IntValue: ptr.To(int64(dev.LogicID))},
			physicalIDAttributeName:           {IntValue: ptr.To(int64(dev.PhyID))},
			consts.DeviceAttributeUUID:        {StringValue: ptr.To(uuidStr)},
			consts.DeviceAttributeModel:       {StringValue: ptr.To(dev.DevType)},
			consts.DeviceAttributeProductName: {StringValue: ptr.To(dev.DevType)},
			consts.DeviceAttributeBrand:       {StringValue: ptr.To(consts.DeviceBrandHuawei)},
			consts.DeviceAttributeType:        {StringValue: ptr.To(deviceType)},
		}

		var capacities map[resourceapi.QualifiedName]resourceapi.DeviceCapacity
		if hamiVNPUCoreEnabled {
			capacities = buildLibvNPUDeviceCapacities(mgr, dev.LogicID)
		} else if vnpuManager != nil {
			vnpuManager.InitPhysicalNPU(deviceName, dev.LogicID, dev.PhyID, dev.DevType)
			maxAICore, maxMemory := getDeviceResources(mgr, dev.DevType, vnpuManager, deviceName)
			devAttributes[consts.DeviceAttributeCores] = resourceapi.DeviceAttribute{IntValue: ptr.To(int64(maxAICore))}
			devAttributes[consts.DeviceAttributeMemory] = resourceapi.DeviceAttribute{IntValue: ptr.To(int64(maxMemory))}
		}

		device := resourceapi.Device{
			Name:       deviceName,
			Attributes: devAttributes,
			Capacity:   capacities,
		}
		if hamiVNPUCoreEnabled {
			device.AllowMultipleAllocations = ptr.To(true)
		}
		alldevices[device.Name] = device
		log.Printf("Discovered NPU device: %s, Type: NPU, Model: %s", deviceName, dev.DevType)
	}
	return alldevices, nil
}

func buildLibvNPUDeviceCapacities(mgr *AscendManager, logicID int32) map[resourceapi.QualifiedName]resourceapi.DeviceCapacity {
	memoryMiB, err := fetchDeviceMemoryMiB(mgr, logicID)
	if err != nil {
		log.Printf("Failed to fetch device memory for logic ID %d: %v; using 32Gi fallback", logicID, err)
		memoryMiB = 32 * 1024
	}

	memValue := *resource.NewQuantity(memoryMiB*1024*1024, resource.BinarySI)
	memStep := resource.MustParse("1Mi")
	aicoreValue := *resource.NewQuantity(100, resource.DecimalSI)
	aicoreMin := *resource.NewQuantity(0, resource.DecimalSI)
	aicoreStep := *resource.NewQuantity(1, resource.DecimalSI)

	return map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
		consts.DeviceCapacityMemory: {
			Value: memValue,
			RequestPolicy: ptr.To(resourceapi.CapacityRequestPolicy{
				Default: ptr.To(memValue),
				ValidRange: ptr.To(resourceapi.CapacityRequestPolicyRange{
					Min:  ptr.To(memStep),
					Max:  ptr.To(memValue),
					Step: ptr.To(memStep),
				}),
			}),
		},
		consts.DeviceCapacityCores: {
			Value: aicoreValue,
			RequestPolicy: ptr.To(resourceapi.CapacityRequestPolicy{
				Default: ptr.To(aicoreValue),
				ValidRange: ptr.To(resourceapi.CapacityRequestPolicyRange{
					Min:  ptr.To(aicoreMin),
					Max:  ptr.To(aicoreValue),
					Step: ptr.To(aicoreStep),
				}),
			}),
		},
	}
}
