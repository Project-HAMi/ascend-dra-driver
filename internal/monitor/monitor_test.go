// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0

package monitor

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func sampleMonitor(t *testing.T) (*Monitor, *PreparedSnapshot, *map[string][]Consumer, time.Time) {
	t.Helper()
	now := time.Unix(1234567890, 0)
	d := Domain{ClaimUID: "claim", Generation: 1, Pool: "node", Device: "npu-1-0", PhysicalID: 3, UUID: "uuid-3", MemoryLimit: 1 << 30, Supported: true}
	p := &PreparedSnapshot{Domains: []Domain{d}}
	w := map[string][]Consumer{"claim": {{Namespace: "ns", Pod: "pod", Container: "app", Devices: map[DeviceRef]bool{{Pool: "node", Device: "npu-1-0"}: true}}}}
	m := &Monitor{
		config: Config{ClaimsRoot: t.TempDir(), Interval: time.Second, MaxAge: 15 * time.Second},
		last:   map[string]time.Time{"devices": now}, reported: make(map[string]string), hostOK: true,
		host: hostResult{at: now, devices: []HostDevice{{LogicalID: 1, PhysicalID: 3, UUID: "uuid-3", Type: "910B", MemoryUsed: 2 << 30, Utilization: 73, MemoryValid: true, UtilValid: true}}},
	}
	m.src = Sources{
		Prepared: func() (PreparedSnapshot, error) {
			snapshot := *p
			snapshot.Domains = append([]Domain(nil), p.Domains...)
			return snapshot, nil
		},
		Generation: func(uid string) uint64 {
			for _, d := range p.Domains {
				if d.ClaimUID == uid {
					return d.Generation
				}
			}
			return 0
		},
		Workloads: func(context.Context) (map[string][]Consumer, error) { return w, nil },
	}
	m.c = &collector{maxAge: m.config.MaxAge, generation: m.src.Generation, now: func() time.Time { return now }}
	data := coreFixture()
	setSlot(data, 0, 11, 4096, 1)
	setSlot(data, 1, 22, 8192, 1)
	writeDomain(t, m.config.ClaimsRoot, d, data)
	return m, p, &w, now
}

func countObservedDomains(o observation, state string) int {
	count := 0
	for _, d := range o.domains {
		if d.state == state {
			count++
		}
	}
	return count
}

func gather(t *testing.T, c prometheus.Collector) string {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	require.NoError(t, reg.Register(c))
	families, err := reg.Gather()
	require.NoError(t, err)
	var b strings.Builder
	for _, f := range families {
		b.WriteString(f.GetName())
		b.WriteByte('\n')
	}
	return b.String()
}

func TestRefreshThrottlesSourcesWithoutExtendingFreshness(t *testing.T) {
	m, prepared, _, now := sampleMonitor(t)
	resolve, snapshot := m.src.Workloads, m.src.Prepared
	workloadCalls, preparedCalls := 0, 0
	m.src.Workloads = func(ctx context.Context) (map[string][]Consumer, error) {
		workloadCalls++
		return resolve(ctx)
	}
	m.src.Prepared = func() (PreparedSnapshot, error) {
		preparedCalls++
		return snapshot()
	}
	m.refresh(context.Background(), now)
	before := m.c.current
	data := coreFixture()
	setSlot(data, 0, 11, 65536, 1)
	writeDomain(t, m.config.ClaimsRoot, prepared.Domains[0], data)

	for _, elapsed := range []time.Duration{0, m.config.Interval / 2, m.config.Interval - time.Nanosecond} {
		m.refresh(context.Background(), now.Add(elapsed))
		require.Equal(t, 1, workloadCalls)
		require.Equal(t, 1, preparedCalls)
		require.Equal(t, before, m.c.current, "skipping must preserve sample timestamps and source status")
		require.Equal(t, now, m.workloadAt)
		require.Equal(t, now, m.last["workload"])
		require.Equal(t, now, m.last["shmem"])
	}

	m.refresh(context.Background(), now.Add(m.config.Interval))
	require.Equal(t, 2, workloadCalls)
	require.Equal(t, 2, preparedCalls)
	require.Equal(t, now.Add(m.config.Interval), m.c.current.at)
	expected := `
# HELP hami_vgpu_memory_used_bytes vGPU device memory usage in bytes
# TYPE hami_vgpu_memory_used_bytes gauge
hami_vgpu_memory_used_bytes{container="app",device_uuid="uuid-3",namespace="ns",pod="pod",vdevice_index="3"} 65536
`
	require.NoError(t, testutil.CollectAndCompare(m.c, strings.NewReader(expected), "hami_vgpu_memory_used_bytes"))
}

