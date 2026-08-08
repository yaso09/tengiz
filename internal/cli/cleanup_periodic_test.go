package cli

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunPeriodicRunsAtLeastThreeTimes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	done := make(chan struct{})

	go func() {
		runPeriodic(ctx, 10*time.Millisecond, func() error {
			if calls.Add(1) >= 3 {
				cancel()
			}
			return nil
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runPeriodic did not return after context cancel")
	}

	if calls.Load() < 3 {
		t.Errorf("runPeriodic calls = %d, want >= 3", calls.Load())
	}
}

func TestRunPeriodicFirstRunErrorPropagates(t *testing.T) {
	err := runPeriodic(context.Background(), time.Minute, func() error {
		return fmt.Errorf("boom")
	})
	if err == nil || err.Error() != "boom" {
		t.Errorf("runPeriodic() error = %v, want %q", err, "boom")
	}
}

func TestRunPeriodicContinuesAfterIterationError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	done := make(chan struct{})

	go func() {
		runPeriodic(ctx, 5*time.Millisecond, func() error {
			n := calls.Add(1)
			if n >= 3 {
				cancel()
			}
			if n%2 == 0 {
				return fmt.Errorf("transient failure")
			}
			return nil
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runPeriodic did not return after context cancel")
	}

	if calls.Load() < 3 {
		t.Errorf("runPeriodic calls = %d, want >= 3 despite iteration errors", calls.Load())
	}
}