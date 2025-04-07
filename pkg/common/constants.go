/* Copyright(C) 2022. Huawei Technologies Co.,Ltd. All rights reserved.
   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

// Package common a series of common function
package common

const (
	MaxVirtualDeviceNum = 1024
)

const (

	// Core1 1 core
	Core1 = "1c"
	// Core2 2 core
	Core2 = "2c"
	// Core4 4 core
	Core4 = "4c"
	// Core8 8 core
	Core8 = "8c"
	// Core16 16 core
	Core16 = "16c"
	// Core4Cpu3 4core 3cpu
	Core4Cpu3 = "4c.3cpu"
	// Core2Cpu1 2core 1cpu
	Core2Cpu1 = "2c.1cpu"
	// Core4Cpu4Dvpp 4core 4cpu dvpp
	Core4Cpu4Dvpp = "4c.4cpu.dvpp"
	// Core4Cpu3Ndvpp 4core 3cpu ndvpp
	Core4Cpu3Ndvpp = "4c.3cpu.ndvpp"

	// Vir01 template name vir01
	Vir01 = "vir01"
	// Vir02 template name vir02
	Vir02 = "vir02"
	// Vir04 template name vir04
	Vir04 = "vir04"
	// Vir08 template name vir08
	Vir08 = "vir08"
	// Vir16 template name vir16
	Vir16 = "vir16"
	// Vir04C3 template name vir04_3c
	Vir04C3 = "vir04_3c"
	// Vir02C1 template name vir02_1c
	Vir02C1 = "vir02_1c"
	// Vir04C4Dvpp template name vir04_4c_dvpp
	Vir04C4Dvpp = "vir04_4c_dvpp"
	// Vir04C3Ndvpp template name vir04_3c_ndvpp
	Vir04C3Ndvpp = "vir04_3c_ndvpp"

	// MaxAICoreNum max ai core num
	MaxAICoreNum = 32
	// MinAICoreNum min ai core num
	MinAICoreNum = 8
)

// Special scene for invoking the dcmi interface
const (
	DeviceNotSupport = 8255
	// DefaultAiCoreNum set a default value of aicore number
	DefaultAiCoreNum = 1
)