func TestRefreshThrottlesFailedAttempts(t *testing.T) {
	m, _, _, now := sampleMonitor(t)
	calls := 0
	m.src.Workloads = func(context.Context) (map[string][]Consumer, error) {
		calls++
		return nil, errors.New("PodResources unavailable")
	}
	m.refresh(context.Background(), now)
	require.False(t, m.c.current.ok["workload"])
	require.Zero(t, m.c.current.last["workload"])
	m.refresh(context.Background(), now.Add(m.config.Interval/2))
	require.Equal(t, 1, calls, "failed attempts must also limit repeated RPCs")
	m.refresh(context.Background(), now.Add(m.config.Interval))
	require.Equal(t, 2, calls)
}

func TestRunRetainsHostResultsWhenRefreshIsSkipped(t *testing.T) {
	m, _, _, _ := sampleMonitor(t)
	m.config.Interval, m.config.MaxAge = time.Hour, 2*time.Hour
	m.done = make(chan struct{})
	resolve := m.src.Workloads
	calls := 0
	m.src.Workloads = func(ctx context.Context) (map[string][]Consumer, error) {
		calls++
		return resolve(ctx)
	}
	ctx, cancel := context.WithCancel(context.Background())
	results := make(chan hostResult)
	go m.run(ctx, results)
	t.Cleanup(func() { cancel(); <-m.done })
	for _, used := range []uint64{111, 222, 333} {
		select {
		case results <- hostResult{at: time.Now(), devices: []HostDevice{{PhysicalID: 3, UUID: "uuid-3", MemoryUsed: used, MemoryValid: true}}}:
		case <-time.After(time.Second):
			t.Fatal("run did not consume host results")
		}
	}
	cancel()
	<-m.done
	require.Equal(t, 1, calls, "nearby host events must not repeat a full refresh")
	require.Equal(t, uint64(333), m.host.devices[0].MemoryUsed)
	require.Equal(t, float64(111), m.c.current.samples[0].value)

	// Once the interval passes, the next refresh must publish the newest host
	// result, including results received while the previous snapshot was kept.
	m.refresh(context.Background(), m.c.current.at.Add(m.config.Interval))
	require.Equal(t, 2, calls)
	require.Equal(t, float64(333), m.c.current.samples[0].value)
}

