// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"ascend-common/devmanager/common"
	"ascend-common/devmanager/dcmi"

	"github.com/Project-HAMi/hami-dra-driver/internal/monitor"
	"github.com/Project-HAMi/hami-dra-driver/pkg/consts"
)

// Legacy checkpoint edits cannot be reused with the new runtime library.
func preparedCoreQuota(devices PreparedDevices) (uint64, bool) {
	env := make(map[string]string)
	runtimeMount := false
	for _, device := range devices {
		if device.ContainerEdits == nil || device.ContainerEdits.ContainerEdits == nil {
			continue
		}
		for _, entry := range device.ContainerEdits.Env {
			key, value, found := strings.Cut(entry, "=")
			if found {
				if old, exists := env[key]; exists && old != value {
					return 0, false
				}
				env[key] = value
			}
		}
		for _, mount := range device.ContainerEdits.Mounts {
			if mount.ContainerPath == "/hami-vnpu-core" && mount.HostPath == consts.CoreRuntimePath {
				runtimeMount = true
			}
		}
	}
	for _, name := range []string{"NPU_MEM_QUOTA", "NPU_PRIORITY", "NPU_FIXED_SHARE_RATIO", "NPU_OVERDRAFT_ENABLED"} {
		if _, exists := env[name]; exists {
			return 0, false
		}
	}
	quota, err := strconv.ParseUint(env["NPU_MEMORY_LIMIT_MB"], 10, 64)
	compute, computeErr := strconv.ParseUint(env["NPU_COMPUTE_LIMIT"], 10, 64)
	ok := err == nil && computeErr == nil && quota > 0 && quota <= math.MaxUint64/(1024*1024) && compute >= 1 && compute <= 100 && runtimeMount &&
		env["NPU_SCHED_POLICY"] == "dynamic" && env["NPU_GLOBAL_SHM_PATH"] == "/hami-shared-region/global_registry" &&
		env["NPU_LOCAL_SHM_PATH"] == libvNPULocalShmemContainerPath+"/"+libvNPULocalShmemFileName
	return quota * 1024 * 1024, ok
}

func (s *DeviceState) metricsSnapshot() (monitor.PreparedSnapshot, error) {
	s.Lock()
	defer s.Unlock()
	result := monitor.PreparedSnapshot{Generation: s.monitorGeneration.Load()}
	// Prepared state is restored once at startup and maintained under this lock
	// by lifecycle operations. Monitoring does no checkpoint I/O under the lock.
	for uid, devices := range s.prepared {
		quota, supported := preparedCoreQuota(devices)
		for _, prepared := range devices {
			device, exists := s.allocatable[prepared.DeviceName]
			if !exists {
				continue
			}
			physical, uuid := device.Attributes[consts.DeviceAttributePhysicalID], device.Attributes[consts.DeviceAttributeUUID]
			if physical.IntValue == nil || uuid.StringValue == nil || *physical.IntValue < 0 {
				return result, fmt.Errorf("missing metrics identity for %s", prepared.DeviceName)
			}
			result.Domains = append(result.Domains, monitor.Domain{
				ClaimUID: uid, Pool: prepared.PoolName, Device: prepared.DeviceName,
				PhysicalID: int(*physical.IntValue), UUID: *uuid.StringValue, MemoryLimit: quota, Supported: supported,
			})
		}
	}
	return result, nil
}

func startMetrics(ctx context.Context, cfg *Config, state *DeviceState) (*monitor.Monitor, error) {
	resolver, err := monitor.NewWorkloadResolver(ctx, cfg.coreclient, cfg.flags.nodeName, cfg.flags.podResourcesSocket)
	if err != nil {
		return nil, err
	}
	source := monitor.Sources{
		Prepared: state.metricsSnapshot, Generation: state.monitorGeneration.Load,
		Workloads: resolver.Resolve, Close: resolver.Close,
		Host: func(ctx context.Context) ([]monitor.HostDevice, error) {
			return collectMetricsHost(ctx, state.manager, dcmi.FuncDcmiGetDeviceHbmInfo)
		},
	}
	m, err := monitor.Start(ctx, monitor.Config{
		BindAddress: cfg.flags.metricsBindAddress, ClaimsRoot: filepath.Join(state.libvNPUHostPath, "containers"),
		Interval: cfg.flags.metricsInterval, MaxAge: cfg.flags.metricsMaxAge,
	}, source)
	if err != nil {
		resolver.Close()
	}
	return m, err
}

func collectMetricsHost(ctx context.Context, manager *AscendManager, hbm func(int32, int32) (*common.HbmInfo, error)) ([]monitor.HostDevice, error) {
	_, ids, err := manager.mgr.GetDeviceList()
	if err != nil {
		return nil, err
	}
	var devices []monitor.HostDevice
	var failures []error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return devices, err
		}
		physical, err := manager.mgr.GetPhysicIDFromLogicID(id)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		uuid, err := manager.getDeviceUUID(id)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		d := monitor.HostDevice{LogicalID: int(id), PhysicalID: int(physical), UUID: uuid}
		d.Type, _ = manager.mgr.GetProductType(id)
		if strings.TrimSpace(d.Type) == "" {
			if chip, err := manager.mgr.GetChipInfo(id); err == nil && chip != nil {
				d.Type = chip.Name
			}
		}
		card, chip, coordErr := manager.mgr.GetCardIDDeviceID(id)
		if coordErr == nil {
			if mem, err := hbm(card, chip); err == nil && mem != nil && mem.MemorySize > 0 && mem.Usage <= mem.MemorySize && mem.Usage <= math.MaxUint64/(1024*1024) {
				d.MemoryUsed, d.MemoryValid = mem.Usage*1024*1024, true
			}
		}
		if !d.MemoryValid {
			if mem, err := manager.mgr.GetDeviceMemoryInfo(id); err == nil && mem != nil && mem.MemorySize > 0 && mem.MemoryAvailable <= mem.MemorySize && mem.MemorySize <= math.MaxUint64/(1024*1024) {
				d.MemoryUsed, d.MemoryValid = (mem.MemorySize-mem.MemoryAvailable)*1024*1024, true
			}
		}
		if util, err := manager.mgr.GetDeviceUtilizationRate(id, common.AICore); err == nil && util <= 100 {
			d.Utilization, d.UtilValid = float64(util), true
		}
		if !d.MemoryValid || !d.UtilValid {
			failures = append(failures, fmt.Errorf("incomplete hardware metrics for physical NPU %d", physical))
		}
		devices = append(devices, d)
	}
	return devices, errors.Join(failures...)
}
