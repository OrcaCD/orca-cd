package auth

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fillSlots(t *testing.T) {
	t.Helper()
	for range cap(hashSlots) {
		hashSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for range cap(hashSlots) {
			select {
			case <-hashSlots:
			default:
			}
		}
	})
}

func TestWithSlot_CapsConcurrency(t *testing.T) {
	var running, peak atomic.Int32
	done := make(chan struct{})

	n := cap(hashSlots) * 3
	for range n {
		go func() {
			defer func() { done <- struct{}{} }()
			_, _ = withSlot(context.Background(), 0, func() bool {
				cur := running.Add(1)
				for {
					p := peak.Load()
					if cur <= p || peak.CompareAndSwap(p, cur) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				running.Add(-1)
				return true
			})
		}()
	}
	for range n {
		<-done
	}

	if got := int(peak.Load()); got > cap(hashSlots) {
		t.Errorf("peak concurrency %d exceeded %d slots", got, cap(hashSlots))
	}
}

func TestWithSlot_ReturnsErrBusyWhenSaturated(t *testing.T) {
	fillSlots(t)

	called := false
	_, err := withSlot(context.Background(), 10*time.Millisecond, func() bool {
		called = true
		return true
	})
	if !errors.Is(err, ErrBusy) {
		t.Errorf("withSlot() error = %v, want ErrBusy", err)
	}
	if called {
		t.Error("fn must not run without a slot")
	}
}

func TestWithSlot_RespectsContextCancellation(t *testing.T) {
	fillSlots(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := withSlot(ctx, 0, func() bool { return true })
	if !errors.Is(err, context.Canceled) {
		t.Errorf("withSlot() error = %v, want context.Canceled", err)
	}
}

func shortenSlotWait(t *testing.T) {
	t.Helper()
	orig := slotWaitTimeout
	slotWaitTimeout = 10 * time.Millisecond
	t.Cleanup(func() { slotWaitTimeout = orig })
}

func TestHashSlots_Capacity(t *testing.T) {
	if c := cap(hashSlots); c < 1 || c > maxHashSlots {
		t.Errorf("cap(hashSlots) = %d, want between 1 and %d", c, maxHashSlots)
	}
}

func TestPasswordContext_ErrBusyWhenSaturated(t *testing.T) {
	if err := initPassword(); err != nil {
		t.Fatalf("initPassword() error: %v", err)
	}
	shortenSlotWait(t)
	fillSlots(t)

	ctx := context.Background()

	if _, err := HashPasswordContext(ctx, "securepassword123"); !errors.Is(err, ErrBusy) {
		t.Errorf("HashPasswordContext() error = %v, want ErrBusy", err)
	}
	if ok, err := CheckPasswordContext(ctx, "securepassword123", "hash"); ok || !errors.Is(err, ErrBusy) {
		t.Errorf("CheckPasswordContext() = %v, %v; want false, ErrBusy", ok, err)
	}
	if err := CompareWithDummyContext(ctx, "securepassword123"); !errors.Is(err, ErrBusy) {
		t.Errorf("CompareWithDummyContext() error = %v, want ErrBusy", err)
	}
}

func TestPasswordContext_CancelledWhenSaturated(t *testing.T) {
	fillSlots(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := HashPasswordContext(ctx, "securepassword123"); !errors.Is(err, context.Canceled) {
		t.Errorf("HashPasswordContext() error = %v, want context.Canceled", err)
	}
	if ok, err := CheckPasswordContext(ctx, "securepassword123", "hash"); ok || !errors.Is(err, context.Canceled) {
		t.Errorf("CheckPasswordContext() = %v, %v; want false, context.Canceled", ok, err)
	}
}

func TestPasswordContext_ShortCircuitsWithoutSlot(t *testing.T) {
	shortenSlotWait(t)
	fillSlots(t)

	ctx := context.Background()

	if _, err := HashPasswordContext(ctx, ""); !errors.Is(err, ErrEmptyPassword) {
		t.Errorf("HashPasswordContext(\"\") error = %v, want ErrEmptyPassword", err)
	}
	if ok, err := CheckPasswordContext(ctx, "", "hash"); ok || err != nil {
		t.Errorf("CheckPasswordContext(empty password) = %v, %v; want false, nil", ok, err)
	}
	if ok, err := CheckPasswordContext(ctx, "password", ""); ok || err != nil {
		t.Errorf("CheckPasswordContext(empty hash) = %v, %v; want false, nil", ok, err)
	}
}

func TestCompareWithDummyContext(t *testing.T) {
	if err := initPassword(); err != nil {
		t.Fatalf("initPassword() error: %v", err)
	}
	if err := CompareWithDummyContext(context.Background(), "some-password"); err != nil {
		t.Errorf("CompareWithDummyContext() error = %v, want nil", err)
	}
}

func TestPasswordContext_ReleasesSlot(t *testing.T) {
	ctx := context.Background()

	hash, err := HashPasswordContext(ctx, "securepassword123")
	if err != nil {
		t.Fatalf("HashPasswordContext() error: %v", err)
	}
	if n := len(hashSlots); n != 0 {
		t.Errorf("%d slots still held after HashPasswordContext()", n)
	}

	if _, err := CheckPasswordContext(ctx, "securepassword123", hash); err != nil {
		t.Fatalf("CheckPasswordContext() error: %v", err)
	}
	if n := len(hashSlots); n != 0 {
		t.Errorf("%d slots still held after CheckPasswordContext()", n)
	}
}

func TestBlockingVariants_WaitForSlot(t *testing.T) {
	hash, err := HashPassword("securepassword123")
	if err != nil {
		t.Fatalf("HashPassword() error: %v", err)
	}

	fillSlots(t)

	hashDone := make(chan error, 1)
	checkDone := make(chan bool, 1)
	go func() {
		_, err := HashPassword("anotherpassword123")
		hashDone <- err
	}()
	go func() { checkDone <- CheckPassword("securepassword123", hash) }()

	select {
	case <-hashDone:
		t.Fatal("HashPassword() returned while all slots were held")
	case <-checkDone:
		t.Fatal("CheckPassword() returned while all slots were held")
	case <-time.After(100 * time.Millisecond):
	}

	for range cap(hashSlots) {
		<-hashSlots
	}

	select {
	case err := <-hashDone:
		if err != nil {
			t.Errorf("HashPassword() error: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("HashPassword() did not complete after slots were freed")
	}
	select {
	case ok := <-checkDone:
		if !ok {
			t.Error("CheckPassword() returned false for the correct password")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("CheckPassword() did not complete after slots were freed")
	}
}

func TestPasswordContext_Roundtrip(t *testing.T) {
	ctx := context.Background()
	hash, err := HashPasswordContext(ctx, "securepassword123")
	if err != nil {
		t.Fatalf("HashPasswordContext() error: %v", err)
	}
	ok, err := CheckPasswordContext(ctx, "securepassword123", hash)
	if err != nil || !ok {
		t.Errorf("CheckPasswordContext() = %v, %v; want true, nil", ok, err)
	}
	ok, err = CheckPasswordContext(ctx, "wrong", hash)
	if err != nil || ok {
		t.Errorf("CheckPasswordContext(wrong) = %v, %v; want false, nil", ok, err)
	}
}

func TestHashAndCheckPassword(t *testing.T) {
	password := "securepassword123"
	hash, err := HashPassword(password)

	if err != nil {
		t.Fatalf("HashPassword() error: %v", err)
	}

	if hash == "" {
		t.Fatal("HashPassword() returned empty hash")
	}

	if hash == password {
		t.Error("hash should not equal plaintext password")
	}

	if !CheckPassword(password, hash) {
		t.Error("CheckPassword() returned false for correct password")
	}

}

func TestHashPassword_EmptyPassword(t *testing.T) {
	_, err := HashPassword("")
	if err == nil {
		t.Fatal("HashPassword(\"\") expected error for empty password")
	}
}

func TestCheckPassword_InvalidHash(t *testing.T) {
	if CheckPassword("password", "not-a-valid-hash") {
		t.Error("CheckPassword() returned true for invalid hash string")
	}
}

func TestHashPassword_MultipleHashes(t *testing.T) {
	pw := "repeatable"
	hash1, err1 := HashPassword(pw)
	hash2, err2 := HashPassword(pw)
	if err1 != nil || err2 != nil {
		t.Fatalf("HashPassword() error: %v, %v", err1, err2)
	}
	if hash1 == hash2 {
		t.Error("HashPassword() produced same hash for same password twice (should be different due to salt)")
	}
}

func TestHashPassword_LongPassword(t *testing.T) {
	longPw := make([]byte, 1000)
	for i := range longPw {
		longPw[i] = 'a'
	}
	hash, err := HashPassword(string(longPw))
	if err != nil {
		t.Fatalf("HashPassword(long) error: %v", err)
	}
	if !CheckPassword(string(longPw), hash) {
		t.Error("CheckPassword() failed for long password")
	}
}

func TestHashPassword_UnicodePassword(t *testing.T) {
	pw := "pässwörd-测试-パスワード" // #nosec
	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword(unicode) error: %v", err)
	}
	if !CheckPassword(pw, hash) {
		t.Error("CheckPassword() failed for unicode password")
	}
}

func TestCheckPassword_WrongPassword(t *testing.T) {
	hash, err := HashPassword("correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("HashPassword() error: %v", err)
	}
	if CheckPassword("wrongpassword", hash) {
		t.Error("CheckPassword() returned true for incorrect password")
	}
}

func TestCheckPassword_EmptyPassword(t *testing.T) {
	if CheckPassword("", "somehash") {
		t.Error("CheckPassword() returned true for empty password")
	}
}

func TestCheckPassword_EmptyHash(t *testing.T) {
	if CheckPassword("password", "") {
		t.Error("CheckPassword() returned true for empty hash")
	}
}

func TestInitPassword_SetsDummyHash(t *testing.T) {
	prev := dummyHash
	dummyHash = ""
	t.Cleanup(func() { dummyHash = prev })

	if err := initPassword(); err != nil {
		t.Fatalf("initPassword() error: %v", err)
	}
	if dummyHash == "" {
		t.Fatal("initPassword() did not set dummyHash")
	}
	if !strings.HasPrefix(dummyHash, "$argon2id$") {
		t.Errorf("dummyHash does not look like an argon2id hash: %q", dummyHash)
	}
}

func TestInitPassword_ReusesDummyHash(t *testing.T) {
	prev := dummyHash
	dummyHash = ""
	t.Cleanup(func() { dummyHash = prev })

	if err := initPassword(); err != nil {
		t.Fatalf("first initPassword() error: %v", err)
	}
	first := dummyHash

	if err := initPassword(); err != nil {
		t.Fatalf("second initPassword() error: %v", err)
	}
	if dummyHash != first {
		t.Error("initPassword() regenerated an already initialized dummy hash")
	}
}

func TestCompareWithDummy_DoesNotPanic(t *testing.T) {
	if err := initPassword(); err != nil {
		t.Fatalf("initPassword() error: %v", err)
	}

	CompareWithDummy("some-password")
	CompareWithDummy("")
	CompareWithDummy(strings.Repeat("a", 1000))
}

func TestValidatePasswordStrength(t *testing.T) {
	tests := []struct {
		password string
		want     bool
	}{
		{"short", false},
		{"alllowercase123!", false},
		{"ALLUPPERCASE123!", false},
		{"NoNumbers!NoNumbers!", false},
		{"NoSpecial1234ABCDEF", false},
		{"ValidPass1!ValidPass", true},
		{"Abcdefghijk1!", true},
		// Non-ASCII: Ä counts as uppercase, ß as lowercase, @ as special.
		{"Äbcdefghijk1@", true},
		// Non-ASCII-only upper/lower still satisfies the policy.
		{"Äßöüàé1234!!", true},
	}
	for _, tc := range tests {
		if got := ValidatePasswordStrength(tc.password); got != tc.want {
			t.Errorf("ValidatePasswordStrength(%q) = %v, want %v", tc.password, got, tc.want)
		}
	}
}

func TestGenerateRandomPassword_MeetsStrengthPolicy(t *testing.T) {
	for range 20 {
		pw, err := GenerateRandomPassword()
		if err != nil {
			t.Fatalf("GenerateRandomPassword() error: %v", err)
		}
		if !ValidatePasswordStrength(pw) {
			t.Errorf("generated password failed strength check: %q", pw)
		}
	}
}

func TestCompareWithDummy_Timing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing test in short mode")
	}

	if err := initPassword(); err != nil {
		t.Fatalf("initPassword() error: %v", err)
	}

	hash, err := HashPassword("reference-password")
	if err != nil {
		t.Fatalf("HashPassword() error: %v", err)
	}

	const runs = 3
	var realTotal, dummyTotal time.Duration
	for range runs {
		start := time.Now()
		CheckPassword("wrong-password", hash)
		realTotal += time.Since(start)

		start = time.Now()
		CompareWithDummy("wrong-password")
		dummyTotal += time.Since(start)
	}

	realAvg := realTotal / runs
	dummyAvg := dummyTotal / runs

	diff := realAvg - dummyAvg
	if diff < 0 {
		diff = -diff
	}
	threshold := realAvg / 2
	if diff > threshold {
		t.Errorf("timing difference too large: real=%v dummy=%v diff=%v (threshold=%v)",
			realAvg, dummyAvg, diff, threshold)
	}
}
