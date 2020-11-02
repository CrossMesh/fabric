package tcp

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/crossmesh/fabric/metanet/backend"
	"github.com/crossmesh/fabric/mux"
	logging "github.com/sirupsen/logrus"
)

const (
	DefaultDrainBufferSize      = 8 * 1024 * 1024
	DefaultDrainStatisticWindow = 1000
	DefaultDrainLatency         = 500
	DefaultBulkThreshold        = 2 * 1024 * 1024
)

// TCPLink maintains data path between two peer.
type TCPLink struct {
	lock    sync.RWMutex
	conn    *net.TCPConn
	crypt   cipher.Block
	remote  *net.TCPAddr
	publish string

	// write context.
	writeLock sync.Mutex
	muxer     mux.Muxer
	w         io.Writer

	// read context.
	demuxer   mux.Demuxer
	readLock  sync.Mutex
	buf       []byte
	cursor    int
	maxCursor int

	backend *tcpEndpoint
}

func newTCPLink(backend *tcpEndpoint) (r *TCPLink) {
	r = &TCPLink{
		backend:   backend,
		buf:       make([]byte, defaultBufferSize),
		cursor:    0,
		maxCursor: 0,
	}
	return r
}

func (l *TCPLink) reset() {
	l.cursor, l.maxCursor = 0, 0
	l.demuxer, l.muxer, l.crypt = nil, nil, nil
	l.remote, l.conn = nil, nil
	l.publish = ""
	l.w = nil
}

// Active determines whether link is avaliable.
func (l *TCPLink) Active() bool {
	return l.conn != nil
}

func (l *TCPLink) assign(right *TCPLink) bool {
	if right == nil {
		return false
	}
	l.lock.Lock()
	defer l.lock.Unlock()
	if l.Active() {
		return false
	}
	right.lock.Lock()
	defer right.lock.Unlock()
	if !right.Active() {
		return false
	}
	l.cursor, l.maxCursor = right.cursor, right.maxCursor
	l.crypt, l.demuxer, l.muxer = right.crypt, right.demuxer, right.muxer
	l.buf = right.buf
	l.remote, l.conn, l.publish = right.remote, right.conn, right.publish
	l.backend = right.backend
	l.w = right.w

	right.reset()

	return true
}

func (l *TCPLink) read(emit func(frame []byte) bool) (err error) {
	conn, feed, cont := l.conn, 0, true
	if conn == nil {
		return backend.ErrConnectionClosed
	}
	l.readLock.Lock()
	defer l.readLock.Unlock()
	for cont && err == nil {

		if l.maxCursor <= 0 {
			// 1. read stream
			if feed, err = conn.Read(l.buf); err != nil {
				break
			}
			l.cursor, l.maxCursor = 0, feed
		} else {
			// 2. feed demuxer
			feed, err = l.demuxer.Demux(l.buf[l.cursor:l.maxCursor], func(frame []byte) bool {
				cont = emit(frame)
				return cont
			})
			l.cursor += feed
		}
		if l.cursor == l.maxCursor {
			l.cursor, l.maxCursor = 0, 0
		}
	}
	return
}

// Send sends data frame.
func (l *TCPLink) Send(frame []byte) (err error) {
	t := l.backend
	if t == nil {
		return backend.ErrOperationCanceled
	}

	muxer := l.muxer
	if muxer == nil {
		return backend.ErrOperationCanceled
	}

	if !muxer.Parallel() {
		l.writeLock.Lock()
		defer l.writeLock.Unlock()
	}

	if _, err = muxer.Mux(frame); err != nil {
		if nerr, ok := err.(net.Error); err == io.EOF || (ok && nerr.Timeout()) {
			err = backend.ErrOperationCanceled
		} else {
			// close corrupted link.
			t.log.Error("mux error: ", err)
			l.Close()
		}
	}
	return
}

// InitializeAESGCM initializes AES-256-GCM muxer and demuxer.
func (l *TCPLink) InitializeAESGCM(key []byte, nonce []byte) (err error) {
	if l.crypt, err = aes.NewCipher(key[:]); err != nil {
		return err
	}
	l.initializeWriter()
	if l.muxer, err = mux.NewGCMStreamMuxer(l.w, l.crypt, nonce); err != nil {
		return err
	}
	if l.demuxer, err = mux.NewGCMStreamDemuxer(l.crypt, nonce); err != nil {
		return err
	}
	return nil
}

func (l *TCPLink) initializeWriter() {
	if l.backend.parameters.Driner.EnableDrainer {
		bufferSize, latency, statisticWindow, threshold := l.backend.parameters.Driner.MaxDrainBuffer,
			l.backend.parameters.Driner.MaxDrainLatancy, l.backend.parameters.Driner.DrainStatisticWindow, l.backend.parameters.Driner.BulkThreshold
		if bufferSize < 1 {
			bufferSize = DefaultDrainBufferSize
		}
		if statisticWindow < 1 {
			statisticWindow = DefaultDrainStatisticWindow
		}
		if latency < 1 {
			statisticWindow = DefaultDrainLatency
		}
		if threshold < 1 {
			threshold = DefaultBulkThreshold
		}
		drainer := mux.NewDrainer(l.backend.Arbiter, nil, l.conn, bufferSize, time.Millisecond*time.Duration(statisticWindow))
		drainer.MaxLatency = time.Duration(statisticWindow) * time.Microsecond
		drainer.FastPathThreshold = threshold
		l.w = drainer
	} else {
		l.w = l.conn
	}
}

