// Package assert provides test assertion helpers.
package assert

import (
	"testing"
	"time"
)

// Eventually retries a condition until it passes or times out.
func Eventually(t *testing.T, condition func() bool, timeout time.Duration, msgAndArgs ...any) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	interval := 100 * time.Millisecond

	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(interval)

		// Exponential backoff up to 1 second
		interval = min(interval*2, time.Second)
	}

	if len(msgAndArgs) > 0 {
		t.Fatalf("condition not met within %v: %v", timeout, msgAndArgs[0])
	} else {
		t.Fatalf("condition not met within %v", timeout)
	}
}

// Never asserts that a condition never becomes true within the duration.
func Never(t *testing.T, condition func() bool, duration time.Duration, msgAndArgs ...any) {
	t.Helper()

	deadline := time.Now().Add(duration)
	interval := 100 * time.Millisecond

	for time.Now().Before(deadline) {
		if condition() {
			if len(msgAndArgs) > 0 {
				t.Fatalf("condition became true unexpectedly: %v", msgAndArgs[0])
			} else {
				t.Fatal("condition became true unexpectedly")
			}
		}
		time.Sleep(interval)
	}
}

// EventuallyWithT is like Eventually but passes *testing.T to the condition
// for assertions within the condition function.
func EventuallyWithT(t *testing.T, condition func(t *testing.T) bool, timeout time.Duration, msgAndArgs ...any) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	interval := 100 * time.Millisecond

	for time.Now().Before(deadline) {
		if condition(t) {
			return
		}
		time.Sleep(interval)

		interval = min(interval*2, time.Second)
	}

	if len(msgAndArgs) > 0 {
		t.Fatalf("condition not met within %v: %v", timeout, msgAndArgs[0])
	} else {
		t.Fatalf("condition not met within %v", timeout)
	}
}
