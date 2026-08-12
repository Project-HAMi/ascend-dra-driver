/* Copyright(C) 2022. Huawei Technologies Co.,Ltd. All rights reserved.
  Licensed under the Apache License, Version 2.0 (the "License");
  you may not use this file except in compliance with the License.
  You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

  Unless required by applicable law or agreed to in writing, software
  distributed under the License is distributed on an "AS IS" BASIS,
  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
  See the License for the specific language governing permissions and
* limitations under the License.
*/

// Package common contains constants and helpers shared across the Ascend DRA driver.
package common

const (
	// MaxVirtualDeviceNum is the maximum number of virtual devices supported per node.
	MaxVirtualDeviceNum = 1024
)

// NPU resource template names used by the Ascend driver.
const (
	// Vir01 template provides 1 AICore / 1 GB memory.
	Vir01 = "vir01"
	// Vir02 template provides 2 AICores / 2 GB memory.
	Vir02 = "vir02"
	// Vir04 template provides 4 AICores / 4 GB memory.
	Vir04 = "vir04"
	// Vir08 template provides 8 AICores / 8 GB memory.
	Vir08 = "vir08"
	// Vir16 template provides 16 AICores / 16 GB memory.
	Vir16 = "vir16"
	// Vir04C3 template provides 4 AICores / 3 GB memory.
	Vir04C3 = "vir04_3c"
	// Vir02C1 template provides 2 AICores / 1 GB memory.
	Vir02C1 = "vir02_1c"
	// Vir04C4Dvpp template provides 4 AICores / 4 CPUs / DVPP.
	Vir04C4Dvpp = "vir04_4c_dvpp"
	// Vir04C3Ndvpp template provides 4 AICores / 3 CPUs / no DVPP.
	Vir04C3Ndvpp = "vir04_3c_ndvpp"
)

// CPU core count suffixes used for naming virtual devices.
const (
	Core1          = "1c"
	Core2          = "2c"
	Core4          = "4c"
	Core8          = "8c"
	Core16         = "16c"
	Core4Cpu3      = "4c.3cpu"
	Core2Cpu1      = "2c.1cpu"
	Core4Cpu4Dvpp  = "4c.4cpu.dvpp"
	Core4Cpu3Ndvpp = "4c.3cpu.ndvpp"
)

// AICore limits.
const (
	// MaxAICoreNum is the maximum number of AICores supported on a single NPU.
	MaxAICoreNum = 32
	// MinAICoreNum is the minimum number of AICores supported on a single NPU.
	MinAICoreNum = 8
)

// Special scene for invoking the dcmi interface.
const (
	// DeviceNotSupport is the DCMI error code returned when the device does not support an operation.
	DeviceNotSupport = 8255
	// DefaultAICoreNum is the fallback AICore count when detection fails.
	DefaultAICoreNum = 1
)
