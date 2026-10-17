package auth

import "time"

func SaturateHashSlotsForTest(wait time.Duration) (release func()) {
	prev := slotWaitTimeout
	slotWaitTimeout = wait

	held := 0
	for range cap(hashSlots) {
		hashSlots <- struct{}{}
		held++
	}

	return func() {
		for range held {
			<-hashSlots
		}
		slotWaitTimeout = prev
	}
}
