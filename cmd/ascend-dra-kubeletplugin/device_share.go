/*
 * Copyright 2026 The HAMi Authors.
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

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/Project-HAMi/hami-dra-driver/pkg/featuregates"
)

var npuSmiCandidates = []string{
	"/usr/local/Ascend/driver/tools/npu-smi",
	"/usr/local/sbin/npu-smi",
	"/usr/local/bin/npu-smi",
}

type physicalChip struct {
	cardID   int32
	deviceID int32
}

type deviceShareRunner interface {
	SetDeviceShare(cardID, deviceID int32, enable bool) error
}

type npuSmiDeviceShareRunner struct {
	path string
}

func newNPUSmiDeviceShareRunner() (*npuSmiDeviceShareRunner, error) {
	for _, candidate := range npuSmiCandidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return &npuSmiDeviceShareRunner{path: candidate}, nil
		}
	}
	path, err := exec.LookPath("npu-smi")
	if err != nil {
		return nil, fmt.Errorf("find npu-smi: %w", err)
	}
	return &npuSmiDeviceShareRunner{path: path}, nil
}

func (r *npuSmiDeviceShareRunner) SetDeviceShare(cardID, deviceID int32, enable bool) error {
	value := "0"
	if enable {
		value = "1"
	}
	cmd := exec.Command(
		r.path,
		"set",
		"-t", "device-share",
		"-i", strconv.FormatInt(int64(cardID), 10),
		"-c", strconv.FormatInt(int64(deviceID), 10),
		"-d", value,
	)
	// Enabling device-share is interactive on supported driver versions.
	// Supplying confirmation also works when the command does not prompt.
	cmd.Stdin = strings.NewReader("Y\n")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", r.path, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func enableHAMivNPUDeviceShare(mgr *AscendManager, runner deviceShareRunner) error {
	if !featuregates.Enabled(featuregates.HAMivNPUCore) {
		return nil
	}

	chips, err := physicalChips(mgr)
	if err != nil {
		return err
	}
	for _, chip := range chips {
		if err := runner.SetDeviceShare(chip.cardID, chip.deviceID, true); err != nil {
			return fmt.Errorf(
				"enable device-share for card %d chip %d: %w",
				chip.cardID,
				chip.deviceID,
				err,
			)
		}
	}
	return nil
}

func physicalChips(mgr *AscendManager) ([]physicalChip, error) {
	_, logicIDs, err := mgr.mgr.GetDeviceList()
	if err != nil {
		return nil, fmt.Errorf("get NPU device list: %w", err)
	}

	seen := make(map[physicalChip]struct{}, len(logicIDs))
	chips := make([]physicalChip, 0, len(logicIDs))
	for _, logicID := range logicIDs {
		cardID, deviceID, err := mgr.mgr.GetCardIDDeviceID(logicID)
		if err != nil {
			return nil, fmt.Errorf("get card and chip IDs for logic ID %d: %w", logicID, err)
		}
		chip := physicalChip{cardID: cardID, deviceID: deviceID}
		if _, exists := seen[chip]; exists {
			continue
		}
		seen[chip] = struct{}{}
		chips = append(chips, chip)
	}
	return chips, nil
}
