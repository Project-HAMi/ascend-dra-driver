package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"slices"
	"strings"
	"sync"

	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	drapbv1 "k8s.io/kubelet/pkg/apis/dra/v1"
	"k8s.io/kubernetes/pkg/kubelet/checkpointmanager"
	"k8s.io/utils/ptr"

	"github.com/Project-HAMi/hami-dra-driver/pkg/consts"

	configapi "github.com/Project-HAMi/hami-dra-driver/api/project-hami.io/resource/npu/v1alpha1"
	"github.com/Project-HAMi/hami-dra-driver/pkg/npuutil"

	"k8s.io/apimachinery/pkg/api/errors"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdispec "tags.cncf.io/container-device-interface/specs-go"
)

type (
	AllocatableDevices         map[string]resourceapi.Device
	PreparedDevices            []*PreparedDevice
	PreparedClaims             map[string]PreparedDevices
	PerDeviceCDIContainerEdits map[string]*cdiapi.ContainerEdits
)

type OpaqueDeviceConfig struct {
	Requests []string
	Config   runtime.Object
}

type VNPUTemplateAttribute struct {
	AICORE int
	Memory int
}

type VNPUTemplate struct {
	Name       string
	Attributes VNPUTemplateAttribute
}

type VNPUSlice struct {
	SliceID      string
	TemplateName string
	Allocated    bool
	Type         string
}

type PhysicalNPUState struct {
	DeviceName       string
	PhysicalDeviceID string
	LogicID          int32
	ModelName        string
	AvailableSlices  []*VNPUSlice
	AllocatedSlices  []*VNPUSlice
	SupportTemplates map[string]*VNPUTemplate
	NextSliceIndex   int
}

type DeviceUpdateCallback func(deviceName string, physicalNpu *PhysicalNPUState)

type VNPUManager struct {
	sync.Mutex
	PhysicalNPUs         map[string]*PhysicalNPUState
	Templates            map[string]*VNPUTemplate
	deviceUpdateCallback DeviceUpdateCallback
}

func (m *VNPUManager) SetDeviceUpdateCallback(callback DeviceUpdateCallback) {
	m.Lock()
	defer m.Unlock()
	m.deviceUpdateCallback = callback
}

type PreparedDevice struct {
	drapbv1.Device
	ContainerEdits *cdiapi.ContainerEdits
}

func (pds PreparedDevices) GetDevices() []*drapbv1.Device {
	var devices []*drapbv1.Device
	for _, pd := range pds {
		devices = append(devices, &pd.Device)
	}
	return devices
}

type DeviceState struct {
	sync.Mutex
	cdi               *CDIHandler
	allocatable       AllocatableDevices
	checkpointManager checkpointmanager.CheckpointManager
	vnpuManager       *VNPUManager
}

func NewDeviceState(config *Config) (*DeviceState, error) {
	allocatable, vnpuManager, err := enumerateAllPossibleDevices()
	if err != nil {
		return nil, fmt.Errorf("error enumerating all possible devices: %v", err)
	}

	cdi, err := NewCDIHandler(config)
	if err != nil {
		return nil, fmt.Errorf("unable to create CDI handler: %v", err)
	}

	err = cdi.CreateCommonSpecFile()
	if err != nil {
		return nil, fmt.Errorf("unable to create CDI spec file for common edits: %v", err)
	}

	checkpointManager, err := checkpointmanager.NewCheckpointManager(config.DriverPluginPath())
	if err != nil {
		return nil, fmt.Errorf("unable to create checkpoint manager: %v", err)
	}

	state := &DeviceState{
		cdi:               cdi,
		allocatable:       allocatable,
		checkpointManager: checkpointManager,
		vnpuManager:       vnpuManager,
	}

	if vnpuManager != nil {
		vnpuManager.SetDeviceUpdateCallback(func(deviceName string, physicalNpu *PhysicalNPUState) {
			if added := state.UpdateAllocatableDevice(deviceName, physicalNpu); added {
				log.Printf("Added new device %s to allocatable devices", deviceName)
			}
		})
	}

	checkpoints, err := state.checkpointManager.ListCheckpoints()
	if err != nil {
		return nil, fmt.Errorf("unable to list checkpoints: %v", err)
	}

	for _, c := range checkpoints {
		if c == DriverPluginCheckpointFile {
			if vnpuManager != nil {
				if err := CreatePredefinedDeviceClasses(vnpuManager); err != nil {
					log.Printf("Failed to create predefined DeviceClasses: %v", err)
				}
			}
			return state, nil
		}
	}

	checkpoint := newCheckpoint()
	if err := state.checkpointManager.CreateCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
		return nil, fmt.Errorf("unable to sync to checkpoint: %v", err)
	}
	if vnpuManager != nil {
		go func() {
			if err := CreatePredefinedDeviceClasses(vnpuManager); err != nil {
				log.Printf("Failed to create predefined DeviceClasses: %v", err)
			}
		}()
	}
	return state, nil
}