func TestMetricsMatchDevicePluginContract(t *testing.T) {
	m, _, _, now := sampleMonitor(t)
	m.sample(context.Background(), now)
	const labels = `container="app",device_uuid="uuid-3",namespace="ns",pod="pod",vdevice_index="3"`
	const host = `device_index="1",device_type="Ascend-910B",device_uuid="uuid-3"`
	expected := `
# HELP hami_host_gpu_memory_used_bytes GPU device memory usage in bytes
# TYPE hami_host_gpu_memory_used_bytes gauge
hami_host_gpu_memory_used_bytes{` + host + `} 2.147483648e+09
# HELP hami_host_gpu_utilization_ratio GPU core utilization ratio (0-100)
# TYPE hami_host_gpu_utilization_ratio gauge
hami_host_gpu_utilization_ratio{` + host + `} 73
# HELP hami_vgpu_memory_used_bytes vGPU device memory usage in bytes
# TYPE hami_vgpu_memory_used_bytes gauge
hami_vgpu_memory_used_bytes{` + labels + `} 12288
# HELP hami_vgpu_memory_limit_bytes vGPU device memory limit in bytes
# TYPE hami_vgpu_memory_limit_bytes gauge
hami_vgpu_memory_limit_bytes{` + labels + `} 1.073741824e+09
# HELP hami_container_device_utilization_ratio Container device SM utilization ratio
# TYPE hami_container_device_utilization_ratio gauge
hami_container_device_utilization_ratio{` + labels + `} 73
# HELP hami_vgpu_memory_context_bytes Container device memory context size in bytes
# TYPE hami_vgpu_memory_context_bytes gauge
hami_vgpu_memory_context_bytes{` + labels + `} 0
# HELP hami_vgpu_memory_module_bytes Container device memory module size in bytes
# TYPE hami_vgpu_memory_module_bytes gauge
hami_vgpu_memory_module_bytes{` + labels + `} 0
# HELP hami_vgpu_memory_buffer_bytes Container device memory buffer size in bytes
# TYPE hami_vgpu_memory_buffer_bytes gauge
hami_vgpu_memory_buffer_bytes{` + labels + `} 0
`
	names := []string{}
	for _, name := range []string{"host_gpu_memory_used_bytes", "host_gpu_utilization_ratio", "vgpu_memory_used_bytes", "vgpu_memory_limit_bytes", "container_device_utilization_ratio", "vgpu_memory_context_bytes", "vgpu_memory_module_bytes", "vgpu_memory_buffer_bytes"} {
		names = append(names, "hami_"+name)
	}
	require.NoError(t, testutil.CollectAndCompare(m.c, strings.NewReader(expected), names...))
	require.Equal(t, 1, countObservedDomains(m.c.current, "ready"))
}

func TestUniqueConsumerConstraintAndRepeatedReferences(t *testing.T) {
	m, _, w, now := sampleMonitor(t)
	(*w)["claim"] = append((*w)["claim"], (*w)["claim"][0])
	m.sample(context.Background(), now)
	require.Equal(t, 1, countObservedDomains(m.c.current, "ready"), "same container with repeated references is one consumer")
	second := (*w)["claim"][0]
	second.Container = "other"
	(*w)["claim"] = append((*w)["claim"], second)
	m.sample(context.Background(), now)
	require.Equal(t, 1, countObservedDomains(m.c.current, "ambiguous"))
	output := gather(t, m.c)
	require.NotContains(t, output, "hami_vgpu_memory_used_bytes")
	require.Contains(t, output, "hami_host_gpu_memory_used_bytes")
}

func TestPendingInvalidAndStaleSamplesAreNotZero(t *testing.T) {
	m, p, _, now := sampleMonitor(t)
	p.Domains[0].PhysicalID = 4 // Missing identity must not silently reuse physical 3.
	m.sample(context.Background(), now)
	require.Equal(t, 1, countObservedDomains(m.c.current, "stale"))
	p.Domains[0].PhysicalID = 3
	p.Domains[0].ClaimUID = "not-started"
	consumers := m.workloads["claim"]
	m.src.Workloads = func(context.Context) (map[string][]Consumer, error) {
		return map[string][]Consumer{"not-started": consumers}, nil
	}
	m.sample(context.Background(), now)
	require.Equal(t, 1, countObservedDomains(m.c.current, "pending"))
	require.NotContains(t, gather(t, m.c), "hami_vgpu_memory_used_bytes")
	writeDomain(t, m.config.ClaimsRoot, p.Domains[0], []byte{1})
	m.sample(context.Background(), now)
	require.Equal(t, 1, countObservedDomains(m.c.current, "pending"))
}

