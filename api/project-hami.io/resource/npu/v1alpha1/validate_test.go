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

func TestNpuConfigValidate(t *testing.T) {
	tests := map[string]struct {
		npuConfig *NpuConfig
		expected  error
	}{
		"empty NpuConfig": {
			npuConfig: &NpuConfig{},
			expected:  errors.New("no sharing strategy set"),
		},
		"empty NpuConfig.Sharing": {
			npuConfig: &NpuConfig{
				Sharing: &NpuSharing{},
			},
			expected: errors.New("unknown NPU sharing strategy: "),
		},
		"unknown NPU sharing strategy": {
			npuConfig: &NpuConfig{
				Sharing: &NpuSharing{
					Strategy: "unknown",
				},
			},
			expected: errors.New("unknown NPU sharing strategy: unknown"),
		},
		"empty NpuConfig.Sharing.TimeSlicingConfig": {
			npuConfig: &NpuConfig{
				Sharing: &NpuSharing{
					Strategy:          TimeSlicingStrategy,
					TimeSlicingConfig: &TimeSlicingConfig{},
				},
			},
			expected: errors.New("unknown time-slice interval: "),
		},
		"valid NpuConfig with TimeSlicing": {
			npuConfig: &NpuConfig{
				Sharing: &NpuSharing{
					Strategy: TimeSlicingStrategy,
					TimeSlicingConfig: &TimeSlicingConfig{
						Interval: MediumTimeSlice,
					},
				},
			},
			expected: nil,
		},
		"negative NpuConfig.Sharing.SpacePartitioningConfig.PartitionCount": {
			npuConfig: &NpuConfig{
				Sharing: &NpuSharing{
					Strategy: SpacePartitioningStrategy,
					SpacePartitioningConfig: &SpacePartitioningConfig{
						PartitionCount: -1,
					},
				},
			},
			expected: errors.New("invalid partition count: -1"),
		},
		"valid NpuConfig with SpacePartitioning": {
			npuConfig: &NpuConfig{
				Sharing: &NpuSharing{
					Strategy: SpacePartitioningStrategy,
					SpacePartitioningConfig: &SpacePartitioningConfig{
						PartitionCount: 1000,
					},
				},
			},
			expected: nil,
		},
		"default NpuConfig": {
			npuConfig: DefaultNpuConfig(),
			expected:  nil,
		},
		"invalid TimeSlicingConfig ignored with strategy is SpacePartitioning": {
			npuConfig: &NpuConfig{
				Sharing: &NpuSharing{
					Strategy:          SpacePartitioningStrategy,
					TimeSlicingConfig: &TimeSlicingConfig{},
					SpacePartitioningConfig: &SpacePartitioningConfig{
						PartitionCount: 1,
					},
				},
			},
			expected: nil,
		},
		"invalid SpacePartitioningConfig ignored with strategy is TimeSlicing": {
			npuConfig: &NpuConfig{
				Sharing: &NpuSharing{
					Strategy: TimeSlicingStrategy,
					TimeSlicingConfig: &TimeSlicingConfig{
						Interval: MediumTimeSlice,
					},
					SpacePartitioningConfig: &SpacePartitioningConfig{
						PartitionCount: -1,
					},
				},
			},
			expected: nil,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := test.npuConfig.Validate()
			assert.Equal(t, test.expected, err)
		})
	}
}
