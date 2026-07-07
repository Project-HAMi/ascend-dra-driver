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

// Package common contains common types used across the Ascend DRA driver.
package common

import (
	"github.com/fsnotify/fsnotify"
	"k8s.io/apimachinery/pkg/util/sets"
)

// NodeDeviceInfoCache captures a snapshot of node NPU device information.
type NodeDeviceInfoCache struct {
	DeviceInfo NodeDeviceInfo
	CheckCode  string
}

// NodeDeviceInfo records per-node NPU device information.
type NodeDeviceInfo struct {
	DeviceList map[string]string
	UpdateTime int64
}

// DeviceHealth represents the health status of a device.
type DeviceHealth struct {
	Health        string
	NetworkHealth string
}

// NPUAllInfo aggregates all NPU devices detected on a node.
type NPUAllInfo struct {
	AllDevTypes []string
	AllDevs     []NPUDevice
	AICoreDevs  []*NPUDevice
}

// NPUDevice describes a physical or virtual NPU device.
type NPUDevice struct {
	DevType    string
	DeviceName string
	LogicID    int32
	PhyID      int32
	CardID     int32
}

// DavinciDev describes a DaVinci NPU device as reported by the DCMI interface.
type DavinciDev struct {
	LogicID int32
	PhyID   int32
	CardID  int32
}

// Device identifies a single allocated device inside an Instance annotation.
type Device struct {
	DeviceID string `json:"device_id"`
	DeviceIP string `json:"device_ip"`
}

// Instance is used to record pod-to-device mappings in annotations.
type Instance struct {
	PodName  string   `json:"pod_name"`
	ServerID string   `json:"server_id"`
	Devices  []Device `json:"devices"`
}

// Option holds runtime configuration options for the Ascend device manager.
type Option struct {
	GetFdFlag          bool
	UseAscendDocker    bool
	UseVolcanoType     bool
	AutoStowingDevs    bool
	PresetVDevice      bool
	Use310PMixedInsert bool
	ListAndWatchPeriod int
	HotReset           int
	AICoreCount        int32
	BuildScene         string
	ProductTypes       []string
	RealCardType       string
}

// FileWatch wraps a fsnotify watcher for socket files.
type FileWatch struct {
	FileWatcher *fsnotify.Watcher
}

// DevStatusSet groups devices by their health state.
type DevStatusSet struct {
	UnHealthyDevice    sets.String
	NetUnHealthyDevice sets.String
	FreeHealthyDevice  map[string]sets.String
}