func (s *DeviceState) Prepare(claim *resourceapi.ResourceClaim) ([]*drapbv1.Device, error) {
	s.Lock()
	defer s.Unlock()

	claimUID := string(claim.UID)

	checkpoint := newCheckpoint()
	if err := s.checkpointManager.GetCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
		return nil, fmt.Errorf("unable to sync from checkpoint: %v", err)
	}
	preparedClaims := checkpoint.V1.PreparedClaims

	if preparedClaims[claimUID] != nil {
		return preparedClaims[claimUID].GetDevices(), nil
	}

	preparedDevices, err := s.prepareDevices(claim)
	if err != nil {
		return nil, fmt.Errorf("prepare failed: %v", err)
	}

	if err = s.cdi.CreateClaimSpecFile(claimUID, preparedDevices); err != nil {
		return nil, fmt.Errorf("unable to create CDI spec file for claim: %v", err)
	}

	preparedClaims[claimUID] = preparedDevices
	if err := s.checkpointManager.CreateCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
		return nil, fmt.Errorf("unable to sync to checkpoint: %v", err)
	}

	return preparedClaims[claimUID].GetDevices(), nil
}

func (s *DeviceState) Unprepare(claimUID string) error {
	s.Lock()
	defer s.Unlock()

	checkpoint := newCheckpoint()
	if err := s.checkpointManager.GetCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
		return fmt.Errorf("unable to sync from checkpoint: %v", err)
	}
	preparedClaims := checkpoint.V1.PreparedClaims
	if preparedClaims[claimUID] == nil {
		return nil
	}

	if err := s.unprepareDevices(claimUID, preparedClaims[claimUID]); err != nil {
		return fmt.Errorf("unprepare failed: %v", err)
	}

	err := s.cdi.DeleteClaimSpecFile(claimUID)
	if err != nil {
		return fmt.Errorf("unable to delete CDI spec file for claim: %v", err)
	}

	delete(preparedClaims, claimUID)
	if err := s.checkpointManager.CreateCheckpoint(DriverPluginCheckpointFile, checkpoint); err != nil {
		return fmt.Errorf("unable to sync to checkpoint: %v", err)
	}

	return nil
}

