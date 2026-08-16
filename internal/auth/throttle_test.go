package auth

import (
	"testing"
	"time"
)

func TestThrottleCountsFailuresNotAttempts(t *testing.T) {
	th := newThrottle(3, time.Minute)

	// Checking is free. Somebody signing in correctly ten times running must
	// never be locked out by their own successful traffic.
	for range 10 {
		if err := th.check("player@example.com", "10.0.0.1"); err != nil {
			t.Fatalf("a check with no recorded failures was refused: %v", err)
		}
	}

	for range 3 {
		th.failed("player@example.com", "10.0.0.1")
	}
	if err := th.check("player@example.com", "10.0.0.1"); err == nil {
		t.Error("err = nil after exhausting the allowance, want the attempt refused")
	}
}

// One address absorbs only so many wrong passwords however many machines the
// attempts come from — this is what defeats a distributed attack on one person.
func TestThrottleBlocksOneAccountAcrossManyAddresses(t *testing.T) {
	th := newThrottle(3, time.Minute)

	th.failed("victim@example.com", "10.0.0.1")
	th.failed("victim@example.com", "10.0.0.2")
	th.failed("victim@example.com", "10.0.0.3")

	if err := th.check("victim@example.com", "10.0.0.4"); err == nil {
		t.Error("a fresh IP got a fresh allowance against an account that had used its own")
	}
	// And an unrelated account from that same new address is unaffected.
	if err := th.check("bystander@example.com", "10.0.0.4"); err != nil {
		t.Errorf("an unrelated account was caught by another's limit: %v", err)
	}
}

// One source can only be wrong so many times across all accounts — this is what
// defeats spraying one common password at every address.
func TestThrottleBlocksOneAddressAcrossManyAccounts(t *testing.T) {
	th := newThrottle(2, time.Minute)
	// The IP allowance is three times the account limit.
	for i := range 6 {
		th.failed(string(rune('a'+i))+"@example.com", "10.0.0.9")
	}

	if err := th.check("untouched@example.com", "10.0.0.9"); err == nil {
		t.Error("a sprayer got a fresh allowance by moving to another account")
	}
	// Somebody else's connection is unaffected.
	if err := th.check("untouched@example.com", "10.0.0.10"); err != nil {
		t.Errorf("an unrelated address was caught by the sprayer's limit: %v", err)
	}
}

// The refusal has to be identical whether or not the account exists, or the
// limiter becomes an enumeration oracle that needs no valid password to
// consult.
func TestThrottleRefusalDoesNotDependOnTheAccountExisting(t *testing.T) {
	th := newThrottle(1, time.Minute)

	th.failed("real@example.com", "10.0.0.1")
	th.failed("made-up@example.com", "10.0.0.1")

	real := th.check("real@example.com", "10.0.0.2")
	fake := th.check("made-up@example.com", "10.0.0.2")
	if real == nil || fake == nil {
		t.Fatal("both should be refused")
	}
	if real.Error() != fake.Error() {
		t.Errorf("the two refusals differ:\n  %v\n  %v", real, fake)
	}
}

func TestThrottleAllowsAgainAfterTheWindow(t *testing.T) {
	th := newThrottle(1, 50*time.Millisecond)
	th.failed("player@example.com", "10.0.0.1")

	if err := th.check("player@example.com", "10.0.0.1"); err == nil {
		t.Fatal("expected the attempt to be refused inside the window")
	}
	time.Sleep(70 * time.Millisecond)
	if err := th.check("player@example.com", "10.0.0.1"); err != nil {
		t.Errorf("still refused after the window elapsed: %v", err)
	}
}

// Being refused must not itself extend the refusal, or a client that keeps
// retrying turns a fixed window into an unbounded one.
func TestThrottleCheckDoesNotExtendItsOwnBlock(t *testing.T) {
	th := newThrottle(1, 60*time.Millisecond)
	th.failed("player@example.com", "10.0.0.1")

	deadline := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(deadline) {
		_ = th.check("player@example.com", "10.0.0.1")
	}
	time.Sleep(40 * time.Millisecond)

	if err := th.check("player@example.com", "10.0.0.1"); err != nil {
		t.Errorf("hammering check kept the block alive past its window: %v", err)
	}
}
