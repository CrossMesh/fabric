package arbiter

import (
	"os"
	"os/signal"
	"sync/atomic"

	logging "github.com/sirupsen/logrus"
)

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

func New(log *logging.Entry) *Arbiter {
	return NewWithParent(nil, log)
}

func (a *Arbiter) Go(proc func()) {
	atomic.AddInt32(&a.runningCount, 1)
	go func() {
		defer func() {
			a.sigFibreExit <- struct{}{}
		}()
		proc()
	}()
}

func (a *Arbiter) Do(proc func()) {
	atomic.AddInt32(&a.runningCount, 1)
	defer func() {
		a.sigFibreExit <- struct{}{}
	}()
	proc()
}

func (a *Arbiter) ShouldRun() bool {
	if a.parent != nil {
		return a.parent.ShouldRun() && a.running
	}
	return a.running
}

func (a *Arbiter) Shutdown() {
	a.shutdown()
	atomic.AddInt32(&a.runningCount, 1)
	a.sigFibreExit <- struct{}{}
}

func (a *Arbiter) shutdown() {
	a.running = false
}

func (a *Arbiter) StopOSSignals(stopSignals ...os.Signal) {
	signal.Notify(a.sigOS, stopSignals...)
}

func (a *Arbiter) Join() {
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

func (a *Arbiter) Arbit() error {
	a.StopOSSignals(os.Kill, os.Interrupt)
	a.Join()
	return nil
}

func (a *Arbiter) HookPreStop(proc func()) {
	a.preStop = proc
}

func (a *Arbiter) HookStopped(proc func()) {
	a.afterStop = proc
}
