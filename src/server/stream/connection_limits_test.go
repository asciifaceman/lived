package stream

import "testing"

func TestStreamConnectionLimiter_EnforcesPerSessionAndPerAccountLimits(t *testing.T) {
	limiter := newStreamConnectionLimiter(2, 1)

	if !limiter.tryAcquire(10, 100) {
		t.Fatalf("expected first session acquire to succeed")
	}
	if limiter.tryAcquire(10, 100) {
		t.Fatalf("expected second acquire for same session to be blocked by per-session limit")
	}
	if !limiter.tryAcquire(10, 101) {
		t.Fatalf("expected second session acquire to succeed within per-account limit")
	}
	if limiter.tryAcquire(10, 102) {
		t.Fatalf("expected third acquire for account to be blocked by per-account limit")
	}

	limiter.release(10, 100)
	if !limiter.tryAcquire(10, 102) {
		t.Fatalf("expected acquire to succeed after release")
	}
}

func TestStreamConnectionLimiter_InvalidIDsRejected(t *testing.T) {
	limiter := newStreamConnectionLimiter(2, 1)

	if limiter.tryAcquire(0, 1) {
		t.Fatalf("expected zero account id to be rejected")
	}
	if limiter.tryAcquire(1, 0) {
		t.Fatalf("expected zero session id to be rejected")
	}
}
