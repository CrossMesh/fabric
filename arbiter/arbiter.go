package arbiter

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
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
	parent   *Arbiter

	preStop   func()
	afterStop func()

	log *logging.Entry
}

// NewWithParent creates a new arbiter atteched to specified parent arbiter.
// The arbiter will be shut down by the parent or a call to Arbiter.Shutdown().
func NewWithParent(parent *Arbiter, log *logging.Entry) *Arbiter {
	if parent != nil && log == nil {
		log = parent.log
	}
	if log == nil {
		log = logging.WithField("module", "arbiter")
	}
	a := &Arbiter{
		sigFibreExit: make(chan struct{}, 10),
		sigOS:        make(chan os.Signal, 0),
		log:          log,
		lock:         make(chan struct{}, 1),
		parent:       parent,
	}
	a.lock <- struct{}{}

	var parentCtx context.Context
	if parent != nil {
		parentCtx = parent.ctx
	} else {
		parentCtx = context.Background()

	}
	a.ctx, a.cancel = context.WithCancel(parentCtx)

	if parent != nil {
		// join parent.
		parent.Go(func() {
			parent.children.Store(a, struct{}{})
			a.Join()
			parent.children.Delete(a)
		})
	}

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
		proc()
	}()
	return a
}

// TickGo spawns proc with specified period.
func (a *Arbiter) TickGo(proc func(func(), time.Time), period time.Duration, brust uint32) (cancel func()) {
	if brust < 1 {
		return
	}
	var tickCtx context.Context
	ticker := time.NewTicker(period)
	tickCtx, cancel = context.WithCancel(a.ctx)

	a.Go(func() {
		defer ticker.Stop()
		defer cancel()
		<-tickCtx.Done()
	})

	for idx := uint32(0); idx < brust; idx++ {
		a.Go(func() {
			for {
				select {
				case <-ticker.C:
					a.Do(func() { proc(cancel, time.Now().Add(period)) })

				case <-tickCtx.Done():
					return
				}
			}
		})
	}
	return
}

// Do calls the proc.
func (a *Arbiter) Do(proc func()) *Arbiter {
	atomic.AddInt32(&a.runningCount, 1)
	defer func() {
		a.sigFibreExit <- struct{}{}
	}()
	proc()
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
func (a *Arbiter) Join() {
	<-a.lock
	defer func() { a.lock <- struct{}{} }()

	if a.ShouldRun() {
		a.Go(func() { <-a.Exit() })

		c := a.runningCount
		for c > 0 {
			select {
			case <-a.sigFibreExit:
				c = atomic.AddInt32(&a.runningCount, -1)

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
	a.StopOSSignals(syscall.SIGTERM, syscall.SIGINT)
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
