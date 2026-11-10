package utils

import (
	"context"
	"sync"
	"time"
)

// LockWithTimeout tries to lock the mutex until the timeout elapses or ctx is
// done. It reports whether the lock was acquired.
func LockWithTimeout(ctx context.Context, lock *sync.Mutex, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for !lock.TryLock() {
		select {
		case <-ticker.C:
		case <-timer.C:
			return false
		case <-ctx.Done():
			return false
		}
	}
	return true
}
