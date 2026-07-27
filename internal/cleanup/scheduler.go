package cleanup

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/yaso09/tengiz/internal/runtime"
)

type SchedulerOptions struct {
	Interval time.Duration
	CleanAll bool
}

type Scheduler struct {
	rt   runtime.Manager
	opts SchedulerOptions
	mu   sync.Mutex
	stop chan struct{}
	done chan struct{}
}

func NewScheduler(rt runtime.Manager, opts SchedulerOptions) *Scheduler {
	if opts.Interval == 0 {
		opts.Interval = 24 * time.Hour
	}
	return &Scheduler{
		rt:   rt,
		opts: opts,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
}

func (s *Scheduler) Start() error {
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(s.opts.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.runCleanup()
			case <-s.stop:
				return
			}
		}
	}()
	return nil
}

func (s *Scheduler) Stop() {
	close(s.stop)
	<-s.done
}

func (s *Scheduler) runCleanup() {
	ctx := context.Background()
	opts := runtime.CleanupOptions{}
	if s.opts.CleanAll {
		opts.All = true
	}
	report, err := s.rt.Cleanup(ctx, opts)
	if err != nil {
		log.Printf("[cleanup] periodic cleanup failed: %v", err)
		return
	}
	if report.ContainersRemoved > 0 || report.ImagesRemoved > 0 {
		log.Printf("[cleanup] periodic cleanup: %d containers, %d images removed", report.ContainersRemoved, report.ImagesRemoved)
	}
}
