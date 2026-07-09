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
	"context"

	"ascend-common/common-utils/hwlog"
)

const defaultAscendTesterLogFile = "/var/log/hami-dra-driver/ascend-dra-tester.log"

func initAscendCommonLogger(logFile string) error {
	return hwlog.InitRunLogger(&hwlog.LogConfig{
		LogFileName:   logFile,
		OnlyToFile:    true,
		LogLevel:      0,
		FileMaxSize:   hwlog.DefaultFileMaxSize,
		MaxBackups:    hwlog.DefaultMaxBackups,
		MaxAge:        hwlog.DefaultMinSaveAge,
		ExpiredTime:   0,
		CacheSize:     0,
		IsCompress:    false,
		MaxLineLength: 0,
	}, context.Background())
}
