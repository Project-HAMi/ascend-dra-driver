/*
 * Copyright 2025 The Kubernetes Authors.
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

package v1alpha1

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNpuConfigNormalize(t *testing.T) {
	tests := map[string]struct {
		npuConfig   *NpuConfig
		expected    *NpuConfig
		expectedErr error
	}{
		"nil NpuConfig": {
			npuConfig:   nil,
			expectedErr: errors.New("config is 'nil'"),
		},
		"empty NpuConfig": {
			npuConfig: &NpuConfig{},
			expected: &NpuConfig{
				Sharing: &NpuSharing{
					Strategy: TimeSlicingStrategy,
					TimeSlicingConfig: &TimeSlicingConfig{
						Interval: DefaultTimeSlice,
					},
				},
			},
		},
		"empty NpuConfig with SpacePartitioning": {
			npuConfig: &NpuConfig{
				Sharing: &NpuSharing{
					Strategy: SpacePartitioningStrategy,
				},
			},
			expected: &NpuConfig{
				Sharing: &NpuSharing{
					Strategy: SpacePartitioningStrategy,
					SpacePartitioningConfig: &SpacePartitioningConfig{
						PartitionCount: 1,
					},
				},
			},
		},
		"full NpuConfig": {
			npuConfig: &NpuConfig{
				Sharing: &NpuSharing{
					Strategy: SpacePartitioningStrategy,
					TimeSlicingConfig: &TimeSlicingConfig{
						Interval: ShortTimeSlice,
					},
					SpacePartitioningConfig: &SpacePartitioningConfig{
						PartitionCount: 5,
					},
				},
			},
			expected: &NpuConfig{
				Sharing: &NpuSharing{
					Strategy: SpacePartitioningStrategy,
					TimeSlicingConfig: &TimeSlicingConfig{
						Interval: ShortTimeSlice,
					},
					SpacePartitioningConfig: &SpacePartitioningConfig{
						PartitionCount: 5,
					},
				},
			},
		},
		"default NpuConfig is already normalized": {
			npuConfig: DefaultNpuConfig(),
			expected:  DefaultNpuConfig(),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := test.npuConfig.Normalize()
			assert.Equal(t, test.expected, test.npuConfig)
			assert.Equal(t, test.expectedErr, err)
		})
	}
}
