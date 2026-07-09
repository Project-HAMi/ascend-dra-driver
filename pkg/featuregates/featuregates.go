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

const projectMajorMinor = "v0.1"

const (
	// HAMivNPUCore enables libvnpu based consumable-capacity sharing for
	// Ascend NPUs. When enabled, physical NPUs are advertised as shareable
	// DRA devices and the libvnpu runtime is injected through CDI edits.
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

// FeatureGates returns the shared feature gate registry for project and
// Kubernetes logging feature gates.
func FeatureGates() featuregate.MutableVersionedFeatureGate {
	featureGatesOnce.Do(func() {
		featureGates = newFeatureGates()
	})
	return featureGates
}

func newFeatureGates() featuregate.MutableVersionedFeatureGate {
	fg := featuregate.NewFeatureGate()

	utilruntime.Must(logsapi.AddFeatureGates(fg))
	utilruntime.Must(fg.SetFromMap(map[string]bool{string(logsapi.ContextualLogging): true}))
	utilruntime.Must(fg.AddVersioned(defaultFeatureGates))

	return fg
}

func Enabled(feature featuregate.Feature) bool {
	return FeatureGates().Enabled(feature)
}

func KnownFeatures() []string {
	return FeatureGates().KnownFeatures()
}
