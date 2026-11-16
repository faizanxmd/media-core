package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKeyframesCachedFailureReturnsError(t *testing.T) {
	oldData := DataDir
	DataDir = t.TempDir()
	defer func() { DataDir = oldData }()

	keyMu.Lock()
	oldCache := keyCache
	oldBusy := keyBusy
	keyCache = map[string]keyEntry{}
	keyBusy = map[string]bool{}
	keyMu.Unlock()
	defer func() {
		keyMu.Lock()
		keyCache, keyBusy = oldCache, oldBusy
		keyMu.Unlock()
	}()

	bad := filepath.Join(t.TempDir(), "broken.avi")
	if err := os.WriteFile(bad, []byte("not a video"), 0o644); err != nil {
		t.Fatal(err)
	}

	// First call starts background probe and returns errKeysPending.
	_, err := keyframes(bad)
	if err != errKeysPending {
		t.Fatalf("first call err = %v, want errKeysPending", err)
	}

	// Wait for background probe to finish and cache failure.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		keyMu.Lock()
		busy := keyBusy[bad]
		keyMu.Unlock()
		if !busy {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Second call hits the cached failure: must return ErrNoKeyframes, NOT (nil, nil).
	keys, err := keyframes(bad)
	if !errors.Is(err, ErrNoKeyframes) {
		t.Fatalf("cached failure returned (%v, %v), expected ErrNoKeyframes", keys, err)
	}
}

func TestKeyframesTimeoutClearsBusyAndCacheBounded(t *testing.T) {
	oldData := DataDir
	oldProbe := keyProbe
	DataDir = t.TempDir()
	defer func() {
		DataDir = oldData
		keyProbe = oldProbe
	}()

	keyMu.Lock()
	oldCache, oldBusy, oldSeq := keyCache, keyBusy, keySeq
	keyCache = map[string]keyEntry{}
	keyBusy = map[string]bool{}
	keyMu.Unlock()
	defer func() {
		keyMu.Lock()
		keyCache, keyBusy, keySeq = oldCache, oldBusy, oldSeq
		keyMu.Unlock()
	}()

	f := filepath.Join(t.TempDir(), "slow.avi")
	os.WriteFile(f, []byte("x"), 0o644)

	keyProbe = func(path string) ([]float64, error) {
		return nil, context.DeadlineExceeded
	}
	if _, err := keyframes(f); err != errKeysPending {
		t.Fatalf("first call err = %v, want errKeysPending", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		keyMu.Lock()
		busy := keyBusy[f]
		keyMu.Unlock()
		if !busy {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	keyMu.Lock()
	busy := keyBusy[f]
	_, cached := keyCache[f]
	keyMu.Unlock()
	if busy {
		t.Fatal("keyBusy was not cleared after timeout")
	}
	if cached {
		t.Fatal("timeout should not permanently cache failure")
	}

	// Test keyCache bounding via keep path (saved keys json).
	tmpDir := t.TempDir()
	keyProbe = func(path string) ([]float64, error) {
		return []float64{0, 2, 4}, nil
	}
	// Populate maxKeyCache + 50 entries via saved json so each call hits keep() synchronously.
	os.MkdirAll(filepath.Join(DataDir, "keys"), 0o755)
	for i := 0; i < maxKeyCache+50; i++ {
		p := filepath.Join(tmpDir, fmt.Sprintf("f%d.avi", i))
		os.WriteFile(p, []byte("x"), 0o644)
		sz, mt, _ := fileKey(p)
		saved := filepath.Join(DataDir, "keys", pathID(p)+".json")
		os.WriteFile(saved, []byte(fmt.Sprintf(`{"size":%d,"mtime":%d,"keys":[0,2,4]}`, sz, mt)), 0o644)
		if _, err := keyframes(p); err != nil {
			t.Fatalf("keyframes(%s): %v", p, err)
		}
	}
	keyMu.Lock()
	n := len(keyCache)
	keyMu.Unlock()
	if n > maxKeyCache {
		t.Fatalf("keyCache grew to %d, expected <= %d", n, maxKeyCache)
	}
}