func (s *DeviceState) prepareDevices(claim *resourceapi.ResourceClaim) (PreparedDevices, error) {
	if claim.Status.Allocation == nil {
		return nil, fmt.Errorf("claim not yet allocated")
	}

	// Retrieve the full set of device configs for the driver.
	configs, err := GetOpaqueDeviceConfigs(
		configapi.Decoder,
		consts.DriverName,
		claim.Status.Allocation.Devices.Config,
	)
	if err != nil {
		return nil, fmt.Errorf("error getting opaque device configs: %v", err)
	}

	// Add the default GPU Config to the front of the config list with the
	// lowest precedence. This guarantees there will be at least one config in
	// the list with len(Requests) == 0 for the lookup below.
	configs = slices.Insert(configs, 0, &OpaqueDeviceConfig{
		Requests: []string{},
		Config:   configapi.DefaultNpuConfig(),
	})

	// Look through the configs and figure out which one will be applied to
	// each device allocation result based on their order of precedence.
	configResultsMap := make(map[runtime.Object][]*resourceapi.DeviceRequestAllocationResult)
	for _, result := range claim.Status.Allocation.Devices.Results {
		origDevice := result.Device

		// If vnpuManager is available, try to allocate vNPU slices first
		if s.vnpuManager != nil {
			if err := s.allocateVNPUSlice(&result, configs, origDevice); err != nil {
				log.Printf("Warning: failed to allocate vNPU slice: %v, attempting to use full card allocation", err)
			}
		}

		if _, ok := s.allocatable[origDevice]; !ok {
			return nil, fmt.Errorf("requested NPU is not allocatable: %v", origDevice)
		}
		// Find matching config
		for _, c := range slices.Backward(configs) {
			if len(c.Requests) == 0 || slices.Contains(c.Requests, result.Request) {
				configResultsMap[c.Config] = append(configResultsMap[c.Config], &result)
				break
			}
		}
	}

	// Normalize, validate, and apply all configs associated with devices that
	// need to be prepared. Track container edits generated from applying the
	// config to the set of device allocation results.
	perDeviceCDIContainerEdits := make(PerDeviceCDIContainerEdits)
	for c, results := range configResultsMap {
		// Cast the opaque config to a NpuConfig
		var config *configapi.NpuConfig
		switch castConfig := c.(type) {
		case *configapi.NpuConfig:
			config = castConfig
		default:
			return nil, fmt.Errorf("runtime object is not a regognized configuration")
		}

		// Normalize the config to set any implied defaults.
		if err := config.Normalize(); err != nil {
			return nil, fmt.Errorf("error normalizing GPU config: %w", err)
		}

		// Validate the config to ensure its integrity.
		if err := config.Validate(); err != nil {
			return nil, fmt.Errorf("error validating GPU config: %w", err)
		}

		// Apply the config to the list of results associated with it.
		containerEdits, err := s.applyConfig(config, results)
		if err != nil {
			return nil, fmt.Errorf("error applying GPU config: %w", err)
		}

		// Merge any new container edits with the overall per device map.
		for k, v := range containerEdits {
			perDeviceCDIContainerEdits[k] = v
		}
	}

	// Walk through each config and its associated device allocation results
	// and construct the list of prepared devices to return.
	var preparedDevices PreparedDevices
	for _, results := range configResultsMap {
		for _, result := range results {
			device := &PreparedDevice{
				Device: drapbv1.Device{
					RequestNames: []string{result.Request},
					PoolName:     result.Pool,
					DeviceName:   result.Device,
					CDIDeviceIDs: s.cdi.GetClaimDevices(string(claim.UID), []string{result.Device}),
				},
				ContainerEdits: perDeviceCDIContainerEdits[result.Device],
			}
			preparedDevices = append(preparedDevices, device)
		}
	}

	return preparedDevices, nil
}

// allocateVNPUSlice tries to allocate a vNPU slice based on user requirements
func (s *DeviceState) allocateVNPUSlice(
	result *resourceapi.DeviceRequestAllocationResult,
	configs []*OpaqueDeviceConfig,
	origDevice string,
) error {
	var requestedAICore, requestedMemory int
	var templateName string
	for _, oc := range configs {
		if gpuConfig, ok := oc.Config.(*configapi.NpuConfig); ok {
			if gpuConfig.VNPUSpec != nil && gpuConfig.VNPUSpec.TemplateName != "" {
				templateName = gpuConfig.VNPUSpec.TemplateName
				if tpl, found := s.vnpuManager.Templates[templateName]; found {
					requestedAICore = tpl.Attributes.AICORE
					requestedMemory = tpl.Attributes.Memory
					log.Printf("Obtained resource requirements from template %s: AICORE=%d, Memory=%dGB",
						templateName, requestedAICore, requestedMemory)
					break
				}
			}
		}
	}
	slice, err := s.vnpuManager.AllocateSlice(origDevice, requestedAICore, requestedMemory)
	if err != nil {
		return err
	}
	result.Device = slice.SliceID
	log.Printf("Successfully allocated vNPU slice for device %s: %s (template: %s, AICORE: %d, Memory: %dGB)",
		origDevice, slice.SliceID, templateName, requestedAICore, requestedMemory)
	return nil
}

