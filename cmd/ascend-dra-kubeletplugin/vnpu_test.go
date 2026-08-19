package main

import (
	"fmt"
	"sync"
	"testing"
)

func TestVNPUManagerAllocatesAndReleasesMultipleSlices(t *testing.T) {
	manager := newVNPUManager(createDefaultTemplates())
	manager.InitPhysicalNPU("npu-0-0", 0, 0, "Ascend910A")

	first, err := manager.AllocateSlice("npu-0-0", 4, 8)
	if err != nil {
		t.Fatalf("allocate first slice: %v", err)
	}
	second, err := manager.AllocateSlice("npu-0-1", 4, 8)
	if err != nil {
		t.Fatalf("allocate second slice: %v", err)
	}

	state, found := manager.PhysicalNPU("npu-0-0")
	if !found {
		t.Fatal("physical NPU not found")
	}
	assertSliceState(t, &state, first.SliceID, true)
	assertSliceState(t, &state, second.SliceID, true)
	assertSliceType(t, &state, first.SliceID, "vNPU")
	assertSliceType(t, &state, second.SliceID, "vNPU")
	assertSliceState(t, &state, "npu-0-2", false)

	if err := manager.ReleaseSlice(first.SliceID); err != nil {
		t.Fatalf("release first slice: %v", err)
	}
	state, _ = manager.PhysicalNPU("npu-0-0")
	assertSliceMissing(t, &state, first.SliceID)
	assertSliceState(t, &state, second.SliceID, true)
	assertSliceState(t, &state, "npu-0-3", false)

	if err := manager.ReleaseSlice(second.SliceID); err != nil {
		t.Fatalf("release second slice: %v", err)
	}
	state, _ = manager.PhysicalNPU("npu-0-0")
	if len(state.AvailableSlices) != 1 || len(state.AllocatedSlices) != 0 {
		t.Fatalf("expected one restored full-card slice, got %d available and %d allocated",
			len(state.AvailableSlices), len(state.AllocatedSlices))
	}
	assertSliceState(t, &state, "npu-0-0", false)
	if state.AvailableSlices[0].Type != "NPU" {
		t.Fatalf("expected restored full-card type NPU, got %q", state.AvailableSlices[0].Type)
	}
}

func TestVNPUManagerSnapshotsAreIndependent(t *testing.T) {
	manager := newVNPUManager(createDefaultTemplates())
	manager.InitPhysicalNPU("npu-0-0", 0, 0, "Ascend910A")

	snapshot, found := manager.PhysicalNPU("npu-0-0")
	if !found {
		t.Fatal("physical NPU not found")
	}
	snapshot.AvailableSlices[0].Allocated = true
	snapshot.SupportTemplates["vir01"].Attributes.Memory = 0

	actual, _ := manager.PhysicalNPU("npu-0-0")
	if actual.AvailableSlices[0].Allocated {
		t.Fatal("mutating a slice snapshot changed manager state")
	}
	if actual.SupportTemplates["vir01"].Attributes.Memory == 0 {
		t.Fatal("mutating a template snapshot changed manager state")
	}
}

func TestVNPUManagerConcurrentSnapshotsAndMutations(t *testing.T) {
	manager := newVNPUManager(createDefaultTemplates())
	manager.InitPhysicalNPU("npu-0-0", 0, 0, "Ascend910A")

	start := make(chan struct{})
	errors := make(chan error, 5)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 100; i++ {
			slice, err := manager.AllocateSlice("npu-0-0", 4, 8)
			if err != nil {
				errors <- fmt.Errorf("allocate iteration %d: %w", i, err)
				return
			}
			if err := manager.ReleaseSlice(slice.SliceID); err != nil {
				errors <- fmt.Errorf("release iteration %d: %w", i, err)
				return
			}
		}
	}()

	for reader := 0; reader < 4; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 100; i++ {
				if snapshots := manager.PhysicalNPUSnapshots(); len(snapshots) != 1 {
					errors <- fmt.Errorf("snapshot iteration %d returned %d physical NPUs", i, len(snapshots))
					return
				}
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func assertSliceState(t *testing.T, state *PhysicalNPUState, sliceID string, allocated bool) {
	t.Helper()
	for _, slice := range allSlices(state) {
		if slice.SliceID == sliceID {
			if slice.Allocated != allocated {
				t.Fatalf("slice %s allocated=%t, want %t", sliceID, slice.Allocated, allocated)
			}
			return
		}
	}
	t.Fatalf("slice %s not found", sliceID)
}

func assertSliceMissing(t *testing.T, state *PhysicalNPUState, sliceID string) {
	t.Helper()
	for _, slice := range allSlices(state) {
		if slice.SliceID == sliceID {
			t.Fatalf("slice %s unexpectedly exists", sliceID)
		}
	}
}

func assertSliceType(t *testing.T, state *PhysicalNPUState, sliceID, sliceType string) {
	t.Helper()
	for _, slice := range allSlices(state) {
		if slice.SliceID == sliceID {
			if slice.Type != sliceType {
				t.Fatalf("slice %s type=%q, want %q", sliceID, slice.Type, sliceType)
			}
			return
		}
	}
	t.Fatalf("slice %s not found", sliceID)
}

func allSlices(state *PhysicalNPUState) []*VNPUSlice {
	slices := make([]*VNPUSlice, 0, len(state.AvailableSlices)+len(state.AllocatedSlices))
	slices = append(slices, state.AvailableSlices...)
	slices = append(slices, state.AllocatedSlices...)
	return slices
}
