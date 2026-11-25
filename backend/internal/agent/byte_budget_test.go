package agent

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var budgetEpoch = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func TestByteBudget_AllowsUpToTheBurst(t *testing.T) {
	b := newByteBudget(100, 1000)

	for i := range 10 {
		if !b.allow(budgetEpoch, 100) {
			t.Fatalf("request %d within the burst was refused", i)
		}
	}
	if b.allow(budgetEpoch, 1) {
		t.Error("the burst is used up, nothing more may pass")
	}
}

func TestByteBudget_RefusesRequestsLargerThanTheBurst(t *testing.T) {
	b := newByteBudget(100, 1000)

	if b.allow(budgetEpoch, 1001) {
		t.Error("a request above the burst can never fit")
	}
	if !b.allow(budgetEpoch, 1000) {
		t.Error("a refused request must not use up tokens")
	}
}

func TestByteBudget_RefillsAtTheRate(t *testing.T) {
	b := newByteBudget(100, 1000)
	b.allow(budgetEpoch, 1000)

	if b.allow(budgetEpoch.Add(time.Second), 101) {
		t.Error("one second only refills 100 bytes")
	}
	if !b.allow(budgetEpoch.Add(2*time.Second), 100) {
		t.Error("100 bytes were refilled by the second second")
	}
}

func TestByteBudget_RefillIsCappedAtTheBurst(t *testing.T) {
	b := newByteBudget(100, 1000)
	b.allow(budgetEpoch, 1000)

	later := budgetEpoch.Add(time.Hour)
	if !b.allow(later, 1000) {
		t.Fatal("the budget must be full again after a long pause")
	}
	if b.allow(later, 1) {
		t.Error("a long pause must not store more than the burst")
	}
}

func TestByteBudget_StartsFullWhateverTheClockSays(t *testing.T) {
	for _, now := range []time.Time{
		time.Time{}.Add(time.Hour),
		budgetEpoch,
		budgetEpoch.AddDate(50, 0, 0),
	} {
		b := newByteBudget(100, 1000)
		if !b.allow(now, 1000) {
			t.Errorf("a new budget must start full at %v", now)
		}
	}
}

func TestByteBudget_ClockGoingBackwardsDoesNotTakeTokens(t *testing.T) {
	b := newByteBudget(100, 1000)
	b.allow(budgetEpoch, 500)

	if !b.allow(budgetEpoch.Add(-time.Hour), 500) {
		t.Error("an earlier timestamp must not reduce the remaining tokens")
	}
}

func TestByteBudget_RefundGivesBytesBack(t *testing.T) {
	b := newByteBudget(0, 1000)
	b.allow(budgetEpoch, 1000)

	b.refund(400)

	if !b.allow(budgetEpoch, 400) {
		t.Error("refunded bytes must be usable again")
	}
	if b.allow(budgetEpoch, 1) {
		t.Error("only the refunded bytes may come back")
	}
}

func TestByteBudget_RefundIsCappedAtTheBurst(t *testing.T) {
	b := newByteBudget(0, 1000)

	b.refund(5000)

	if !b.allow(budgetEpoch, 1000) {
		t.Fatal("the budget must still be full")
	}
	if b.allow(budgetEpoch, 1) {
		t.Error("a refund must not raise the budget above the burst")
	}
}

func TestByteBudget_ConcurrentUseNeverExceedsTheBurst(t *testing.T) {
	b := newByteBudget(0, 10_000)
	var admitted atomic.Int64
	var wg sync.WaitGroup

	for range 8 {
		wg.Go(func() {
			for range 500 {
				if b.allow(budgetEpoch, 10) {
					admitted.Add(10)
				}
			}
		})
	}
	wg.Wait()

	if got := admitted.Load(); got != 10_000 {
		t.Errorf("admitted %d bytes, want exactly the burst of 10000", got)
	}
}