func TestShortFileGraceExpiresAndRecoversWithoutCachedUsage(t *testing.T) {
	m, p, _, now := sampleMonitor(t)
	m.sample(context.Background(), now)
	require.Contains(t, gather(t, m.c), "hami_vgpu_memory_used_bytes")
	writeDomain(t, m.config.ClaimsRoot, p.Domains[0], []byte{1})
	for _, elapsed := range []time.Duration{0, m.config.Interval / 2, m.config.Interval} {
		m.sample(context.Background(), now.Add(elapsed))
		output := gather(t, m.c)
		require.NotContains(t, output, "hami_vgpu_")
		require.NotContains(t, output, "hami_container_")
		require.Contains(t, output, "hami_host_gpu_memory_used_bytes")
		if elapsed < m.config.Interval {
			require.Equal(t, 1, countObservedDomains(m.c.current, "pending"))
			require.True(t, m.c.current.ok["shmem"])
		}
	}
	require.Equal(t, 1, countObservedDomains(m.c.current, "invalid"))
	require.False(t, m.c.current.ok["shmem"])
	data := coreFixture()
	setSlot(data, 0, 11, 4096, 1)
	writeDomain(t, m.config.ClaimsRoot, p.Domains[0], data)
	m.sample(context.Background(), now.Add(2*m.config.Interval))
	require.Equal(t, 1, countObservedDomains(m.c.current, "ready"))
	require.True(t, m.c.current.ok["shmem"])
	expected := `
# HELP hami_vgpu_memory_used_bytes vGPU device memory usage in bytes
# TYPE hami_vgpu_memory_used_bytes gauge
hami_vgpu_memory_used_bytes{container="app",device_uuid="uuid-3",namespace="ns",pod="pod",vdevice_index="3"} 4096
`
	require.NoError(t, testutil.CollectAndCompare(m.c, strings.NewReader(expected), "hami_vgpu_memory_used_bytes"))
	writeDomain(t, m.config.ClaimsRoot, p.Domains[0], []byte{1})
	m.sample(context.Background(), now.Add(3*m.config.Interval))
	require.Equal(t, 1, countObservedDomains(m.c.current, "pending"), "a recovered file must not retain its previous failure history")
}

func TestGenerationRemovalAndFreshnessSuppressOldSeries(t *testing.T) {
	m, p, _, now := sampleMonitor(t)
	m.sample(context.Background(), now)
	require.Contains(t, gather(t, m.c), "hami_vgpu_memory_used_bytes")
	p.Domains[0].Generation++
	output := gather(t, m.c)
	require.NotContains(t, output, "hami_vgpu_")
	require.NotContains(t, output, "hami_container_")
	require.Contains(t, output, "hami_host_gpu_memory_used_bytes")
	require.Contains(t, output, "hami_host_gpu_utilization_ratio")
	expected := `
# HELP ascend_dra_monitor_domains Number of prepared physical-card domains by observation state
# TYPE ascend_dra_monitor_domains gauge
ascend_dra_monitor_domains{state="pending"} 0
ascend_dra_monitor_domains{state="ready"} 0
ascend_dra_monitor_domains{state="unmapped"} 0
ascend_dra_monitor_domains{state="ambiguous"} 0
ascend_dra_monitor_domains{state="invalid"} 0
ascend_dra_monitor_domains{state="stale"} 1
ascend_dra_monitor_domains{state="unsupported"} 0
`
	require.NoError(t, testutil.CollectAndCompare(m.c, strings.NewReader(expected), "ascend_dra_monitor_domains"))
	p.Domains = nil
	m.sample(context.Background(), now)
	require.NotContains(t, gather(t, m.c), "hami_vgpu_memory_used_bytes")
	m.c.now = func() time.Time { return now.Add(time.Minute) }
	require.NotContains(t, gather(t, m.c), "hami_host_gpu_memory_used_bytes")
}

