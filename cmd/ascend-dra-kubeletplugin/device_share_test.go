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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Project-HAMi/hami-dra-driver/pkg/featuregates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordedDeviceShareCall struct {
	cardID   int32
	deviceID int32
	enable   bool
}

type fakeDeviceShareRunner struct {
	calls  []recordedDeviceShareCall
	failAt int
}

func (f *fakeDeviceShareRunner) SetDeviceShare(cardID, deviceID int32, enable bool) error {
	f.calls = append(f.calls, recordedDeviceShareCall{
		cardID:   cardID,
		deviceID: deviceID,
		enable:   enable,
	})
	if f.failAt > 0 && len(f.calls) == f.failAt {
		return errors.New("npu-smi failed")
	}
	return nil
}

func TestEnableHAMivNPUDeviceShareEnablesEachPhysicalChipOnce(t *testing.T) {
	enableHAMivNPUCore(t)
	manager := newStubManager()
	stub := manager.mgr.(*stubDeviceManager)
	stub.deviceList = []int32{0, 1, 2}
	stub.cardDeviceIDMap = map[int32][2]int32{
		0: {4, 0},
		1: {5, 0},
		2: {4, 0},
	}
	runner := &fakeDeviceShareRunner{}

	err := enableHAMivNPUDeviceShare(manager, runner)

	require.NoError(t, err)
	assert.Equal(t, []recordedDeviceShareCall{
		{cardID: 4, deviceID: 0, enable: true},
		{cardID: 5, deviceID: 0, enable: true},
	}, runner.calls)
}

func TestEnableHAMivNPUDeviceShareFailsFast(t *testing.T) {
	enableHAMivNPUCore(t)
	manager := newStubManager()
	stub := manager.mgr.(*stubDeviceManager)
	stub.deviceList = []int32{0, 1, 2}
	stub.cardDeviceIDMap = map[int32][2]int32{
		0: {4, 0},
		1: {5, 0},
		2: {6, 0},
	}
	runner := &fakeDeviceShareRunner{failAt: 2}

	err := enableHAMivNPUDeviceShare(manager, runner)

	require.ErrorContains(t, err, "enable device-share for card 5 chip 0")
	assert.Len(t, runner.calls, 2)
}

func TestEnableHAMivNPUDeviceShareIsNoopWhenFeatureDisabled(t *testing.T) {
	require.NoError(t, featuregates.FeatureGates().Set("HAMivNPUCore=false"))
	manager := newStubManager()
	runner := &fakeDeviceShareRunner{}

	err := enableHAMivNPUDeviceShare(manager, runner)

	require.NoError(t, err)
	assert.Empty(t, runner.calls)
}

func TestNPUSmiDeviceShareRunnerConfirmsInteractiveEnable(t *testing.T) {
	tempDir := t.TempDir()
	capturePath := filepath.Join(tempDir, "capture")
	scriptPath := filepath.Join(tempDir, "npu-smi")
	script := `#!/bin/sh
read confirmation
printf '%s\n%s\n' "$*" "$confirmation" > "$NPU_SMI_CAPTURE"
`
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))
	t.Setenv("NPU_SMI_CAPTURE", capturePath)

	runner := &npuSmiDeviceShareRunner{path: scriptPath}
	require.NoError(t, runner.SetDeviceShare(4, 0, true))

	content, err := os.ReadFile(capturePath)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"set -t device-share -i 4 -c 0 -d 1",
		"Y",
	}, strings.Split(strings.TrimSpace(string(content)), "\n"))
}
