package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// NewVnpuManager creates and initializes a new VnpuManager.
func NewVnpuManager() (*VnpuManager, error) {
	templates, err := GetNpuTemplateInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get NPU template info: %v", err)
	}
	return &VnpuManager{
		PhysicalNpus: make(map[string]*PhysicalNpuState),
		Templates:    templates,
	}, nil
}

// GetNpuTemplateInfo attempts to read the NPU template information from a file.
// If the file is not found, it falls back to default templates.
func GetNpuTemplateInfo() (map[string]*VnpuTemplate, error) {
	filePath := "/etc/npu/template-info.txt"
	content, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("Failed to read template file: %v. Using default templates.", err)
		return createDefaultTemplates(), nil
	}
	templates := make(map[string]*VnpuTemplate)
	if err := parseTemplateInfo(string(content), templates); err != nil {
		return nil, err
	}
	log.Printf("Successfully loaded %d templates from file.", len(templates))
	return templates, nil
}

// createDefaultTemplates generates a set of default templates.
func createDefaultTemplates() map[string]*VnpuTemplate {
	templates := map[string]*VnpuTemplate{
		"vir01": {Name: "vir01", Attributes: VnpuTemplateAttribute{AICORE: 4, Memory: 8}},
		"vir02": {Name: "vir02", Attributes: VnpuTemplateAttribute{AICORE: 8, Memory: 12}},
		"vir04": {Name: "vir04", Attributes: VnpuTemplateAttribute{AICORE: 16, Memory: 16}},
	}
	log.Printf("Using default templates. Total: %d", len(templates))
	return templates
}

// parseTemplateInfo parses the template info string and populates the templates map.
func parseTemplateInfo(output string, templates map[string]*VnpuTemplate) error {
	scanner := bufio.NewScanner(strings.NewReader(output))

	var headerLine string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "Name") && strings.Contains(line, "AICORE") && strings.Contains(line, "Memory") {
			headerLine = line
			break
		}
	}
	if headerLine == "" {
		return fmt.Errorf("failed to find template info header")
	}

	headerLine = strings.Trim(headerLine, "|")
	headerFields := regexp.MustCompile(`\s+`).Split(strings.TrimSpace(headerLine), -1)
	columnPositions := map[string]int{}
	for i, field := range headerFields {
		columnPositions[field] = i
	}

	// Skip the line immediately after the header
	if scanner.Scan() {
	}

	// Skip until we reach a line containing "=="
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "==") {
			break
		}
	}

	var (
		currentTemplate string
		currentAttrs    *VnpuTemplateAttribute
	)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.Trim(scanner.Text(), "|"))
		if line == "" || strings.Contains(line, "--") {
			continue
		}
		fields := regexp.MustCompile(`\s+`).Split(line, -1)
		if len(fields) > 0 && strings.HasPrefix(fields[0], "vir") {
			currentTemplate = fields[0]
			currentAttrs = &VnpuTemplateAttribute{}
			for attr, pos := range columnPositions {
				if attr == "Name" || pos >= len(fields) {
					continue
				}
				valStr := fields[pos]
				if attr == "Memory" {
					valStr = strings.TrimSuffix(valStr, "GB")
				}
				val, err := strconv.Atoi(valStr)
				if err != nil {
					log.Printf("Warning: failed to parse %s value %s: %v", attr, fields[pos], err)
					continue
				}
				switch attr {
				case "AICORE":
					currentAttrs.AICORE = val
				case "Memory":
					currentAttrs.Memory = val
				}
			}
			templates[currentTemplate] = &VnpuTemplate{
				Name:       currentTemplate,
				Attributes: *currentAttrs,
			}
		}
	}
	return nil
}

// InitPhysicalNpu initializes a physical NPU, using the entire card as a default available slice.
func (m *VnpuManager) InitPhysicalNpu(deviceName string, logicID int32, modelName string) {
	m.Lock()
	defer m.Unlock()

	if _, exists := m.PhysicalNpus[deviceName]; exists {
		log.Printf("Physical NPU %s already exists, skipping initialization.", deviceName)
		return
	}

	physicalDeviceID := fmt.Sprintf("npu-%d", logicID)

	npu := &PhysicalNpuState{
		DeviceName:       deviceName,
		PhysicalDeviceID: physicalDeviceID,
		LogicID:          logicID,
		ModelName:        modelName,
		AvailableSlices:  []*VnpuSlice{},
		AllocatedSlices:  []*VnpuSlice{},
		SupportTemplates: cloneTemplates(m.Templates),
		NextSliceIndex:   1,
	}

	npu.AvailableSlices = append(npu.AvailableSlices, &VnpuSlice{
		SliceID:      deviceName,
		TemplateName: "",
		Allocated:    false,
		Type:         "NPU",
	})
	m.PhysicalNpus[deviceName] = npu

	log.Printf("Physical NPU %s has been initialized.", deviceName)
}

