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

// Package npuutil provides utility helpers for parsing and canonicalizing
// Ascend NPU device names used by the DRA driver.
package npuutil

import (
	"fmt"
	"strconv"
	"strings"
)

// DeviceName represents the canonical name of an NPU device or vNPU slice.
//
// The DRA driver uses two naming forms:
//   - "npu-<logicID>" for a full physical NPU card.
//   - "npu-<logicID>-<sliceID>" for a vNPU slice created from a physical card.
type DeviceName struct {
	LogicID int32
	SliceID int32
}

// ParseDeviceName parses a canonical NPU device name.
// It accepts both "npu-X" (full card) and "npu-X-Y" (vNPU slice) formats.
func ParseDeviceName(name string) (DeviceName, error) {
	const prefix = "npu-"
	if !strings.HasPrefix(name, prefix) {
		return DeviceName{}, fmt.Errorf("device name %q does not start with %q", name, prefix)
	}

	rest := strings.TrimPrefix(name, prefix)
	if rest == "" {
		return DeviceName{}, fmt.Errorf("device name %q has no ID", name)
	}

	parts := strings.Split(rest, "-")
	if len(parts) < 1 || len(parts) > 2 {
		return DeviceName{}, fmt.Errorf("invalid device name %q: expected npu-<logicID> or npu-<logicID>-<sliceID>", name)
	}

	logicID, err := strconv.Atoi(parts[0])
	if err != nil {
		return DeviceName{}, fmt.Errorf("invalid logic ID in device name %q: %w", name, err)
	}

	sliceID := 0
	if len(parts) == 2 {
		sliceID, err = strconv.Atoi(parts[1])
		if err != nil {
			return DeviceName{}, fmt.Errorf("invalid slice ID in device name %q: %w", name, err)
		}
	}

	return DeviceName{
		LogicID: int32(logicID),
		SliceID: int32(sliceID),
	}, nil
}

// IsSlice reports whether the device name represents a vNPU slice.
func (d DeviceName) IsSlice() bool {
	return d.SliceID != 0
}

// VisibleDevice returns the string used for ASCEND_VISIBLE_DEVICES.
// For both full cards and slices this is the physical logic ID.
func (d DeviceName) VisibleDevice() string {
	return strconv.Itoa(int(d.LogicID))
}

// EnvSuffix returns the suffix used for per-device environment variables such
// as NPU_DEVICE_<suffix>_SHARING_STRATEGY. For full cards this is
// "<logicID>", for slices it is "<logicID>_<sliceID>".
func (d DeviceName) EnvSuffix() string {
	if d.IsSlice() {
		return fmt.Sprintf("%d_%d", d.LogicID, d.SliceID)
	}
	return strconv.Itoa(int(d.LogicID))
}

// String returns the canonical device name.
func (d DeviceName) String() string {
	if d.IsSlice() {
		return fmt.Sprintf("npu-%d-%d", d.LogicID, d.SliceID)
	}
	return fmt.Sprintf("npu-%d", d.LogicID)
}