func TestLabelCollisionSuppressesBothClaims(t *testing.T) {
	m, p, w, now := sampleMonitor(t)
	d := p.Domains[0]
	d.ClaimUID = "second"
	p.Domains = append(p.Domains, d)
	(*w)["second"] = (*w)["claim"]
	m.sample(context.Background(), now)
	require.Equal(t, 2, countObservedDomains(m.c.current, "ambiguous"))
	require.NotContains(t, gather(t, m.c), "hami_vgpu_memory_used_bytes")
}

func TestMultipleCardsAndIndependentClaimsKeepTheirAccounting(t *testing.T) {
	m, p, w, now := sampleMonitor(t)
	secondCard := p.Domains[0]
	secondCard.Device, secondCard.PhysicalID, secondCard.UUID = "npu-0-0", 7, "uuid-7"
	p.Domains = append(p.Domains, secondCard)
	(*w)["claim"][0].Devices[DeviceRef{Pool: "node", Device: secondCard.Device}] = true
	m.host.devices = append(m.host.devices, HostDevice{LogicalID: 0, PhysicalID: 7, UUID: "uuid-7", Type: "910B", MemoryValid: true, UtilValid: true})
	data := coreFixture()
	setSlot(data, 0, 11, 32768, 1)
	writeDomain(t, m.config.ClaimsRoot, secondCard, data)
	other := p.Domains[0]
	other.ClaimUID, other.MemoryLimit = "other-claim", 2<<30
	p.Domains = append(p.Domains, other)
	consumer := (*w)["claim"][0]
	consumer.Pod = "other-pod"
	(*w)["other-claim"] = []Consumer{consumer}
	data = coreFixture()
	binary.LittleEndian.PutUint64(data, 2<<30)
	writeDomain(t, m.config.ClaimsRoot, other, data)
	m.sample(context.Background(), now)
	require.Equal(t, 3, countObservedDomains(m.c.current, "ready"))
	expected := `
# HELP hami_vgpu_memory_used_bytes vGPU device memory usage in bytes
# TYPE hami_vgpu_memory_used_bytes gauge
hami_vgpu_memory_used_bytes{container="app",device_uuid="uuid-3",namespace="ns",pod="pod",vdevice_index="3"} 12288
hami_vgpu_memory_used_bytes{container="app",device_uuid="uuid-7",namespace="ns",pod="pod",vdevice_index="7"} 32768
hami_vgpu_memory_used_bytes{container="app",device_uuid="uuid-3",namespace="ns",pod="other-pod",vdevice_index="3"} 0
`
	require.NoError(t, testutil.CollectAndCompare(m.c, strings.NewReader(expected), "hami_vgpu_memory_used_bytes"))
}

func TestGenerationChangeDuringSamplingCannotPublishRemovedDomain(t *testing.T) {
	m, p, _, now := sampleMonitor(t)
	prepared := m.src.Prepared
	m.src.Prepared = func() (PreparedSnapshot, error) {
		snapshot, err := prepared()
		p.Domains[0].Generation++
		return snapshot, err
	}
	m.sample(context.Background(), now)
	output := gather(t, m.c)
	require.NotContains(t, output, "hami_vgpu_")
	require.NotContains(t, output, "hami_container_")
	require.Contains(t, output, "hami_host_gpu_memory_used_bytes")
	require.Contains(t, output, "hami_host_gpu_utilization_ratio")
	m.src.Prepared = prepared
	m.sample(context.Background(), now)
	require.Contains(t, gather(t, m.c), "hami_vgpu_memory_used_bytes", "a fresh generation must resume collection")
}