// ReleaseSlice releases the specified VNPU slice.
func (m *VnpuManager) ReleaseSlice(sliceID string) error {
	m.Lock()
	defer m.Unlock()

	pnpu, idx, slice, err := m.findAllocatedSlice(sliceID)
	if err != nil {
		return err
	}

	pnpu.AllocatedSlices = append(pnpu.AllocatedSlices[:idx], pnpu.AllocatedSlices[idx+1:]...)
	slice.Allocated = false

	if slice.Type == "NPU" {
		pnpu.AllocatedSlices = []*VnpuSlice{}
		pnpu.AvailableSlices = []*VnpuSlice{}
		pnpu.NextSliceIndex = 1
		pnpu.AvailableSlices = append(pnpu.AvailableSlices, &VnpuSlice{
			SliceID:      pnpu.DeviceName,
			TemplateName: "",
			Allocated:    false,
			Type:         "NPU",
		})
		log.Printf("Successfully released the entire NPU card %s, restored to initial state", pnpu.DeviceName)
		return nil
	}

	if len(pnpu.AllocatedSlices) == 0 {
		pnpu.AvailableSlices = []*VnpuSlice{}
		pnpu.NextSliceIndex = 1
		pnpu.AvailableSlices = append(pnpu.AvailableSlices, &VnpuSlice{
			SliceID:      pnpu.DeviceName,
			TemplateName: "",
			Allocated:    false,
			Type:         "NPU",
		})
		log.Printf("All vNPU slices released for device %s, restored to full card state", pnpu.DeviceName)
	} else {
		pnpu.AvailableSlices = []*VnpuSlice{}
		newSliceID := fmt.Sprintf("npu-%d-%d", pnpu.LogicID, pnpu.NextSliceIndex)
		newSlice := &VnpuSlice{
			SliceID:      newSliceID,
			TemplateName: "",
			Allocated:    false,
			Type:         "vNPU",
		}
		pnpu.AvailableSlices = append(pnpu.AvailableSlices, newSlice)
		pnpu.NextSliceIndex++

		if m.deviceUpdateCallback != nil {
			m.deviceUpdateCallback(newSliceID, pnpu)
		}
		log.Printf("Released vNPU slice %s, created new available slice %s", sliceID, newSliceID)
	}

	m.updateSupportTemplates(pnpu)
	return nil
}

// GetVnpuSpecsEnv returns the ASCEND_VNPU_SPECS environment variable for a given slice.
func (m *VnpuManager) GetVnpuSpecsEnv(sliceID string) (string, error) {
	m.Lock()
	defer m.Unlock()

	_, _, slice, err := m.findAllocatedSlice(sliceID)
	if err != nil {
		return "", err
	}
	if slice.TemplateName == "" {
		return "", nil
	}
	return slice.TemplateName, nil
}

// findAllocatedSlice is a helper method to locate an allocated slice by its ID.
func (m *VnpuManager) findAllocatedSlice(sliceID string) (*PhysicalNpuState, int, *VnpuSlice, error) {
	for _, npu := range m.PhysicalNpus {
		for i, s := range npu.AllocatedSlices {
			if s.SliceID == sliceID {
				return npu, i, s, nil
			}
		}
	}
	return nil, -1, nil, fmt.Errorf("VNPU slice %s not found", sliceID)
}

// wholeCardIsAvailable checks if the entire card slice is in the available slices.
func (m *VnpuManager) wholeCardIsAvailable(npu *PhysicalNpuState) bool {
	for _, s := range npu.AvailableSlices {
		if s.SliceID == npu.DeviceName {
			return true
		}
	}
	return false
}

func (d *driver) syncAllocatable() {
	deviceNames := d.getAvailableDeviceNames()

	availableMap := make(map[string]struct{}, len(deviceNames))
	for _, name := range deviceNames {
		availableMap[name] = struct{}{}
	}

	for k := range d.state.allocatable {
		if _, ok := availableMap[k]; !ok {
			delete(d.state.allocatable, k)
		}
	}
}

func (d *driver) getAvailableDeviceNames() []string {
	var deviceNames []string
	if d.state.vnpuManager != nil {
		for _, physicalNpu := range d.state.vnpuManager.PhysicalNpus {
			for _, slice := range physicalNpu.AvailableSlices {
				deviceNames = append(deviceNames, slice.SliceID)
			}
			for _, slice := range physicalNpu.AllocatedSlices {
				deviceNames = append(deviceNames, slice.SliceID)
			}
		}
	}

	return deviceNames
}

// updateSupportTemplates updates the set of templates supported by the physical NPU.
func (m *VnpuManager) updateSupportTemplates(npu *PhysicalNpuState) {
	// If no slices are allocated, support all templates.
	if len(npu.AllocatedSlices) == 0 {
		npu.SupportTemplates = cloneTemplates(m.Templates)
		return
	}
	// Otherwise, filter templates as needed.
	npu.SupportTemplates = make(map[string]*VnpuTemplate)
	for name, tpl := range m.Templates {
		// For example, keep only "vir01" when any slice is allocated.
		if strings.HasPrefix(name, "vir01") {
			npu.SupportTemplates[name] = tpl
		}
	}
}

// cloneTemplates performs a shallow copy of the templates.
func cloneTemplates(src map[string]*VnpuTemplate) map[string]*VnpuTemplate {
	dst := make(map[string]*VnpuTemplate, len(src))
	for k, v := range src {
		copied := *v
		dst[k] = &copied
	}
	return dst
}
