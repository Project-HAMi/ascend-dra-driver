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

package common

// GetTemplateName2DeviceTypeMap returns the Ascend virtual-device template
// name to device-type mapping used by the DRA driver.
func GetTemplateName2DeviceTypeMap() map[string]string {
	return map[string]string{
		Vir01:        "Ascend310P-1c",
		Vir02:        "Ascend310P-2c",
		Vir04:        "Ascend310P-4c",
		Vir08:        "Ascend310P-8c",
		Vir16:        "Ascend310P-16c",
		Vir04C3:      "Ascend310P-4c.3cpu",
		Vir02C1:      "Ascend310P-2c.1cpu",
		Vir04C4Dvpp:  "Ascend310P-4c.4cpu.dvpp",
		Vir04C3Ndvpp: "Ascend310P-4c.3cpu.ndvpp",
	}
}
