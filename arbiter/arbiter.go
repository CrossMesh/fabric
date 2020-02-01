package arbiter

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"

	logging "github.com/sirupsen/logrus"
)

// Arbiter is tracer to manage lifecycle of goroutines.
type Arbiter struct {
	lock chan struct{}

	runningCount int32

	ctx    context.Context
	cancel context.CancelFunc

	sigFibreExit chan struct{}
	sigOS        chan os.Signal

	children sync.Map

	preStop   func()
	afterStop func()

	log *logging.Entry
}

// NewWithParent creates a new arbiter atteched to specified parent arbiter.
// The arbiter will be shut down by the parent or a call to Arbiter.Shutdown().
func NewWithParent(parent *Arbiter, log *logging.Entry) *Arbiter {
	if log == nil {
		log = logging.WithField("module", "arbiter")
	}
	a := &Arbiter{
		sigFibreExit: make(chan struct{}, 10),
		sigOS:        make(chan os.Signal, 0),
		log:          log,
		lock:         make(chan struct{}, 1),
	}
	a.lock <- struct{}{}

	var parentCtx context.Context
	if parent != nil {
		parentCtx = parent.ctx

		// join parent.
		parent.Go(func() {
			parent.children.Store(a, struct{}{})
			a.Join()
			parent.children.Delete(a)
		})
	} else {
		parentCtx = context.Background()

	}
	a.ctx, a.cancel = context.WithCancel(parentCtx)

	return a
}

// New create a new arbiter.
func New(log *logging.Entry) *Arbiter {
	return NewWithParent(nil, log)
}

// Log return Entry of arbiter.
func (a *Arbiter) Log() *logging.Entry {
	return a.log
}

// NumGoroutine returns the number of goroutines currently exists on Arbiter tree.
func (a *Arbiter) NumGoroutine() (n int32) {
	n = atomic.LoadInt32(&a.runningCount)
	if n > 0 {
		n -= int32(1 - len(a.lock))
	}
	a.children.Range(func(k, v interface{}) bool {
		n += k.(*Arbiter).NumGoroutine() - 1
		return true
	})
	return
}

// Go spawns the proc (act the same as the "go" keyword) and let the arbiter traces it.
func (a *Arbiter) Go(proc func()) *Arbiter {
	atomic.AddInt32(&a.runningCount, 1)
	go func() {
		defer func() {
			a.sigFibreExit <- struct{}{}
		}()
		if a.ShouldRun() {
			proc()
		}
	}()
	return a
}

// TickGo spawns proc with specified period.
func (a *Arbiter) TickGo(proc func(func(), time.Time), period time.Duration, brust uint32) (cancel func()) {
	if brust < 1 {
		return
	}

	runningCount, canceled := uint32(0), false
	cancel = func() { canceled = true } // cancel function.
	a.Go(func() {
		nextSpawn := time.Now()

		for a.ShouldRun() && !canceled {
			if nextSpawn.Before(time.Now()) {
				nextSpawn = time.Now().Add(period)

				if runningCount < brust {
					atomic.AddUint32(&runningCount, 1)
					a.Go(func() {
						defer func() {
							atomic.AddUint32(&runningCount, uint32(0xFFFFFFFF))
						}()
						proc(cancel, nextSpawn)
					})
				}
			}

			sleepTimeout := nextSpawn.Sub(time.Now())
			if sleepTimeout > time.Second {
				sleepTimeout = time.Second
			}
			time.Sleep(sleepTimeout)
		}
	})

	return
}

// Do calls the proc.
func (a *Arbiter) Do(proc func()) *Arbiter {
	atomic.AddInt32(&a.runningCount, 1)
	defer func() {
		a.sigFibreExit <- struct{}{}
	}()
	if a.ShouldRun() {
		proc()
	}
	return a
}

// ShouldRun is called by goroutines traced by Arbiter, indicating whether the goroutines can continue to execute.
func (a *Arbiter) ShouldRun() bool {
	select {
	case <-a.ctx.Done():
		return false
	default:
	}
	return true
}

// Exit returns a channel that is closed when arbiter is shutdown.
func (a *Arbiter) Exit() <-chan struct{} {
	return a.ctx.Done()
}

// Context return context for goroutine control.
func (a *Arbiter) Context() context.Context {
	return a.ctx
}

// Shutdown shuts the arbiter, sending exit signal to all goroutines and executions.
func (a *Arbiter) Shutdown() {
	a.Do(func() { a.shutdown() })
}

func (a *Arbiter) shutdown() {
	a.cancel()
}

// StopOSSignals chooses OS signals to shut the arbiter.
func (a *Arbiter) StopOSSignals(stopSignals ...os.Signal) *Arbiter {
	signal.Notify(a.sigOS, stopSignals...)
	return a
}

// Join waits until all goroutines exited (sync mode).
// NOTE: Not less then one goroutines should Join() arbiter.
func (a *Arbiter) Join() {
	<-a.lock
	defer func() { a.lock <- struct{}{} }()

	if a.ShouldRun() {
		a.Go(func() { a.sigFibreExit <- <-a.Exit() })

		for a.runningCount > 0 {
			select {
			case <-a.sigFibreExit:
				atomic.AddInt32(&a.runningCount, -1)

			case <-a.sigOS:
				if a.ShouldRun() {
					a.shutdown()
					preStop := a.preStop
					if preStop != nil {
						preStop()
					}
				}
			}
		}
		afterStop := a.afterStop
		if afterStop != nil {
			afterStop()
		}
	}
}

// Arbit configures SIGKILL and SIGINT as shutting down signal and waits until all goroutines exited.
func (a *Arbiter) Arbit() error {
	a.StopOSSignals(os.Kill, os.Interrupt)
	a.Join()
	return nil
}

// HookPreStop inserts pre-stop (a shutdown triggered) callback function.
func (a *Arbiter) HookPreStop(proc func()) *Arbiter {
	a.preStop = proc
	return a
}

// HookStopped inserts after-stop (all goroutines and executions finished) callback function.
func (a *Arbiter) HookStopped(proc func()) *Arbiter {
	a.afterStop = proc
	return a
}