// unprepareDevices reclaims devices under the specified ClaimUID
func (s *DeviceState) unprepareDevices(claimUID string, devices PreparedDevices) error {
	log.Printf("Starting to release devices, claimUID: %s", claimUID)
	if s.vnpuManager == nil {
		return nil
	}
	for _, dev := range devices {
		if err := s.vnpuManager.ReleaseSlice(dev.Device.DeviceName); err != nil {
			log.Printf("Warning: failed to release vNPU slice %s: %v", dev.Device.DeviceName, err)
		} else {
			log.Printf("Successfully released vNPU slice: %s", dev.Device.DeviceName)
		}
	}
	return nil
}

// applyConfig applies a configuration to a set of device allocation results.
//
// For the Ascend NPU driver this means emitting environment variables that
// describe the selected sharing strategy and, for vNPU slices, the slice
// specification. The Ascend runtime performs the actual hardware-level
// configuration based on these values and the allocated device.
func (s *DeviceState) applyConfig(config *configapi.NpuConfig, results []*resourceapi.DeviceRequestAllocationResult) (PerDeviceCDIContainerEdits, error) {
	perDeviceEdits := make(PerDeviceCDIContainerEdits)

	for _, result := range results {
		envs := buildBaseEnv(result.Device)
		if s.vnpuManager != nil {
			envs = s.addVNPUEnvIfSlice(envs, result.Device)
		}
		envs = addSharingStrategyEnv(envs, config, result.Device)
		edits := &cdispec.ContainerEdits{Env: envs}
		perDeviceEdits[result.Device] = &cdiapi.ContainerEdits{ContainerEdits: edits}
	}
	return perDeviceEdits, nil
}

// buildBaseEnv constructs basic environment variables such as ASCEND_VISIBLE_DEVICES
func buildBaseEnv(deviceName string) []string {
	dn, err := npuutil.ParseDeviceName(deviceName)
	if err != nil {
		log.Printf("Warning: invalid NPU device name %q: %v", deviceName, err)
		return nil
	}
	return []string{
		fmt.Sprintf("ASCEND_VISIBLE_DEVICES=%s", dn.VisibleDevice()),
	}
}

// addVNPUEnvIfSlice adds ASCEND_VNPU_SPECS if it is a slice format npu-x-y.
func (s *DeviceState) addVNPUEnvIfSlice(envs []string, deviceID string) []string {
	dn, err := npuutil.ParseDeviceName(deviceID)
	if err != nil || !dn.IsSlice() {
		return envs
	}
	vnpuSpec, err := s.vnpuManager.GetVNPUSpecsEnv(deviceID)
	if err != nil {
		log.Printf("Warning: failed to get vNPU specs: %v", err)
		return envs
	}
	if vnpuSpec != "" {
		envs = append(envs, fmt.Sprintf("ASCEND_VNPU_SPECS=%s", vnpuSpec))
		log.Printf("Set vNPU specs for device %s: %s", deviceID, vnpuSpec)
	}
	return envs
}

// addSharingStrategyEnv adds environment variables for the sharing strategy
func addSharingStrategyEnv(envs []string, config *configapi.NpuConfig, deviceName string) []string {
	if config.Sharing == nil {
		return envs
	}
	dn, err := npuutil.ParseDeviceName(deviceName)
	if err != nil {
		log.Printf("Warning: invalid NPU device name %q for sharing strategy env: %v", deviceName, err)
		return envs
	}
	suffix := dn.EnvSuffix()
	envs = append(envs, fmt.Sprintf("NPU_DEVICE_%s_SHARING_STRATEGY=%s", suffix, config.Sharing.Strategy))
	switch {
	case config.Sharing.IsTimeSlicing():
		tsconfig, _ := config.Sharing.GetTimeSlicingConfig()
		if tsconfig != nil {
			envs = append(envs, fmt.Sprintf("NPU_DEVICE_%s_TIMESLICE_INTERVAL=%v", suffix, tsconfig.Interval))
		}
	case config.Sharing.IsSpacePartitioning():
		spconfig, _ := config.Sharing.GetSpacePartitioningConfig()
		if spconfig != nil {
			envs = append(envs, fmt.Sprintf("NPU_DEVICE_%s_PARTITION_COUNT=%v", suffix, spconfig.PartitionCount))
		}
	}
	return envs
}

