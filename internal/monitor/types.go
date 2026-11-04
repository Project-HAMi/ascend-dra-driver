// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0

// Package monitor exports Device Plugin compatible HAMi core metrics.
package monitor

import (
	"context"
	"time"
)

// Domain is one prepared Claim's accounting domain on a physical NPU.
type Domain struct {
	ClaimUID, Pool, Device, UUID string
	Generation                   uint64
	PhysicalID                   int
	MemoryLimit                  uint64
	Supported                    bool
}

// PreparedSnapshot is an immutable copy of the driver's prepared state.
type PreparedSnapshot struct {
	Domains []Domain
}

// DeviceRef identifies a device in PodResources' DRA results.
type DeviceRef struct{ Pool, Device string }

// Consumer identifies a Claim's container, with duplicate request references merged.
type Consumer struct {
	Namespace, Pod, Container string
	Devices                   map[DeviceRef]bool
}

// HostDevice separates valid hardware readings from failed queries.
type HostDevice struct {
	LogicalID, PhysicalID  int
	UUID, Type             string
	MemoryUsed             uint64
	Utilization            float64
	MemoryValid, UtilValid bool
}

// Sources are the monitor's read-only dependencies. Host may block in the SDK;
// the monitor permits only one outstanding call and never waits for it on shutdown.
type Sources struct {
	Prepared   func() (PreparedSnapshot, error)
	Generation func(claimUID string) uint64 // Zero means no prepared lifetime.
	Host       func(context.Context) ([]HostDevice, error)
	Workloads  func(context.Context) (map[string][]Consumer, error)
	Close      func()
}

// Config controls the HTTP listener and bounded-age sampling cache.
type Config struct {
	BindAddress, ClaimsRoot string
	Interval, MaxAge        time.Duration
}

const (
	DefaultInterval = 5 * time.Second
	DefaultMaxAge   = 15 * time.Second
)
