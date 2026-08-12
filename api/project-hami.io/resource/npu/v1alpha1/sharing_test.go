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

func TestNpuSharingGetTimeSlicingConfig(t *testing.T) {
	tests := map[string]struct {
		npuSharing  *NpuSharing
		expected    *TimeSlicingConfig
		expectedErr error
	}{
		"nil NpuSharing": {
			npuSharing:  nil,
			expectedErr: errors.New("no sharing set to get config from"),
		},
		"strategy is not TimeSlicing": {
			npuSharing: &NpuSharing{
				Strategy: SpacePartitioningStrategy,
			},
			expectedErr: errors.New("strategy is not set to 'TimeSlicing'"),
		},
		"non-nil SpacePartitioningConfig": {
			npuSharing: &NpuSharing{
				Strategy:                TimeSlicingStrategy,
				SpacePartitioningConfig: &SpacePartitioningConfig{},
			},
			expectedErr: errors.New("cannot use SpacePartitioningConfig with the 'TimeSlicing' strategy"),
		},
		"valid TimeSlicingConfig": {
			npuSharing: &NpuSharing{
				Strategy: TimeSlicingStrategy,
				TimeSlicingConfig: &TimeSlicingConfig{
					Interval: LongTimeSlice,
				},
			},
			expected: &TimeSlicingConfig{
				Interval: LongTimeSlice,
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			timeSlicing, err := test.npuSharing.GetTimeSlicingConfig()
			assert.Equal(t, test.expected, timeSlicing)
			assert.Equal(t, test.expectedErr, err)
		})
	}

}
func TestNpuSharingGetSpacePartitioningConfig(t *testing.T) {
	tests := map[string]struct {
		npuSharing  *NpuSharing
		expected    *SpacePartitioningConfig
		expectedErr error
	}{
		"nil NpuSharing": {
			npuSharing:  nil,
			expectedErr: errors.New("no sharing set to get config from"),
		},
		"strategy is not SpacePartitioning": {
			npuSharing: &NpuSharing{
				Strategy: TimeSlicingStrategy,
			},
			expectedErr: errors.New("strategy is not set to 'SpacePartitioning'"),
		},
		"non-nil TimeSlicingConfig": {
			npuSharing: &NpuSharing{
				Strategy:          SpacePartitioningStrategy,
				TimeSlicingConfig: &TimeSlicingConfig{},
			},
			expectedErr: errors.New("cannot use TimeSlicingConfig with the 'SpacePartitioning' strategy"),
		},
		"valid SpacePartitioningConfig": {
			npuSharing: &NpuSharing{
				Strategy: SpacePartitioningStrategy,
				SpacePartitioningConfig: &SpacePartitioningConfig{
					PartitionCount: 5,
				},
			},
			expected: &SpacePartitioningConfig{
				PartitionCount: 5,
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			spacePartitioning, err := test.npuSharing.GetSpacePartitioningConfig()
			assert.Equal(t, test.expected, spacePartitioning)
			assert.Equal(t, test.expectedErr, err)
		})
	}
}
