// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0

package monitor

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Layout of hami-vnpu-core 98ec21f, Linux little-endian 64-bit repr(C).
// Keep this contract synchronized with crates/limiter/src/shmem/mod.rs.
const (
	shmemPrefixSize = 6224
	processOffset   = 1080
	processStride   = 80
	processCount    = 64
	initializedAt   = 6220
)

var (
	errPending = errors.New("core domain not initialized")
	errShort   = errors.New("incomplete core shared memory")
)

type memoryStats struct{ used, limit uint64 }

func domainPath(root string, d Domain) (string, error) {
	if d.ClaimUID == "" || d.ClaimUID == "." || d.ClaimUID == ".." ||
		filepath.Base(d.ClaimUID) != d.ClaimUID || strings.ContainsAny(d.ClaimUID, `/\\`) || d.PhysicalID < 0 {
		return "", fmt.Errorf("invalid core domain identity")
	}
	return filepath.Join(root, d.ClaimUID, "vnpu_local_shmem.device-"+strconv.Itoa(d.PhysicalID)), nil
}

// Read a private copy rather than mmap: an unexpected truncation must not SIGBUS
// the kubelet plugin. SHM has no transactional snapshot; recheck identities to
// reject observed concurrent slot reuse, without claiming global consistency.
func readMemory(path string) (memoryStats, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return memoryStats{}, errPending
	}
	if err != nil {
		return memoryStats{}, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return memoryStats{}, err
	}
	if !info.Mode().IsRegular() {
		return memoryStats{}, fmt.Errorf("core shared memory is not a regular file")
	}
	if info.Size() < shmemPrefixSize {
		return memoryStats{}, errShort
	}
	for attempt := 0; attempt < 2; attempt++ {
		var data, verify [shmemPrefixSize]byte
		if _, err := f.ReadAt(data[:], 0); err != nil {
			return memoryStats{}, fmt.Errorf("read core shared memory: %w", err)
		}
		if _, err := f.ReadAt(verify[:], 0); err != nil {
			return memoryStats{}, fmt.Errorf("recheck core shared memory: %w", err)
		}
		stats, err := decodeMemory(data[:], verify[:])
		if !errors.Is(err, io.ErrNoProgress) {
			return stats, err
		}
	}
	return memoryStats{}, fmt.Errorf("core slots changed during read")
}

func decodeMemory(data, verify []byte) (memoryStats, error) {
	if len(data) < shmemPrefixSize || len(verify) < shmemPrefixSize {
		return memoryStats{}, errShort
	}
	u32 := binary.LittleEndian.Uint32
	u64 := binary.LittleEndian.Uint64
	init := u32(data[initializedAt:])
	if init == 0 {
		return memoryStats{}, errPending
	}
	if init != 1 {
		return memoryStats{}, fmt.Errorf("invalid core initialized flag %d", init)
	}
	if u32(verify[initializedAt:]) != init || u64(data[:8]) != u64(verify[:8]) {
		return memoryStats{}, io.ErrNoProgress
	}
	stats := memoryStats{limit: u64(data[:8])}
	if stats.limit == 0 {
		return memoryStats{}, fmt.Errorf("zero core quota is unsupported")
	}
	for i := 0; i < processCount; i++ {
		base := processOffset + i*processStride
		pid, active := u32(data[base:]), u32(data[base+72:])
		if pid != u32(verify[base:]) || active != u32(verify[base+72:]) {
			return memoryStats{}, io.ErrNoProgress
		}
		if int32(pid) <= 0 || active != 1 {
			continue
		}
		used := u64(data[base+8:]) // Column zero belongs to this physical-card domain.
		if used > math.MaxUint64-stats.used {
			return memoryStats{}, fmt.Errorf("core usage overflow")
		}
		stats.used += used
	}
	return stats, nil
}
