package arbiter

import (
	"os"
	"os/signal"
	"sync/atomic"
	"time"

	logging "github.com/sirupsen/logrus"
)

// Arbiter is tracer to manage lifecycle of goroutines.
type Arbiter struct {
	runningCount int32
	running      bool

	sigFibreExit chan struct{}
	sigPreStop   chan struct{}
	sigOS        chan os.Signal

	preStop   func()
	afterStop func()

	log *logging.Entry

	parent *Arbiter
}

// NewWithParent create a new arbiter atteched to specified parent arbiter.
// The arbiter will be shut down by the parent or a call to Arbiter.Shutdown().
func NewWithParent(parent *Arbiter, log *logging.Entry) *Arbiter {
	if log == nil {
		log = logging.WithField("module", "arbiter")
	}
	a := &Arbiter{
		sigFibreExit: make(chan struct{}, 10),
		sigPreStop:   make(chan struct{}, 1),
		sigOS:        make(chan os.Signal, 0),
		log:          log,
		running:      true,
		parent:       parent,
	}
	a.sigPreStop <- struct{}{}
	return a
}

// New create a new arbiter.
func New(log *logging.Entry) *Arbiter {
	return NewWithParent(nil, log)
}

func (a *Arbiter) Reset() {
	a.Join(false)
	a.running = true
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

func (a *Arbiter) TickGo(proc func(func()), period time.Duration, brust uint32) (cancel func()) {
	if brust < 1 {
		return
	}

	runningCount, canceled := uint32(0), false
	cancel = func() { canceled = true } // cancel function.
	a.Go(func() {
		nextSpawn := time.Now()

		for a.ShouldRun() && !canceled {
			if nextSpawn.Before(time.Now()) {
				if runningCount < brust {
					atomic.AddUint32(&runningCount, 1)
					a.Go(func() {
						defer func() {
							atomic.AddUint32(&runningCount, uint32(0xFFFFFFFF))
						}()
						proc(cancel)
					})
				}
				nextSpawn = time.Now().Add(period)
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
	if a.parent != nil {
		return a.parent.ShouldRun() && a.running
	}
	return a.running
}

// Shutdown shuts the arbiter.
func (a *Arbiter) Shutdown() {
	a.shutdown()
	atomic.AddInt32(&a.runningCount, 1)
	a.sigFibreExit <- struct{}{}
}

func (a *Arbiter) shutdown() {
	a.running = false
}

// StopOSSignals chooses OS signals to shut the arbiter.
func (a *Arbiter) StopOSSignals(stopSignals ...os.Signal) *Arbiter {
	signal.Notify(a.sigOS, stopSignals...)
	return a
}

func (a *Arbiter) join() {
	<-a.sigPreStop
	defer func() {
		a.sigPreStop <- struct{}{}
	}()

	if a.runningCount > 0 {
		for a.runningCount > 0 {
			select {
			case <-a.sigFibreExit:
				a.runningCount--

			case <-a.sigOS:
				if a.running {
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

// Join waits until all goroutines exited (sync mode).
// NOTE: Not less then one goroutines should Join() arbiter.
func (a *Arbiter) Join(async bool) {
	if !async {
		go a.join()
		return
	}
	a.join()
}

// Arbit configures SIGKILL and SIGINT as shutting down signal and waits until all goroutines exited.
func (a *Arbiter) Arbit() error {
	a.StopOSSignals(os.Kill, os.Interrupt)
	a.join()
	return nil
}

// HookPreStop inserts pre-stop (a shutdown triggered) callback function.
func (a *Arbiter) HookPreStop(proc func()) *Arbiter {
	a.preStop = proc
	return a
}

// HookStopped inserts after-stop (all goroutines existed) callback function.
func (a *Arbiter) HookStopped(proc func()) *Arbiter {
	a.afterStop = proc
	return a
}
