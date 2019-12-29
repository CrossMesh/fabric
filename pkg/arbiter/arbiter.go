package arbiter

import (
	"os"
	"os/signal"
	"sync/atomic"

	log "github.com/sirupsen/logrus"
)

type Arbiter struct {
	runningCount uint32
	running      bool
	sigFibreExit chan struct{}
	log          *log.Entry
}

func New(log *log.Entry) *Arbiter {
	return &Arbiter{
		sigFibreExit: make(chan struct{}, 10),
		log:          log,
		running:      true,
	}
}

func (a *Arbiter) Go(proc func()) {
	go func() {
		atomic.AddUint32(&a.runningCount, 1)
		defer func() {
			a.sigFibreExit <- struct{}{}
		}()
		proc()
	}()
}

func (a *Arbiter) ShouldRun() bool { return a.running }

func (a *Arbiter) Shutdown() {
	a.running = false
	atomic.AddUint32(&a.runningCount, 1)
	a.sigFibreExit <- struct{}{}
}

func (a *Arbiter) Wait() {
	for a.runningCount > 0 {
		select {
		case <-a.sigFibreExit:
			a.runningCount--
		}
	}
}

func (a *Arbiter) Arbit() error {
	sig := make(chan os.Signal)
	signal.Notify(sig, os.Interrupt, os.Kill)

Arbiting:
	for {
		select {
		case s := <-sig:
			if (s == os.Interrupt || s == os.Kill) && a.running {
				a.running = false
				if a.log != nil {
					a.log.Info("shutting down...")
				}
			}

		case <-a.sigFibreExit:
			a.runningCount--
		}
		if a.runningCount < 1 {
			break Arbiting
		}
	}

	if a.log != nil {
		a.log.Info("exiting...")
	}
	return nil
}