// GetOpaqueDeviceConfigs returns an ordered list of the configs contained in possibleConfigs for this driver.
//
// Configs can either come from the resource claim itself or from the device
// class associated with the request. Configs coming directly from the resource
// claim take precedence over configs coming from the device class. Moreover,
// configs found later in the list of configs attached to its source take
// precedence over configs found earlier in the list for that source.
//
// All of the configs relevant to the driver from the list of possibleConfigs
// will be returned in order of precedence (from lowest to highest). If no
// configs are found, nil is returned.
func GetOpaqueDeviceConfigs(
	decoder runtime.Decoder,
	driverName string,
	possibleConfigs []resourceapi.DeviceAllocationConfiguration,
) ([]*OpaqueDeviceConfig, error) {
	// Collect all configs in order of reverse precedence.
	var classConfigs []resourceapi.DeviceAllocationConfiguration
	var claimConfigs []resourceapi.DeviceAllocationConfiguration
	var candidateConfigs []resourceapi.DeviceAllocationConfiguration
	for _, config := range possibleConfigs {
		switch config.Source {
		case resourceapi.AllocationConfigSourceClass:
			classConfigs = append(classConfigs, config)
		case resourceapi.AllocationConfigSourceClaim:
			claimConfigs = append(claimConfigs, config)
		default:
			return nil, fmt.Errorf("invalid config source: %v", config.Source)
		}
	}
	candidateConfigs = append(candidateConfigs, classConfigs...)

	// Decode all configs that are relevant for the driver.
	var resultConfigs []*OpaqueDeviceConfig
	for _, config := range candidateConfigs {
		// If this is nil, the driver doesn't support some future API extension
		// and needs to be updated.
		if config.DeviceConfiguration.Opaque == nil {
			return nil, fmt.Errorf("only opaque parameters are supported by this driver")
		}

		// Configs for different drivers may have been specified because a
		// single request can be satisfied by different drivers. This is not
		// an error -- drivers must skip over other driver's configs in order
		// to support this.
		if config.DeviceConfiguration.Opaque.Driver != driverName {
			continue
		}

		decodedConfig, err := runtime.Decode(decoder, config.DeviceConfiguration.Opaque.Parameters.Raw)
		if err != nil {
			return nil, fmt.Errorf("error decoding config parameters: %w", err)
		}

		resultConfig := &OpaqueDeviceConfig{
			Requests: config.Requests,
			Config:   decodedConfig,
		}

		resultConfigs = append(resultConfigs, resultConfig)
	}

	return resultConfigs, nil
}

// AllocateSlice allocates a vNPU slice based on the requested computational resources
func (m *VNPUManager) AllocateSlice(deviceName string, requestedAICore, requestedMemory int) (*VNPUSlice, error) {
	m.Lock()
	defer m.Unlock()
	log.Printf("Attempting to allocate vNPU slice, device: %s, requirements: AICORE=%d, Memory=%dGB", deviceName, requestedAICore, requestedMemory)
	physicalNpu, ok := m.PhysicalNPUs[deviceName]
	if !ok {
		return nil, fmt.Errorf("physical NPU not found: %s", deviceName)
	}
	if requestedAICore == 0 && requestedMemory == 0 {
		return m.allocateFullCard(physicalNpu, deviceName)
	}
	return m.allocateSliceByTemplate(physicalNpu, deviceName, requestedAICore, requestedMemory)
}