func TestClaimGenerationChangesOnlyInvalidateThatClaimsDomains(t *testing.T) {
	m, p, workloads, now := sampleMonitor(t)
	card := p.Domains[0]
	card.PhysicalID, card.UUID, card.Device = 7, "uuid-7", "npu-0-0"
	p.Domains = append(p.Domains, card)
	(*workloads)["claim"][0].Devices[DeviceRef{Pool: "node", Device: card.Device}] = true
	m.host.devices = append(m.host.devices, HostDevice{PhysicalID: 7, UUID: "uuid-7", MemoryValid: true, UtilValid: true})
	writeDomain(t, m.config.ClaimsRoot, card, coreFixture())
	other := p.Domains[0]
	other.ClaimUID = "other"
	p.Domains = append(p.Domains, other)
	consumer := (*workloads)["claim"][0]
	consumer.Pod = "unrelated"
	(*workloads)["other"] = []Consumer{consumer}
	writeDomain(t, m.config.ClaimsRoot, other, coreFixture())
	m.sample(context.Background(), now)

	// Change one Claim's lifetime after its snapshot was captured. Both its
	// cards must disappear, while all six metrics of the other Claim survive.
	prepared := m.src.Prepared
	m.src.Prepared = func() (PreparedSnapshot, error) {
		snapshot, err := prepared()
		for i := range p.Domains {
			if p.Domains[i].ClaimUID == "claim" {
				p.Domains[i].Generation++
			}
		}
		return snapshot, err
	}
	m.sample(context.Background(), now)
	assertOnlyUnrelated := func() {
		reg := prometheus.NewPedanticRegistry()
		require.NoError(t, reg.Register(m.c))
		families, err := reg.Gather()
		require.NoError(t, err)
		containers := 0
		for _, f := range families {
			if !strings.HasPrefix(f.GetName(), "hami_") || strings.HasPrefix(f.GetName(), "hami_host_") {
				continue
			}
			containers++
			require.Len(t, f.Metric, 1)
			for _, label := range f.Metric[0].Label {
				if label.GetName() == "pod" {
					require.Equal(t, "unrelated", label.GetValue())
				}
			}
		}
		require.Equal(t, 6, containers)
	}
	assertOnlyUnrelated()
	assertCounts := func(stale int) {
		expected := "# HELP ascend_dra_monitor_domains Number of prepared physical-card domains by observation state\n# TYPE ascend_dra_monitor_domains gauge\n"
		for _, state := range states {
			count := 0
			switch state {
			case "ready":
				count = 1
			case "stale":
				count = stale
			}
			expected += fmt.Sprintf("ascend_dra_monitor_domains{state=%q} %d\n", state, count)
		}
		require.NoError(t, testutil.CollectAndCompare(m.c, strings.NewReader(expected), "ascend_dra_monitor_domains"))
	}
	assertCounts(2)

	// Removal must not leave obsolete prepared counts or resurrect old samples.
	p.Domains = p.Domains[2:]
	assertOnlyUnrelated()
	assertCounts(0)
	require.Contains(t, gather(t, m.c), "hami_host_gpu_memory_used_bytes")
}

func TestWorkloadFailureExpiresAndDoesNotInventConsumers(t *testing.T) {
	m, _, _, now := sampleMonitor(t)
	m.sample(context.Background(), now)
	m.src.Workloads = func(context.Context) (map[string][]Consumer, error) { return nil, errors.New("unavailable") }
	m.sample(context.Background(), now.Add(time.Second))
	require.False(t, m.c.current.ok["workload"])
	m.sample(context.Background(), now.Add(time.Minute))
	require.NotContains(t, gather(t, m.c), "hami_vgpu_memory_used_bytes")
}

