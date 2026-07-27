package cleanup

import (
	"testing"
	"time"

	"github.com/yaso09/tengiz/internal/runtime"
)

func TestSchedulerStartStop(t *testing.T) {
	rt := runtime.NewStub()
	s := NewScheduler(rt, SchedulerOptions{
		Interval: 50 * time.Millisecond,
		CleanAll: true,
	})
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	s.Stop()
}