// allocateFullCard allocates the entire card
func (m *VNPUManager) allocateFullCard(npu *PhysicalNPUState, deviceName string) (*VNPUSlice, error) {
	for i, slice := range npu.AvailableSlices {
		if slice.SliceID == deviceName && !slice.Allocated {
			slice.Allocated = true
			npu.AllocatedSlices = append(npu.AllocatedSlices, slice)
			npu.AvailableSlices = append(npu.AvailableSlices[:i], npu.AvailableSlices[i+1:]...)
			log.Printf("Successfully allocated the full physical NPU slice %s", deviceName)
			return slice, nil
		}
	}
	return nil, fmt.Errorf("the slice %s has already been allocated", deviceName)
}

// allocateSliceByTemplate allocates a vNPU slice based on template attributes
func (m *VNPUManager) allocateSliceByTemplate(
	npu *PhysicalNPUState,
	deviceName string,
	requestedAICore, requestedMemory int,
) (*VNPUSlice, error) {
	var bestTemplate *VNPUTemplate
	bestDiff := math.MaxInt32
	for _, template := range npu.SupportTemplates {
		if template.Attributes.AICORE >= requestedAICore &&
			template.Attributes.Memory >= requestedMemory {
			diff := (template.Attributes.AICORE - requestedAICore) + (template.Attributes.Memory - requestedMemory)
			if diff < bestDiff {
				bestDiff = diff
				bestTemplate = template
			}
		}
	}
	if bestTemplate == nil {
		return nil, fmt.Errorf("no partition scheme found that meets the requirements: AICORE>=%d, Memory>=%dGB", requestedAICore, requestedMemory)
	}

	var currentSlice *VNPUSlice
	var sliceIndex int
	for i, slice := range npu.AvailableSlices {
		if slice.SliceID == deviceName && !slice.Allocated {
			currentSlice = slice
			sliceIndex = i
			break
		}
	}

	if currentSlice == nil {
		return nil, fmt.Errorf("cannot find available slice %s", deviceName)
	}

	npu.AvailableSlices = append(npu.AvailableSlices[:sliceIndex], npu.AvailableSlices[sliceIndex+1:]...)

	currentSlice.TemplateName = bestTemplate.Name
	currentSlice.Allocated = true

	npu.AllocatedSlices = append(npu.AllocatedSlices, currentSlice)

	newSliceID := fmt.Sprintf("%s%d-%d", consts.NPUPrefix, npu.LogicID, npu.NextSliceIndex)
	newSlice := &VNPUSlice{
		SliceID:      newSliceID,
		TemplateName: "",
		Allocated:    false,
		Type:         "vNPU",
	}
	npu.AvailableSlices = append(npu.AvailableSlices, newSlice)

	if m.deviceUpdateCallback != nil {
		m.deviceUpdateCallback(newSliceID, npu)
	}

	npu.NextSliceIndex++

	log.Printf("Successfully allocated vNPU slice: %s with template %s (AICORE: %d, Memory: %dGB)",
		currentSlice.SliceID, bestTemplate.Name, bestTemplate.Attributes.AICORE, bestTemplate.Attributes.Memory)
	log.Printf("Created new available slice: %s representing remaining resources", newSliceID)

	return currentSlice, nil
}

