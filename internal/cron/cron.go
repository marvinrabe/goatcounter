// Package cron schedules cancellable, sequential jobs in each replica.
// Database transactions coordinate durable work across replicas.
package cron

import (
	"context"
	"sync"
	"time"

	"github.com/marvinrabe/goatcounter/internal/log"
)

type Runner struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func Start(ctx context.Context, interval time.Duration) *Runner {
	ctx, cancel := context.WithCancel(ctx)
	r := &Runner{cancel: cancel, done: make(chan struct{})}
	var wg sync.WaitGroup
	for _, task := range []struct {
		name   string
		period time.Duration
		run    func(context.Context) error
	}{
		{"persist hits", interval, PersistAndStat},
		{"expire sessions", time.Minute, sessions},
		{"expire bots", 24 * time.Hour, oldBot},
		{"expire filters", time.Hour, oldFilters},
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer log.Recover(ctx)
			timer := time.NewTimer(task.period)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					if err := task.run(ctx); err != nil && ctx.Err() == nil {
						log.Module("cron").Error(ctx, err, "task", task.name)
					}
					timer.Reset(task.period)
				}
			}
		}()
	}
	go func() { wg.Wait(); close(r.done) }()
	return r
}

// Stop cancels active transactions. Uncommitted inbox entries remain durable
// and can be processed by another replica; shutdown needs no final snapshot.
func (r *Runner) Stop(ctx context.Context) error {
	r.cancel()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
