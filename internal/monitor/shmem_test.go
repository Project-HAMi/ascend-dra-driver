// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0

package monitor

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func coreFixture() []byte {
	// Values in this fixture are from the pinned Rust repr(C) ABI,
	// deliberately independent of reader constants.
	data := make([]byte, 6224)
	binary.LittleEndian.PutUint64(data, 1<<30)
	binary.LittleEndian.PutUint32(data[6220:], 1)
	return data
}

func setSlot(data []byte, index int, pid uint32, used uint64, active uint32) {
	base := 1080 + index*80
	binary.LittleEndian.PutUint32(data[base:], pid)
	binary.LittleEndian.PutUint64(data[base+8:], used)
	binary.LittleEndian.PutUint32(data[base+72:], active)
}

func writeDomain(t *testing.T, root string, d Domain, data []byte) string {
	t.Helper()
	path, err := domainPath(root, d)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0750))
	require.NoError(t, os.WriteFile(path, data, 0600))
	return path
}

func TestCoreMemoryAggregatesOnlyActivePerCardColumnZero(t *testing.T) {
	data := coreFixture()
	// memory_used is a different ledger, not the exported per-process sum.
	binary.LittleEndian.PutUint64(data[8:], 9999)
	setSlot(data, 0, 11, 4096, 1)
	setSlot(data, 63, 22, 8192, 1)
	setSlot(data, 1, 0, 100000, 1)
	setSlot(data, 2, 33, 100000, 0)
	setSlot(data, 3, math.MaxUint32, 100000, 1)
	// Nonzero legacy column and active_workers=0 must not change the result.
	binary.LittleEndian.PutUint64(data[processOffset+8+3*8:], 1000000)
	path := writeDomain(t, t.TempDir(), Domain{ClaimUID: "claim", PhysicalID: 3}, data)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	stats, err := readMemory(path)
	require.NoError(t, err)
	require.Equal(t, uint64(12288), stats.used)
	require.Equal(t, uint64(1<<30), stats.limit)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after, "reader must not repair or modify core accounting")
}

func TestCoreMemoryMissingPendingMalformedAndReplacedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "domain")
	_, err := readMemory(path)
	require.ErrorIs(t, err, errPending)
	require.NoError(t, os.WriteFile(path, []byte{1}, 0600))
	_, err = readMemory(path)
	require.ErrorIs(t, err, errShort)
	data := coreFixture()
	binary.LittleEndian.PutUint32(data[initializedAt:], 0)
	require.NoError(t, os.WriteFile(path, data, 0600))
	_, err = readMemory(path)
	require.ErrorIs(t, err, errPending)
	binary.LittleEndian.PutUint32(data[initializedAt:], 7)
	require.NoError(t, os.WriteFile(path, data, 0600))
	_, err = readMemory(path)
	require.ErrorContains(t, err, "invalid core initialized")
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.Symlink("/dev/zero", path))
	_, err = readMemory(path)
	require.Error(t, err, "must not follow a replaced file")
}

func TestCoreMemoryRejectsReuseAndOverflow(t *testing.T) {
	data := coreFixture()
	setSlot(data, 0, 12, 500, 1)
	verify := append([]byte{}, data...)
	binary.LittleEndian.PutUint32(verify[processOffset:], 13)
	_, err := decodeMemory(data, verify)
	require.Error(t, err)
	setSlot(data, 0, 12, math.MaxUint64, 1)
	setSlot(data, 1, 13, 1, 1)
	_, err = decodeMemory(data, data)
	require.ErrorContains(t, err, "overflow")
}

func TestCoreDomainPathsRejectInvalidIdentity(t *testing.T) {
	for _, uid := range []string{"", ".", "..", "../other", "a/b", `a\b`} {
		_, err := domainPath(t.TempDir(), Domain{ClaimUID: uid})
		require.Error(t, err)
	}
	_, err := domainPath(t.TempDir(), Domain{ClaimUID: "claim", PhysicalID: -1})
	require.Error(t, err)
}

func TestConcurrentTruncationDoesNotCrashReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "domain")
	data := coreFixture()
	require.NoError(t, os.WriteFile(path, data, 0600))
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 100; i++ {
			if err := os.WriteFile(path, data, 0600); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 100; i++ {
		// A racing read may fail or see a complete sample; neither may panic.
		_, _ = readMemory(path)
	}
	require.NoError(t, <-done)
}
