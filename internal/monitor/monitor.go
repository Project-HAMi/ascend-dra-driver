// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0

package monitor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"
)

type hostResult struct {
	devices []HostDevice
	err     error
	at      time.Time
}

// Monitor owns sampling and the HTTP endpoint. No sampling operation writes
// allocation state, CDI, or core shared memory.
type Monitor struct {
	config Config
	src    Sources
	c      *collector
	server *http.Server
	lis    net.Listener
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once

	host       hostResult
	hostOK     bool
	workloads  map[string][]Consumer
	workloadAt time.Time
	last       map[string]time.Time
	reported   map[string]string
	shortFiles map[string]time.Time
}

// Start binds synchronously, then starts independent hardware and core sampling.
func Start(ctx context.Context, cfg Config, src Sources) (*Monitor, error) {
	if cfg.Interval <= 0 || cfg.MaxAge < cfg.Interval || cfg.ClaimsRoot == "" {
		return nil, fmt.Errorf("invalid metrics interval, maximum age or Claims root")
	}
	if src.Prepared == nil || src.Generation == nil || src.Host == nil || src.Workloads == nil {
		return nil, fmt.Errorf("missing metrics source")
	}
	lis, err := net.Listen("tcp", cfg.BindAddress)
	if err != nil {
		return nil, fmt.Errorf("listen for metrics: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	m := &Monitor{
		config: cfg, src: src, lis: lis, cancel: cancel, done: make(chan struct{}),
		last: make(map[string]time.Time), reported: make(map[string]string),
		c: &collector{maxAge: cfg.MaxAge, generation: src.Generation, now: time.Now},
	}
	reg := prometheus.NewRegistry()
	reg.MustRegister(m.c)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	m.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	go func() {
		if err := m.server.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			klog.ErrorS(err, "Metrics HTTP server stopped")
		}
	}()
	results := make(chan hostResult, 1)
	go m.pollHost(ctx, results)
	go m.run(ctx, results)
	return m, nil
}

// Address is the bound address (also useful with an ephemeral test port).
func (m *Monitor) Address() string { return m.lis.Addr().String() }

// Close releases the listener and cancels samplers. A non-cancellable SDK query
// cannot be killed by Go, but no replacements are spawned while it is blocked.
func (m *Monitor) Close(ctx context.Context) error {
	m.once.Do(func() {
		m.cancel()
		if m.src.Close != nil {
			m.src.Close()
		}
	})
	err := m.server.Shutdown(ctx)
	if err != nil {
		_ = m.server.Close()
	}
	select {
	case <-m.done:
		return err
	case <-ctx.Done():
		return errors.Join(err, ctx.Err())
	}
}

func (m *Monitor) pollHost(ctx context.Context, results chan<- hostResult) {
	for {
		started := time.Now()
		devices, err := m.src.Host(ctx)
		select {
		case results <- hostResult{devices: devices, err: err, at: started}:
		case <-ctx.Done():
			return
		}
		timer := time.NewTimer(m.config.Interval)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}
}

func (m *Monitor) run(ctx context.Context, results <-chan hostResult) {
	defer close(m.done)
	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()
	m.refresh(ctx, time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case result := <-results:
			m.hostOK = result.err == nil
			if result.err == nil || len(result.devices) > 0 {
				m.host = result
			}
			if result.err == nil {
				m.last["devices"] = result.at
				delete(m.reported, "devices")
			} else {
				m.report("devices", result.err.Error())
			}
			m.refresh(ctx, time.Now())
		case now := <-ticker.C:
			m.refresh(ctx, now)
		}
	}
}

func (m *Monitor) report(key, message string) {
	if m.reported[key] != message {
		klog.InfoS("Metrics observation unavailable", "domain", key, "reason", message)
		m.reported[key] = message
	}
}

func (m *Monitor) refresh(ctx context.Context, now time.Time) {
	o := observation{at: now, counts: make(map[string]int), ok: make(map[string]bool), last: make(map[string]time.Time)}
	workCtx, cancel := context.WithTimeout(ctx, m.config.Interval)
	workloads, err := m.src.Workloads(workCtx)
	cancel()
	o.ok["workload"] = err == nil
	if err == nil {
		m.workloads, m.workloadAt, m.last["workload"] = workloads, now, now
		delete(m.reported, "workload")
	} else {
		m.report("workload", err.Error())
	}
	o.ok["devices"] = m.hostOK
	hostByPhysical := make(map[int]HostDevice)
	if !m.host.at.IsZero() && now.Sub(m.host.at) <= m.config.MaxAge {
		for _, h := range m.host.devices {
			hostByPhysical[h.PhysicalID] = h
			labels := []string{strconv.Itoa(h.LogicalID), h.UUID, deviceType(h.Type)}
			if h.MemoryValid {
				o.samples = append(o.samples, sample{desc: 0, value: float64(h.MemoryUsed), labels: labels, at: m.host.at})
			}
			if h.UtilValid {
				o.samples = append(o.samples, sample{desc: 1, value: h.Utilization, labels: labels, at: m.host.at})
			}
		}
	}
	prepared, err := m.src.Prepared()
	o.gen = prepared.Generation
	o.ok["shmem"] = err == nil
	if err != nil {
		m.report("prepared", err.Error())
	} else {
		delete(m.reported, "prepared")
		m.observeDomains(&o, prepared, hostByPhysical, now)
	}
	if o.ok["shmem"] {
		m.last["shmem"] = now
	}
	for source, at := range m.last {
		o.last[source] = at
	}
	// Collect checks this snapshot's generation against the current driver state,
	// so lifecycle changes invalidate all its container samples in one place.
	m.c.Lock()
	m.c.current = o
	m.c.Unlock()
}

