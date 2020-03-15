package mux

import (
	"io"
	"sync"
	"time"

	arbit "git.uestc.cn/sunmxt/utt/arbiter"
	logging "github.com/sirupsen/logrus"
)

// Drainer provides a more efficient stream sending algorithm
// to reduce context switching overload.
type Drainer struct {
	lock sync.Mutex

	buf      []byte
	ptr      int
	written  uint32
	round    uint32
	winStart time.Time

	w        io.Writer
	arbiter  *arbit.Arbiter
	log      *logging.Entry
	drainSig chan uint32

	MaxLatency        time.Duration // Maximum tolanced writing latency.
	Window            time.Duration
	FastPathThreshold uint32 // rate threshold (Bps) to trigger high throughput mode.
}

// NewDrainer creates new Drainer.
func NewDrainer(arbiter *arbit.Arbiter, log *logging.Entry, w io.Writer, maxBuffer uint32, window time.Duration) (d *Drainer) {
	if log == nil {
		log = logging.WithField("module", "drainer")
	}
	d = &Drainer{
		buf:      make([]byte, maxBuffer),
		w:        w,
		winStart: time.Now(),
		arbiter:  arbit.NewWithParent(arbiter, nil),
		Window:   window,
		drainSig: make(chan uint32),
		log:      log,
	}
	d.goDraining()
	return d
}

func (d *Drainer) write(buf []byte) (written int, err error) {
	d.written += uint32(len(buf))
	d.round++
	return d.w.Write(buf)
}

func (d *Drainer) drain() (err error) {
	if d.ptr < 1 {
		return nil
	}
	written, err := d.write(d.buf[:d.ptr])
	if written < d.ptr {
		copy(d.buf[:d.ptr-written], d.buf[written:d.ptr])
		d.ptr -= written
	} else {
		d.ptr = 0
	}

	return err
}

// Drain flushes buffer.
func (d *Drainer) Drain() (err error) {
	d.lock.Lock()
	defer d.lock.Unlock()

	return d.drain()
}

func (d *Drainer) goDraining() {
	d.arbiter.Go(func() {
		round := d.round

	loopDraining:
		for {
			select {
			case <-d.arbiter.Exit():
				break
			case round = <-d.drainSig:
			}

			for {
				select {
				case <-d.arbiter.Exit():
					break loopDraining
				case round = <-d.drainSig:
					continue
				case <-time.After(d.MaxLatency):
					// drain.
					d.lock.Lock()
					if round == d.round {
						if err := d.drain(); err != nil {
							d.log.Error("goDraining() got error: ", err)
						}
					}
					d.lock.Unlock()
					break
				}
			}
		}
	})
}

func (d *Drainer) getWindow() time.Duration {
	win := d.Window
	if win > 0 {
		return win
	}
	return time.Second * 5
}

func (d *Drainer) isFastMode() bool {
	win := d.getWindow()
	if now := time.Now(); now.After(d.winStart.Add(win)) {
		d.written = 0
		d.winStart = now
	}
	// uint64(d.written) * 1000000000 / uint64(window) >= uint64(d.FastPathThreshold)
	return uint64(d.written)*1000000000 < uint64(d.FastPathThreshold)*uint64(win)
}

func (d *Drainer) Write(buf []byte) (written int, err error) {
	d.arbiter.Do(func() {
		d.lock.Lock()
		defer d.lock.Unlock()

		fast := d.isFastMode()

		for len(buf) > 0 {
			bufCap := len(d.buf)
			if d.ptr < 1 {
				if fast || // fast path: low throughput mode. write directly.
					bufCap <= len(buf) { // fast path: no need to buffer
					w, ierr := d.write(buf)
					if ierr != nil {
						err = ierr
						return
					}
					written += w
					return
				}
			}

			if nextPtr := d.ptr + len(buf); nextPtr >= bufCap { // cut off
				rest := bufCap - d.ptr
				copy(d.buf[d.ptr:bufCap], buf[:rest]) // fill
				written += rest

				// buffer is full now.
				if _, err = d.write(d.buf); err != nil {
					return
				}
				d.ptr, buf = 0, buf[rest:] // move to the slice to process next.

			} else {
				// not cut off.
				copy(d.buf[d.ptr:nextPtr], buf) // fill
				written += len(buf)
				d.ptr, buf = nextPtr, nil
				if fast {
					// entering fast mode. drain immediately.
					if _, err = d.write(d.buf[:d.ptr]); err != nil {
						return
					}
					d.ptr = 0
				} else {
					// send a signal to trigger lazy writing.
					d.lock.Unlock()
					d.drainSig <- d.round
					d.lock.Lock()
				}
			}
		}
	})

	return
}

// Close closes writer.
func (d *Drainer) Close() error {
	arbiter := d.arbiter
	if arbiter == nil {
		return nil
	}
	d.arbiter.Shutdown()
	d.arbiter.Join()

	d.lock.Lock()
	defer d.lock.Unlock()
	if d.arbiter != nil {
		d.arbiter = nil
		if d.drainSig != nil {
			close(d.drainSig)
			d.drainSig = nil
		}
	}
	return nil
}