// InitializeNoCryption initializes normal muxer and demuxer without encryption.
func (l *TCPLink) InitializeNoCryption() {
	l.initializeWriter()
	l.muxer, l.demuxer = mux.NewStreamMuxer(l.w), mux.NewStreamDemuxer()
}

func (l *TCPLink) close() (err error) {
	// TODO(xutao): fix race.
	conn := l.conn
	if conn != nil {
		conn.Close()
		l.reset()
	}
	return
}

// Close terminates link.
func (l *TCPLink) Close() (err error) {
	l.lock.Lock()
	defer l.lock.Unlock()

	l.writeLock.Lock()
	defer l.writeLock.Unlock()

	l.readLock.Lock()
	defer l.readLock.Unlock()

	return l.close()
}

func (e *tcpEndpoint) connect(addr *net.TCPAddr, publish string) (l *TCPLink, err error) {
	if !e.Arbiter.ShouldRun() {
		return nil, backend.ErrOperationCanceled
	}

	// get link.
	key := publish
	link := e.getLink(key)
	if link.conn != nil {
		// fast path: link is valid.
		return link, nil
	}

	link.lock.Lock()
	if link.conn != nil {
		// fast path: link is valid.
		link.lock.Unlock()
		return link, nil
	}

	defer link.lock.Unlock()
	ctx, cancel := context.WithTimeout(e.Arbiter.Context(), e.getConnectTimeout())
	defer cancel()

	e.log.Infof("connecting to %v(%v)", publish, addr.String())
	// dial
	dialer := net.Dialer{}
	if conn, ierr := dialer.DialContext(ctx, "tcp", addr.String()); ierr != nil {
		if e.Arbiter.ShouldRun() {
			e.log.Error(ierr)
		}
		return nil, ierr

	} else if tcpConn, isTCP := conn.(*net.TCPConn); !isTCP {
		e.log.Error("got non-tcp connection")
		return nil, ErrNonTCPConnection

	} else {
		link.conn = tcpConn
		link.publish = addr.String()
	}

	connID := atomic.AddUint32(&e.connID, 1)
	log := e.log.WithField("conn_id", connID)
	// handshake
	var accepted bool
	if accepted, err = e.connectHandshake(ctx, log, link); err != nil {
		log.Error("handshake failure: ", err)
		link.close()
		return nil, err
	}
	if !accepted {
		log.Error("denied by remote peer.")
		link.close()
		return nil, err
	}

	e.goTCPLinkDaemon(log, key, link)

	return link, nil
}

// Connect trys to establish data path to peer.
func (e *tcpEndpoint) Connect(endpoint string) (l backend.Link, err error) {
	var (
		addr *net.TCPAddr
		link *TCPLink
	)
	if addr, err = e.resolve(endpoint); err != nil {
		return nil, err
	}
	if link, err = e.connect(addr, endpoint); err != nil {
		return nil, err
	}
	return link, err
}

func (e *tcpEndpoint) forwardProc(log *logging.Entry, key string, link *TCPLink) {
	var err error

	for e.Arbiter.ShouldRun() {
		conn := link.conn
		if conn == nil {
			break
		}
		if err = link.conn.SetReadDeadline(time.Now().Add(time.Second * 3)); err != nil {
			log.Error("conn.SetReadDeadline() error: ", err)
			break
		}
		if err = link.read(func(frame []byte) bool {
			// deliver frame to all watchers.
			e.watch.Range(func(k, v interface{}) bool {
				if emit, ok := v.(func(backend.Backend, []byte, string)); ok {
					emit(e, frame, link.publish)
				}
				return true
			})
			return true
		}); err != nil {
			// handle errors.
			if err == io.EOF { // connection closed.
				break
			}
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				err = nil
			}
		}
	}
}

func (e *tcpEndpoint) goTCPLinkDaemon(log *logging.Entry, key string, link *TCPLink) {
	var (
		routines uint32
		err      error
	)

	log.Infof("link to foreign peer \"%v\" established.", key)
	// clear any deadline.
	if err = link.conn.SetDeadline(time.Time{}); err != nil {
		log.Error("conn.SetDeadline error: ", err)
		return
	}
	if err = link.conn.SetWriteDeadline(time.Time{}); err != nil {
		log.Error("conn.SetWriteDeadline error: ", err)
		return
	}
	if err = link.conn.SetReadDeadline(time.Time{}); err != nil {
		log.Error("conn.SetReadDeadline error: ", err)
		return
	}
	// TCP options.
	if bufSize := e.parameters.SendBufferSize; bufSize > 0 {
		if err = link.conn.SetWriteBuffer(bufSize); err != nil {
			log.Error("conn.SetWriteBuffer error: ", err)
			return
		}
	}
	if err = link.conn.SetNoDelay(true); err != nil {
		log.Error("conn.SetNoDelay error: ", err)
		return
	}
	if err = link.conn.SetKeepAlive(true); err != nil {
		log.Error("conn.SetKeepalive error: ", err)
		return
	}

	if keepalivePeriod := e.parameters.KeepalivePeriod; keepalivePeriod > 0 {
		if err = link.conn.SetKeepAlivePeriod(time.Second * time.Duration(keepalivePeriod)); err != nil {
			log.Error("conn.SetKeepalivePeriod error: ", err)
			return
		}
	}

	// spwan.
	for n := e.getRoutinesCount(); n > 0; n-- {
		e.Arbiter.Go(func() {
			atomic.AddUint32(&routines, 1)
			defer func() {
				if last := atomic.AddUint32(&routines, 0xFFFFFFFF); last == 0 {
					link.Close()
				}
			}()
			e.forwardProc(log, key, link)
		})
	}
}
