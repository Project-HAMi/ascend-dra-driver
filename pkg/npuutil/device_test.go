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

package npuutil

import (
	"testing"
)

func TestParseDeviceName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      DeviceName
		wantErr   bool
		isSlice   bool
		visible   string
		envSuffix string
		canonical string
	}{
		{
			name:      "full card",
			input:     "npu-0",
			want:      DeviceName{LogicID: 0},
			isSlice:   false,
			visible:   "0",
			envSuffix: "0",
			canonical: "npu-0",
		},
		{
			name:      "slice",
			input:     "npu-3-7",
			want:      DeviceName{LogicID: 3, SliceID: 7},
			isSlice:   true,
			visible:   "3",
			envSuffix: "3_7",
			canonical: "npu-3-7",
		},
		{
			name:    "missing prefix",
			input:   "gpu-0",
			wantErr: true,
		},
		{
			name:    "empty after prefix",
			input:   "npu-",
			wantErr: true,
		},
		{
			name:    "too many parts",
			input:   "npu-1-2-3",
			wantErr: true,
		},
		{
			name:    "non-numeric",
			input:   "npu-abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDeviceName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseDeviceName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got != tt.want {
				t.Fatalf("ParseDeviceName(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
			if got.IsSlice() != tt.isSlice {
				t.Fatalf("IsSlice() = %v, want %v", got.IsSlice(), tt.isSlice)
			}
			if got.VisibleDevice() != tt.visible {
				t.Fatalf("VisibleDevice() = %q, want %q", got.VisibleDevice(), tt.visible)
			}
			if got.EnvSuffix() != tt.envSuffix {
				t.Fatalf("EnvSuffix() = %q, want %q", got.EnvSuffix(), tt.envSuffix)
			}
			if got.String() != tt.canonical {
				t.Fatalf("String() = %q, want %q", got.String(), tt.canonical)
			}
		})
	}
}