// CreatePredefinedDeviceClasses idempotently creates/updates DeviceClasses
func CreatePredefinedDeviceClasses(vnpuManager *VNPUManager) error {
	log.Printf("Starting to create predefined DeviceClasses...")
	config, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("failed to get in-cluster config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	uniqueModels := make(map[string]bool)
	uniqueTemplates := make(map[string]*VNPUTemplate)

	for _, pNpu := range vnpuManager.PhysicalNPUs {
		modelName := pNpu.ModelName
		if modelName == "" {
			modelName = "unknown"
		}
		uniqueModels[modelName] = true
		for _, tpl := range pNpu.SupportTemplates {
			key := fmt.Sprintf("aicore-%d-mem-%d", tpl.Attributes.AICORE, tpl.Attributes.Memory)
			if _, ok := uniqueTemplates[key]; !ok {
				uniqueTemplates[key] = tpl
			}
		}
	}

	// Create a full-card DeviceClass for each unique model
	for modelName := range uniqueModels {
		if err := createFullCardDeviceClass(clientset, modelName); err != nil {
			log.Printf("Failed to create/update full-card DeviceClass: %v", err)
		}
	}

	// Create the corresponding DeviceClass for each unique template and each unique model
	for _, tpl := range uniqueTemplates {
		for modelName := range uniqueModels {
			if err := createMemoryDeviceClass(clientset, modelName, tpl); err != nil {
				log.Printf("Failed to create/update Memory DeviceClass: %v", err)
			}
			if err := createAICoreDeviceClass(clientset, modelName, tpl); err != nil {
				log.Printf("Failed to create/update AICORE DeviceClass: %v", err)
			}
		}
	}

	log.Printf("Predefined DeviceClass creation completed")
	return nil
}

// createFullCardDeviceClass creates or updates a "full-card" DeviceClass
func createFullCardDeviceClass(clientset *kubernetes.Clientset, modelName string) error {
	safeModel := toSafeModelName(modelName)
	dcName := fmt.Sprintf("%s%s%s%s", consts.DeviceClassNamePrefix, safeModel, consts.DeviceClassNameSuffix, "")
	expr := fmt.Sprintf(`device.attributes["%s"].model == "%s" && device.attributes["%s"].type == "NPU"`,
		DriverDomainName, modelName, DriverDomainName)
	return upsertDeviceClass(clientset, dcName, expr, "")
}

// createMemoryDeviceClass creates or updates a DeviceClass based on memory
func createMemoryDeviceClass(clientset *kubernetes.Clientset, modelName string, tpl *VNPUTemplate) error {
	safeModel := toSafeModelName(modelName)
	dcName := fmt.Sprintf("%s%s-mem%d%s", consts.DeviceClassNamePrefix, safeModel, tpl.Attributes.Memory, consts.DeviceClassNameSuffix)
	expr := fmt.Sprintf(`device.attributes["%s"].memory >= %d && device.attributes["%s"].model == "%s"`,
		DriverDomainName, tpl.Attributes.Memory, DriverDomainName, modelName)
	return upsertDeviceClass(clientset, dcName, expr, tpl.Name)
}

// createAICoreDeviceClass creates or updates a DeviceClass based on AICORE
func createAICoreDeviceClass(clientset *kubernetes.Clientset, modelName string, tpl *VNPUTemplate) error {
	safeModel := toSafeModelName(modelName)
	dcName := fmt.Sprintf("%s%s-aicore%d%s", consts.DeviceClassNamePrefix, safeModel, tpl.Attributes.AICORE, consts.DeviceClassNameSuffix)
	expr := fmt.Sprintf(`device.attributes["%s"].aicore >= %d && device.attributes["%s"].model == "%s"`,
		DriverDomainName, tpl.Attributes.AICORE, DriverDomainName, modelName)
	return upsertDeviceClass(clientset, dcName, expr, tpl.Name)
}

// upsertDeviceClass idempotently creates/updates a DeviceClass
func upsertDeviceClass(clientset *kubernetes.Clientset, name, expr, tpl string) error {
	want, err := buildDeviceClass(name, expr, tpl)
	if err != nil {
		return err
	}

	got, getErr := clientset.ResourceV1().DeviceClasses().Get(
		context.TODO(), name, metav1.GetOptions{},
	)
	if errors.IsNotFound(getErr) {
		_, createErr := clientset.ResourceV1().DeviceClasses().Create(
			context.TODO(), want, metav1.CreateOptions{},
		)
		if createErr != nil {
			return fmt.Errorf("failed to create DeviceClass: %v", createErr)
		}
		log.Printf("Successfully created DeviceClass: %s", name)
		return nil
	}
	if getErr != nil {
		return fmt.Errorf("failed to get DeviceClass: %v", getErr)
	}

	if !deviceClassEquals(got, want) {
		want.ObjectMeta.ResourceVersion = got.ObjectMeta.ResourceVersion
		_, updateErr := clientset.ResourceV1().DeviceClasses().Update(
			context.TODO(), want, metav1.UpdateOptions{},
		)
		if updateErr != nil {
			return fmt.Errorf("failed to update DeviceClass: %v", updateErr)
		}
		log.Printf("Successfully updated DeviceClass: %s", name)
	}

	return nil
}

// buildDeviceClass generates the target DeviceClass
func buildDeviceClass(name, celExpression, tplName string) (*resourceapi.DeviceClass, error) {
	paramObj := map[string]interface{}{
		"apiVersion": "npu.project-hami.io/v1alpha1",
		"kind":       "NpuConfig",
		"vnpuSpec": map[string]interface{}{
			"templateName": tplName,
		},
	}
	raw, err := json.Marshal(paramObj)
	if err != nil {
		return nil, err
	}
	return &resourceapi.DeviceClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: resourceapi.DeviceClassSpec{
			Selectors: []resourceapi.DeviceSelector{{
				CEL: &resourceapi.CELDeviceSelector{Expression: celExpression},
			}},
			Config: []resourceapi.DeviceClassConfiguration{
				{
					DeviceConfiguration: resourceapi.DeviceConfiguration{
						Opaque: &resourceapi.OpaqueDeviceConfiguration{
							Driver: consts.DriverName,
							Parameters: runtime.RawExtension{
								Raw: raw,
							},
						},
					},
				},
			},
		},
	}, nil
}

