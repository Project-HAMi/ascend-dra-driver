/*
 * Copyright 2024 The Kubernetes Authors.
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
	"fmt"
)

// These constants represent the different Sharing strategies.
const (
	TimeSlicingStrategy       NpuSharingStrategy = "TimeSlicing"
	SpacePartitioningStrategy NpuSharingStrategy = "SpacePartitioning"
)

// These constants represent the different TimeSlicing configurations.
const (
	DefaultTimeSlice TimeSliceInterval = "Default"
	ShortTimeSlice   TimeSliceInterval = "Short"
	MediumTimeSlice  TimeSliceInterval = "Medium"
	LongTimeSlice    TimeSliceInterval = "Long"
)

// NpuSharingStrategy defines the valid Sharing strategies as a string.
type NpuSharingStrategy string

// TimeSliceInterval defines the valid timeslice interval as a string.
type TimeSliceInterval string

// NpuSharing holds the current sharing strategy for NPUs and its settings.
// If DeviceClass and ResourceClaim set this, then the strategy from the claim
// is used. If multiple configurations set this, then the last one is used.
type NpuSharing struct {
	Strategy                NpuSharingStrategy       `json:"strategy"`
	TimeSlicingConfig       *TimeSlicingConfig       `json:"timeSlicingConfig,omitempty"`
	SpacePartitioningConfig *SpacePartitioningConfig `json:"spacePartitioningConfig,omitempty"`
}

// TimeSlicingSettings provides the settings for the TimeSlicing strategy.
type TimeSlicingConfig struct {
	Interval TimeSliceInterval `json:"interval,omitempty"`
}

// SpacePartitioningConfig provides the configuring for the SpacePartitioning strategy.
type SpacePartitioningConfig struct {
	// SliceCount indicates how many equally sized (memory and compute) slices
	// the NPU should be divided into. Each client that attaches will get
	// access to exactly one of these slices.
	PartitionCount int `json:"partitionCount,omitempty"`
}

// IsTimeSlicing checks if the TimeSlicing strategy is applied.
func (s *NpuSharing) IsTimeSlicing() bool {
	if s == nil {
		return false
	}
	return s.Strategy == TimeSlicingStrategy
}

// IsSpacePartitioning checks if the SpacePartitioning strategy is applied.
func (s *NpuSharing) IsSpacePartitioning() bool {
	if s == nil {
		return false
	}
	return s.Strategy == SpacePartitioningStrategy
}

// GetTimeSlicingConfig returns the timeslicing config that applies to the given strategy.
func (s *NpuSharing) GetTimeSlicingConfig() (*TimeSlicingConfig, error) {
	if s == nil {
		return nil, fmt.Errorf("no sharing set to get config from")
	}
	if s.Strategy != TimeSlicingStrategy {
		return nil, fmt.Errorf("strategy is not set to '%v'", TimeSlicingStrategy)
	}
	if s.SpacePartitioningConfig != nil {
		return nil, fmt.Errorf("cannot use SpacePartitioningConfig with the '%v' strategy", TimeSlicingStrategy)
	}
	return s.TimeSlicingConfig, nil
}

// GetSpacePartitioningConfig returns the SpacePartitioning config that applies to the given strategy.
func (s *NpuSharing) GetSpacePartitioningConfig() (*SpacePartitioningConfig, error) {
	if s == nil {
		return nil, fmt.Errorf("no sharing set to get config from")
	}
	if s.Strategy != SpacePartitioningStrategy {
		return nil, fmt.Errorf("strategy is not set to '%v'", SpacePartitioningStrategy)
	}
	if s.TimeSlicingConfig != nil {
		return nil, fmt.Errorf("cannot use TimeSlicingConfig with the '%v' strategy", SpacePartitioningStrategy)
	}
	return s.SpacePartitioningConfig, nil
}
