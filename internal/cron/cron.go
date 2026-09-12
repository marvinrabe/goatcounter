// Package cron schedules jobs.
package cron

import (
	"context"
	"math/rand/v2"
	"strings"
	"sync/atomic"
	"time"

	"github.com/marvinrabe/goatcounter/internal/bgrun"
	"github.com/marvinrabe/goatcounter/internal/log"
	"zgo.at/zstd/zruntime"
	"zgo.at/zstd/zsync"
)

type Task struct {
	Desc   string
	Fun    func(context.Context) error
	Period time.Duration
}

func (t Task) ID() string {
	return strings.Replace(zruntime.FuncName(t.Fun), "github.com/marvinrabe/goatcounter/internal/cron.", "", 1)
}

var Tasks = []Task{
	{"vacuum pageviews (old bot)", oldBot, 24 * time.Hour},
	{"cycle sessions", sessions, 1 * time.Minute},
	{"persist hits", persistAndStat, time.Duration(persistInterval.Load())},
	{"vacuum filters", oldFilters, 1 * time.Hour},
}

var (
	stopped         = zsync.NewAtomicInt(0)
	started         = zsync.NewAtomicInt(0)
	persistInterval = func() *atomic.Int64 {
		var d atomic.Int64
		d.Store(int64(10 * time.Second))
		return &d
	}()
)

func SetPersistInterval(d time.Duration) {
	persistInterval.Store(int64(d))
}

// Start running tasks in the background.
func Start(ctx context.Context) {
	if started.Value() == 1 {
		return
	}
	started.Set(1)

	// Nothing displays the job history, so don't retain it.
	bgrun.History(-1)

	l := log.Module("cron")

	for _, t := range Tasks {
		f := t.ID()
		bgrun.NewTask("cron:"+f, 1, func(context.Context) error {
			err := t.Fun(ctx)
			if err != nil {
				l.Error(ctx, err, "task", f)
			}
			return nil
		})
	}

	for _, t := range Tasks {
		go func(t Task) {
			defer log.Recover(ctx)

			id := t.ID()
			for {
				if id == "persistAndStat" {
					time.Sleep(time.Duration(persistInterval.Load()))
				} else {
					p := t.Period
					// Add some random jitter to prevent jobs from running at
					// the same time.
					if t.Period > time.Minute {
						m := t.Period / 50
						if t.Period >= time.Hour*12 {
							m = t.Period / 100
						}
						rnd := time.Duration(rand.Int64N(int64(m))).Round(time.Second)
						if rand.IntN(2) == 1 {
							rnd = -rnd
						}
						p += rnd
					}
					time.Sleep(p)
				}
				if stopped.Value() == 1 {
					return
				}

				err := bgrun.RunTask("cron:" + id)
				if err != nil {
					log.Error(ctx, err)
				}
			}
		}(t)
	}
}

func Stop() error {
	stopped.Set(1)
	started.Set(0)
	bgrun.Wait("")
	bgrun.Reset()
	return nil
}

func TaskSessions() error       { return bgrun.RunTask("cron:sessions") }
func TaskPersistAndStat() error { return bgrun.RunTask("cron:persistAndStat") }
func WaitSessions()             { bgrun.Wait("cron:sessions") }
