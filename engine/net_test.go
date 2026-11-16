package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDevicesDoesNotHoldLockAndCaches(t *testing.T) {
	oldFetch := fetchDevices
	devMu.Lock()
	oldAt, oldList, oldFlight := devAt, devList, devFlight
	devAt, devList, devFlight = time.Time{}, nil, nil
	devMu.Unlock()
	defer func() {
		fetchDevices = oldFetch
		devMu.Lock()
		devAt, devList, devFlight = oldAt, oldList, oldFlight
		devMu.Unlock()
	}()

	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	fetchDevices = func() []Device {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return []Device{{Name: "phone", Online: true, ips: []string{"100.64.0.2"}}}
	}

	var wg sync.WaitGroup
	res := make([][]Device, 2)
	wg.Add(1)
	go func() {
		defer wg.Done()
		res[0] = devices()
	}()
	<-started

	// devMu must NOT be held while fetchDevices is running.
	if !devMu.TryLock() {
		close(release)
		t.Fatal("devMu is held while fetchDevices is running")
	}
	devMu.Unlock()

	// Concurrent second call coalesces onto the in-flight fetch.
	wg.Add(1)
	go func() {
		defer wg.Done()
		res[1] = devices()
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if calls.Load() != 1 {
		t.Fatalf("expected 1 fetchDevices call, got %d", calls.Load())
	}
	if len(res[0]) != 1 || res[0][0].Name != "phone" || len(res[1]) != 1 || res[1][0].Name != "phone" {
		t.Fatalf("unexpected devices result: %+v / %+v", res[0], res[1])
	}

	// Subsequent call within 10s uses cache without calling fetchDevices.
	cached := devices()
	if calls.Load() != 1 || len(cached) != 1 {
		t.Fatalf("expected cached result with 1 call, got calls=%d len=%d", calls.Load(), len(cached))
	}
}
