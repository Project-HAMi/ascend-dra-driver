package main

import (
	"fmt"
	"log"
	"os"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/utils/ptr"

	"github.com/Project-HAMi/hami-dra-driver/pkg/consts"
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
	allInfo, _ := mgr.NewHwDevManager()
	vnpuManager, err := NewVNPUManager()
	if err != nil {
		log.Printf("Failed to initialize vNPU manager: %v. Only full-card allocation is supported.", err)
	}

	alldevices := make(AllocatableDevices)
	for _, dev := range allInfo.AllDevs {
		deviceName := fmt.Sprintf("%s%d-0", consts.NPUPrefix, dev.LogicID)
		uuidStr := fmt.Sprintf("%s-%d", os.Getenv("NODE_NAME"), dev.LogicID)

		devAttributes := map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
			DriverDomain + "index": {IntValue: ptr.To(int64(dev.LogicID))},
			DriverDomain + "uuid":  {StringValue: ptr.To(uuidStr)},
			DriverDomain + "model": {StringValue: ptr.To(dev.DevType)},
			DriverDomain + "type":  {StringValue: ptr.To("NPU")},
		}

		if vnpuManager != nil {
			vnpuManager.InitPhysicalNPU(deviceName, dev.LogicID, dev.DevType)
			maxAICore, maxMemory := getDeviceResources(mgr, dev.DevType, vnpuManager, deviceName)
			devAttributes[DriverDomain+"aicore"] = resourceapi.DeviceAttribute{IntValue: ptr.To(int64(maxAICore))}
			devAttributes[DriverDomain+"memory"] = resourceapi.DeviceAttribute{IntValue: ptr.To(int64(maxMemory))}
		}

		device := resourceapi.Device{
			Name:       deviceName,
			Attributes: devAttributes,
		}
		alldevices[device.Name] = device
		log.Printf("Discovered NPU device: %s, Type: NPU, Model: %s", deviceName, dev.DevType)
	}
	return alldevices, vnpuManager, nil
}