func TestCollectChecksWorkloadOwnershipAge(t *testing.T) {
	for _, tc := range []struct {
		name             string
		age              time.Duration
		missingTimestamp bool
		blockedRefresh   bool
		expired          bool
	}{
		{name: "at_max_age", age: 15 * time.Second},
		{name: "past_max_age", age: 15*time.Second + time.Nanosecond, expired: true},
		{name: "missing_timestamp", age: 14 * time.Second, missingTimestamp: true, expired: true},
		{name: "blocked_refresh", age: 15*time.Second + time.Nanosecond, blockedRefresh: true, expired: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, prepared, _, start := sampleMonitor(t)
			unsupported := prepared.Domains[0]
			unsupported.ClaimUID, unsupported.Supported = "unsupported", false
			prepared.Domains = append(prepared.Domains, unsupported)
			m.sample(context.Background(), start)
			resolve := m.src.Workloads
			m.src.Workloads = func(context.Context) (map[string][]Consumer, error) {
				return nil, errors.New("PodResources unavailable")
			}
			// Refresh SHM and hardware while the cached ownership is still valid.
			// The next scrape must use ownership age, not these new sample ages.
			m.host.at = start.Add(14 * time.Second)
			m.sample(context.Background(), m.host.at)
			if tc.missingTimestamp {
				delete(m.c.current.last, "workload")
			}
			if tc.blockedRefresh {
				entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
				snapshot := m.src.Prepared
				m.src.Prepared = func() (PreparedSnapshot, error) {
					close(entered)
					<-release
					return snapshot()
				}
				go func() {
					defer close(done)
					m.sample(context.Background(), start.Add(15*time.Second))
				}()
				t.Cleanup(func() { close(release); <-done })
				<-entered
			}
			m.c.now = func() time.Time { return start.Add(tc.age) }
			assertMetrics := func(expired bool) {
				t.Helper()
				output := gather(t, m.c)
				require.Contains(t, output, "hami_host_gpu_memory_used_bytes")
				require.Contains(t, output, "hami_host_gpu_utilization_ratio")
				for _, name := range []string{
					"hami_vgpu_memory_used_bytes", "hami_vgpu_memory_limit_bytes",
					"hami_container_device_utilization_ratio", "hami_vgpu_memory_context_bytes",
					"hami_vgpu_memory_module_bytes", "hami_vgpu_memory_buffer_bytes",
				} {
					if expired {
						require.NotContains(t, output, name)
					} else {
						require.Contains(t, output, name)
					}
				}
				ready, stale := 1, 0
				if expired {
					ready, stale = 0, 1
				}
				expected := "# HELP ascend_dra_monitor_domains Number of prepared physical-card domains by observation state\n# TYPE ascend_dra_monitor_domains gauge\n"
				for _, state := range states {
					count := map[string]int{"ready": ready, "stale": stale, "unsupported": 1}[state]
					expected += fmt.Sprintf("ascend_dra_monitor_domains{state=%q} %d\n", state, count)
				}
				require.NoError(t, testutil.CollectAndCompare(m.c, strings.NewReader(expected), "ascend_dra_monitor_domains"))
			}
			assertMetrics(tc.expired)
			if !tc.blockedRefresh {
				m.src.Workloads = resolve
				m.host.at = start.Add(tc.age)
				m.sample(context.Background(), m.host.at)
				assertMetrics(false)
			}
		})
	}
}

func TestHTTPClosesAndBlockedHardwareDoesNotSpawnReplacements(t *testing.T) {
	var calls atomic.Int32
	blocked := make(chan struct{})
	defer close(blocked)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := Sources{
		Prepared: func() (PreparedSnapshot, error) { return PreparedSnapshot{}, nil }, Generation: func(string) uint64 { return 0 },
		Workloads: func(context.Context) (map[string][]Consumer, error) { return nil, nil },
		Host:      func(context.Context) ([]HostDevice, error) { calls.Add(1); <-blocked; return nil, nil },
	}
	cfg := Config{BindAddress: "127.0.0.1:0", ClaimsRoot: t.TempDir(), Interval: 5 * time.Millisecond, MaxAge: time.Second}
	m, err := Start(ctx, cfg, src)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return calls.Load() == 1 }, time.Second, time.Millisecond)
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + m.Address() + "/metrics")
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Contains(t, string(body), "ascend_dra_monitor_source_success")
	require.NotContains(t, string(body), "go_goroutines")
	require.Equal(t, int32(1), calls.Load())
	closeCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, m.Close(closeCtx))
	_, err = client.Get("http://" + m.Address() + "/metrics")
	require.Error(t, err)
}