func (m *Monitor) observeDomains(o *observation, prepared PreparedSnapshot, hosts map[int]HostDevice, now time.Time) {
	type row struct {
		d Domain
		c Consumer
		h HostDevice
	}
	var rows []row
	shortFiles := make(map[string]time.Time)
	keys := make(map[[5]string]int)
	seen := make(map[[2]string]bool)
	activeClaims := make(map[string]bool)
	for _, d := range prepared.Domains {
		activeClaims[d.ClaimUID] = true
		identity := [2]string{d.ClaimUID, strconv.Itoa(d.PhysicalID)}
		if seen[identity] {
			continue
		}
		seen[identity] = true
		state := ""
		consumers := uniqueConsumers(m.workloads[d.ClaimUID])
		h, exists := hosts[d.PhysicalID]
		switch {
		case !d.Supported:
			state = "unsupported"
		case m.workloadAt.IsZero() || now.Sub(m.workloadAt) > m.config.MaxAge:
			state = "stale"
		case len(consumers) == 0:
			state = "unmapped"
		case len(consumers) > 1:
			state = "ambiguous"
			m.report(d.ClaimUID, "multiple_consumers")
		case !consumers[0].Devices[DeviceRef{Pool: d.Pool, Device: d.Device}]:
			state = "unmapped"
		case !exists || h.UUID != d.UUID || d.UUID == "":
			state = "stale"
		}
		if state != "" {
			o.counts[state]++
			continue
		}
		c := consumers[0]
		key := [5]string{c.Namespace, c.Pod, c.Container, strconv.Itoa(d.PhysicalID), d.UUID}
		keys[key]++
		rows = append(rows, row{d: d, c: c, h: h})
	}
	for _, r := range rows {
		labels := []string{r.c.Namespace, r.c.Pod, r.c.Container, strconv.Itoa(r.d.PhysicalID), r.d.UUID}
		key := [5]string{labels[0], labels[1], labels[2], labels[3], labels[4]}
		if keys[key] > 1 {
			o.counts["ambiguous"]++
			m.report(r.d.ClaimUID, "label_collision")
			continue
		}
		path, err := domainPath(m.config.ClaimsRoot, r.d)
		var memory memoryStats
		if err == nil {
			memory, err = readMemory(path)
		}
		if errors.Is(err, errShort) {
			first, found := m.shortFiles[path]
			if !found {
				first = now
			}
			shortFiles[path] = first
			// Allow one sampling interval for a newly created file to grow.
			if now.Sub(first) < m.config.Interval {
				err = errPending
			}
		}
		if err != nil {
			if errors.Is(err, errPending) {
				o.counts["pending"]++
			} else {
				o.counts["invalid"]++
				o.ok["shmem"] = false
				m.report(r.d.ClaimUID, err.Error())
			}
			continue
		}
		if memory.limit != r.d.MemoryLimit {
			m.report(r.d.ClaimUID, fmt.Sprintf("quota mismatch: core=%d injected=%d", memory.limit, r.d.MemoryLimit))
		} else {
			delete(m.reported, r.d.ClaimUID)
		}
		o.counts["ready"]++
		for i, value := range []float64{float64(memory.used), float64(memory.limit), r.h.Utilization, 0, 0, 0} {
			if i == 2 && !r.h.UtilValid {
				continue
			}
			at := now
			if i == 2 {
				at = m.host.at
			}
			o.samples = append(o.samples, sample{desc: i + containerMetricStart, value: value, labels: labels, at: at})
		}
	}
	m.shortFiles = shortFiles
	for key := range m.reported {
		if key == "devices" || key == "workload" || key == "prepared" {
			continue
		}
		if !activeClaims[key] {
			delete(m.reported, key)
		}
	}
}

func uniqueConsumers(input []Consumer) []Consumer {
	indices := make(map[[3]string]int)
	var output []Consumer
	for _, c := range input {
		key := [3]string{c.Namespace, c.Pod, c.Container}
		i, exists := indices[key]
		if !exists {
			i = len(output)
			indices[key] = i
			copy := c
			copy.Devices = make(map[DeviceRef]bool)
			output = append(output, copy)
		}
		for d := range c.Devices {
			output[i].Devices[d] = true
		}
	}
	return output
}
