// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0

package monitor

import (
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var hostLabels = []string{"device_index", "device_uuid", "device_type"}
var containerLabels = []string{"namespace", "pod", "container", "vdevice_index", "device_uuid"}

const containerMetricStart = 2

// Names, HELP and labels intentionally match ascend-device-plugin ab41bec.
var business = []*prometheus.Desc{
	prometheus.NewDesc("hami_host_gpu_memory_used_bytes", "GPU device memory usage in bytes", hostLabels, nil),
	prometheus.NewDesc("hami_host_gpu_utilization_ratio", "GPU core utilization ratio (0-100)", hostLabels, nil),
	prometheus.NewDesc("hami_vgpu_memory_used_bytes", "vGPU device memory usage in bytes", containerLabels, nil),
	prometheus.NewDesc("hami_vgpu_memory_limit_bytes", "vGPU device memory limit in bytes", containerLabels, nil),
	prometheus.NewDesc("hami_container_device_utilization_ratio", "Container device SM utilization ratio", containerLabels, nil),
	prometheus.NewDesc("hami_vgpu_memory_context_bytes", "Container device memory context size in bytes", containerLabels, nil),
	prometheus.NewDesc("hami_vgpu_memory_module_bytes", "Container device memory module size in bytes", containerLabels, nil),
	prometheus.NewDesc("hami_vgpu_memory_buffer_bytes", "Container device memory buffer size in bytes", containerLabels, nil),
}

var sourceSuccess = prometheus.NewDesc("ascend_dra_monitor_source_success", "Whether the latest source collection succeeded", []string{"source"}, nil)
var lastSuccess = prometheus.NewDesc("ascend_dra_monitor_last_success_timestamp_seconds", "Unix time of the last successful source collection", []string{"source"}, nil)
var domainCount = prometheus.NewDesc("ascend_dra_monitor_domains", "Number of prepared physical-card domains by observation state", []string{"state"}, nil)
var states = []string{"pending", "ready", "unmapped", "ambiguous", "invalid", "stale", "unsupported"}

type sample struct {
	desc   int
	value  float64
	labels []string
	at     time.Time
}

type observation struct {
	samples []sample
	counts  map[string]int
	ok      map[string]bool
	last    map[string]time.Time
	at      time.Time
	gen     uint64
}

type collector struct {
	sync.RWMutex
	current    observation
	maxAge     time.Duration
	generation func() uint64
	now        func() time.Time
}

func (c *collector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range business {
		ch <- d
	}
	ch <- sourceSuccess
	ch <- lastSuccess
	ch <- domainCount
}

func (c *collector) Collect(ch chan<- prometheus.Metric) {
	c.RLock()
	o := c.current
	c.RUnlock()
	now, gen := c.now(), c.generation()
	stale := now.Sub(o.at) > c.maxAge || o.gen != gen
	for _, s := range o.samples {
		if now.Sub(s.at) > c.maxAge || (s.desc >= containerMetricStart && o.gen != gen) {
			continue
		}
		ch <- prometheus.MustNewConstMetric(business[s.desc], prometheus.GaugeValue, s.value, s.labels...)
	}
	for _, source := range []string{"devices", "shmem", "workload"} {
		value := 0.0
		if o.ok[source] && !o.last[source].IsZero() && now.Sub(o.last[source]) <= c.maxAge {
			value = 1
		}
		ch <- prometheus.MustNewConstMetric(sourceSuccess, prometheus.GaugeValue, value, source)
		stamp := 0.0
		if !o.last[source].IsZero() {
			stamp = float64(o.last[source].Unix())
		}
		ch <- prometheus.MustNewConstMetric(lastSuccess, prometheus.GaugeValue, stamp, source)
	}
	for _, state := range states {
		count := o.counts[state]
		if stale {
			count = 0
			if state == "stale" {
				for _, n := range o.counts {
					count += n
				}
			}
		}
		ch <- prometheus.MustNewConstMetric(domainCount, prometheus.GaugeValue, float64(count), state)
	}
}

func deviceType(name string) string {
	if strings.HasPrefix(name, "Ascend-") || strings.HasPrefix(name, "NVIDIA-") {
		return name
	}
	return "Ascend-" + name
}