// deviceClassEquals performs a simple comparison of the Spec fields
func deviceClassEquals(a, b *resourceapi.DeviceClass) bool {
	aSpec, _ := json.Marshal(a.Spec)
	bSpec, _ := json.Marshal(b.Spec)
	return string(aSpec) == string(bSpec)
}

// toSafeModelName removes extra characters from model and converts to lowercase
func toSafeModelName(model string) string {
	model = strings.ReplaceAll(model, " ", "-")
	model = strings.ReplaceAll(model, "/", "-")
	return strings.ToLower(model)
}

func (s *DeviceState) UpdateAllocatableDevice(deviceName string, physicalNpu *PhysicalNPUState) bool {
	_, exists := s.allocatable[deviceName]
	if exists {
		return false
	}

	var sliceType string = "NPU"
	for _, slice := range physicalNpu.AvailableSlices {
		if slice.SliceID == deviceName {
			sliceType = slice.Type
			break
		}
	}

	uuidStr := fmt.Sprintf("%s-%d", os.Getenv("NODE_NAME"), physicalNpu.LogicID)

	devAttributes := map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
		DriverDomain + "index": {IntValue: ptr.To(int64(physicalNpu.LogicID))},
		DriverDomain + "uuid":  {StringValue: ptr.To(uuidStr)},
		DriverDomain + "model": {StringValue: ptr.To(physicalNpu.ModelName)},
		DriverDomain + "type":  {StringValue: ptr.To(sliceType)},
	}

	if s.vnpuManager != nil {
		maxAICore, maxMemory := 0, 0
		for _, tpl := range physicalNpu.SupportTemplates {
			if tpl.Attributes.AICORE > maxAICore {
				maxAICore = tpl.Attributes.AICORE
			}
			if tpl.Attributes.Memory > maxMemory {
				maxMemory = tpl.Attributes.Memory
			}
		}

		devAttributes[DriverDomain+"aicore"] = resourceapi.DeviceAttribute{IntValue: ptr.To(int64(maxAICore))}
		devAttributes[DriverDomain+"memory"] = resourceapi.DeviceAttribute{IntValue: ptr.To(int64(maxMemory))}
	}

	device := resourceapi.Device{
		Name:       deviceName,
		Attributes: devAttributes,
	}

	s.allocatable[deviceName] = device
	log.Printf("Added new allocatable NPU device: %s, Type: %s, Model: %s", deviceName, sliceType, physicalNpu.ModelName)
	return true
}
