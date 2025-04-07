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

// GetTemplateName2DeviceTypeMap get virtual device type by template
func GetTemplateName2DeviceTypeMap() map[string]string {
	return map[string]string{
		Vir16:        Core16,
		Vir08:        Core8,
		Vir04:        Core4,
		Vir02:        Core2,
		Vir01:        Core1,
		Vir04C3:      Core4Cpu3,
		Vir02C1:      Core2Cpu1,
		Vir04C4Dvpp:  Core4Cpu4Dvpp,
		Vir04C3Ndvpp: Core4Cpu3Ndvpp,
	}
}
