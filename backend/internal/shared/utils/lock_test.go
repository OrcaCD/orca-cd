package utils

import (
	"sync"
	"testing"
	"time"
)

func TestLockWithTimeout(t *testing.T) {
	var lock sync.Mutex

	if !LockWithTimeout(t.Context(), &lock, time.Second) {
		t.Fatal("expected to acquire a free lock")
	}
	if LockWithTimeout(t.Context(), &lock, 100*time.Millisecond) {
		t.Fatal("expected a held lock to time out")
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		lock.Unlock()
	}()
	if !LockWithTimeout(t.Context(), &lock, 2*time.Second) {
		t.Fatal("expected to acquire the lock once it was released")
	}
}
