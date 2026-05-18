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

package featuregates

import (
	"sync"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/component-base/featuregate"
	logsapi "k8s.io/component-base/logs/api/v1"
)

// Project version for feature gate lifecycle management.
const projectMajorMinor = "v0.1"

const (
	// HAMivNPUCore enables HAMi libvnpu consumable capacity mode for Ascend NPU
	// sharing. When enabled, physical NPUs are advertised with Capacity and
	// AllowMultipleAllocations, and libvnpu runtime sharing is injected via CDI.
	// When disabled, the driver falls back to pre-sliced static vNPU allocation.
	HAMivNPUCore featuregate.Feature = "HAMivNPUCore"
)

var defaultFeatureGates = map[featuregate.Feature]featuregate.VersionedSpecs{
	HAMivNPUCore: {
		{
			Default:    false,
			PreRelease: featuregate.Alpha,
			Version:    version.MustParse(projectMajorMinor),
		},
	},
}

var (
	featureGatesOnce sync.Once
	featureGates     featuregate.MutableVersionedFeatureGate
)

// FeatureGates returns the package-level singleton representing all feature
// gates (both project-specific and standard Kubernetes logging feature gates).
func FeatureGates() featuregate.MutableVersionedFeatureGate {
	if featureGates == nil {
		featureGatesOnce.Do(func() {
			featureGates = newFeatureGates(version.MustParse(projectMajorMinor))
		})
	}
	return featureGates
}

func newFeatureGates(v *version.Version) featuregate.MutableVersionedFeatureGate {
	fg := featuregate.NewVersionedFeatureGate(v)

	// Add standard Kubernetes logging feature gates
	utilruntime.Must(logsapi.AddFeatureGates(fg))

	// Add project-specific feature gates
	utilruntime.Must(fg.AddVersioned(defaultFeatureGates))

	return fg
}

// Enabled returns true if the specified feature gate is enabled.
func Enabled(feature featuregate.Feature) bool {
	return FeatureGates().Enabled(feature)
}

// KnownFeatures returns a list of known feature gates with their descriptions.
func KnownFeatures() []string {
	return FeatureGates().KnownFeatures()
}
