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

// Package consts defines top-level constants for the Ascend DRA driver.
package consts

// DriverName is the resource driver name used for registration with kubelet
// and inside ResourceSlice / ResourceClaim objects.
const DriverName = "ascend.project-hami.io"

// ResourceSlice attribute names owned by this driver are intentionally
// unqualified. Kubernetes interprets unqualified names as belonging to the
// driver's domain, while consumers such as HAMi-DRA read these map keys
// directly.
const (
	DeviceAttributeIndex       = "index"
	DeviceAttributePhysicalID  = "physicalID"
	DeviceAttributeUUID        = "uuid"
	DeviceAttributeModel       = "model"
	DeviceAttributeProductName = "productName"
	DeviceAttributeBrand       = "brand"
	DeviceAttributeType        = "type"
	DeviceAttributeCores       = "cores"
	DeviceAttributeMemory      = "memory"
)

// ResourceSlice capacity names follow the HAMi-DRA capacity contract.
const (
	DeviceCapacityCores  = "cores"
	DeviceCapacityMemory = "memory"
)

const (
	DeviceBrandHuawei      = "Huawei"
	DeviceTypeNPU          = "NPU"
	DeviceTypeHAMivNPUCore = "HAMivNPUCore"
)

// DeviceClassNamePrefix is the prefix used for dynamically created DeviceClasses.
const DeviceClassNamePrefix = "npu-"

// DeviceClassNameSuffix is the suffix used for dynamically created DeviceClasses.
const DeviceClassNameSuffix = ".project-hami.io"

// TemplateInfoPath is the path to the vNPU template description file on the
// host. It is mounted into the driver container.
const TemplateInfoPath = "/etc/npu/template-info.txt"

// NPUPrefix is the canonical prefix for NPU device names used by the driver.
const NPUPrefix = "npu-"
