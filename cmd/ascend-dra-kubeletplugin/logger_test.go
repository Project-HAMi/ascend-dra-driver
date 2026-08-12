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

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"ascend-common/common-utils/hwlog"
)

func TestInitAscendCommonLoggerInitializesRunLogWithoutStdout(t *testing.T) {
	hwlog.RunLog = nil
	t.Cleanup(func() {
		hwlog.RunLog = nil
	})

	logDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	logFile := filepath.Join(logDir, "ascend-dra-kubeletplugin.log")

	originalStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = writePipe

	err = initAscendCommonLogger(logFile)

	if closeErr := writePipe.Close(); closeErr != nil {
		t.Fatalf("close stdout writer: %v", closeErr)
	}
	os.Stdout = originalStdout

	var stdout bytes.Buffer
	if _, copyErr := io.Copy(&stdout, readPipe); copyErr != nil {
		t.Fatalf("read stdout: %v", copyErr)
	}
	if closeErr := readPipe.Close(); closeErr != nil {
		t.Fatalf("close stdout reader: %v", closeErr)
	}

	if err != nil {
		t.Fatalf("init logger: %v", err)
	}
	if hwlog.RunLog == nil {
		t.Fatal("RunLog is nil")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout log output, got %q", stdout.String())
	}
	if _, err := os.Stat(logFile); err != nil {
		t.Fatalf("expected log file to be created: %v", err)
	}
}
