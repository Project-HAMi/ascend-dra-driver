package main

import (
	"fmt"
	"os"

	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdiparser "tags.cncf.io/container-device-interface/pkg/parser"
	cdispec "tags.cncf.io/container-device-interface/specs-go"
)

const (
	cdiVendor           = "k8s." + DriverName
	cdiClass            = "npu"
	cdiKind             = cdiVendor + "/" + cdiClass
	cdiCommonDeviceName = "common"
)

type CDIHandler struct {
	cache *cdiapi.Cache
}

func NewCDIHandler(config *Config) (*CDIHandler, error) {
	cache, err := cdiapi.NewCache(
		cdiapi.WithSpecDirs(config.flags.cdiRoot),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create a new CDI cache: %w", err)
	}
	handler := &CDIHandler{
		cache: cache,
	}

	return handler, nil
}

func (cdi *CDIHandler) CreateCommonSpecFile() error {
	spec := &cdispec.Spec{
		Kind: cdiKind,
		Devices: []cdispec.Device{
			{
				Name: cdiCommonDeviceName,
				ContainerEdits: cdispec.ContainerEdits{
					Env: []string{
						fmt.Sprintf("KUBERNETES_NODE_NAME=%s", os.Getenv("NODE_NAME")),
						fmt.Sprintf("DRA_RESOURCE_DRIVER_NAME=%s", DriverName),
					},
				},
			},
		},
	}

	minVersion, err := cdiapi.MinimumRequiredVersion(spec)
	if err != nil {
		return fmt.Errorf("failed to get minimum required CDI spec version: %v", err)
	}
	spec.Version = minVersion

	specName, err := cdiapi.GenerateNameForTransientSpec(spec, cdiCommonDeviceName)
	if err != nil {
		return fmt.Errorf("failed to generate Spec name: %w", err)
	}

	return cdi.cache.WriteSpec(spec, specName)
}

func (cdi *CDIHandler) CreateClaimSpecFile(claimUID string, devices PreparedDevices) error {
	specName := cdiapi.GenerateTransientSpecName(cdiVendor, cdiClass, claimUID)
	var merged cdispec.ContainerEdits
	for _, d := range devices {
		merged.Env = append(merged.Env, d.ContainerEdits.Env...)
		merged.DeviceNodes = append(merged.DeviceNodes, d.ContainerEdits.DeviceNodes...)
		merged.Hooks = append(merged.Hooks, d.ContainerEdits.Hooks...)
		merged.Mounts = append(merged.Mounts, d.ContainerEdits.Mounts...)
	}

	spec := &cdispec.Spec{
		Kind: cdiKind,
		Devices: []cdispec.Device{
			{
				Name:           claimUID,
				ContainerEdits: merged,
			},
		},
	}

	return cdi.cache.WriteSpec(spec, specName)
}

func (cdi *CDIHandler) DeleteClaimSpecFile(claimUID string) error {
	specName := cdiapi.GenerateTransientSpecName(cdiVendor, cdiClass, claimUID)
	return cdi.cache.RemoveSpec(specName)
}

func (cdi *CDIHandler) GetClaimDevices(claimUID string, devices []string) []string {
	cdiDevices := []string{
		cdiparser.QualifiedName(cdiVendor, cdiClass, cdiCommonDeviceName),
		cdiparser.QualifiedName(cdiVendor, cdiClass, claimUID),
	}

	return cdiDevices
}
