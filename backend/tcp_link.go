package backend

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"git.uestc.cn/sunmxt/utt/mux"
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
	muxer   mux.Muxer
	demuxer mux.Demuxer
	w       io.Writer

	conn    *net.TCPConn
	crypt   cipher.Block
	remote  *net.TCPAddr
	publish string

	// read buffer.
	readLock  sync.Mutex
	buf       []byte
	cursor    int
	maxCursor int

	lock sync.RWMutex

	backend *TCP
}

func newTCPLink(backend *TCP) (r *TCPLink) {
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
	l.remote, l.conn, l.publish = right.remote, right.conn, right.publish
	l.backend = right.backend
	right.reset()

	return true
}

func (l *TCPLink) read(emit func(frame []byte) bool) (err error) {
	conn, feed, cont := l.conn, 0, true
	if conn == nil {
		return ErrConnectionClosed
	}
	l.readLock.Lock()
	defer l.readLock.Unlock()
	for cont && err == nil {

		if l.maxCursor <= 0 {
			// 1. read stream
			feed, err = conn.Read(l.buf)
			if err != nil {
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
		return ErrOperationCanceled
	}

	l.lock.Lock()
	defer l.lock.Unlock()

	muxer := l.muxer
	if muxer == nil {
		// got closed link.
		err = ErrOperationCanceled
		return
	}

	if _, err = muxer.Mux(frame); err != nil {
		if nerr, ok := err.(net.Error); err == io.EOF || (ok && nerr.Timeout()) {
			err = ErrOperationCanceled
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
	if l.backend.config.EnableDrainer {
		bufferSize, latency, statisticWindow, threshold := l.backend.config.MaxDrainBuffer, l.backend.config.MaxDrainLatancy, l.backend.config.DrainStatisticWindow, l.backend.config.BulkThreshold
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
	return l.close()
}

func (t *TCP) connect(addr *net.TCPAddr, publish string) (l *TCPLink, err error) {
	if !t.Arbiter.ShouldRun() {
		return nil, ErrOperationCanceled
	}

	// get link.
	key := publish
	link := t.getLink(key)
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

	ctx, cancel := context.WithTimeout(t.Arbiter.Context(), t.getConnectTimeout())
	defer cancel()

	t.log.Infof("connecting to %v(%v)", publish, addr.String())
	// dial
	dialer := net.Dialer{}
	if conn, ierr := dialer.DialContext(ctx, "tcp", addr.String()); ierr != nil {
		if t.Arbiter.ShouldRun() {
			t.log.Error(ierr)
		}
		return nil, ierr

	} else if tcpConn, isTCP := conn.(*net.TCPConn); !isTCP {
		t.log.Error("got non-tcp connection")
		return nil, ErrNonTCPConnection

	} else {
		link.conn = tcpConn
	}

	connID := atomic.AddUint32(&t.connID, 1)
	log := t.log.WithField("conn_id", connID)
	// handshake
	var accepted bool
	if accepted, err = t.connectHandshake(ctx, log, link); err != nil {
		log.Error("handshake failure: ", err)
		link.close()
		return nil, err
	}
	if !accepted {
		log.Error("denied by remote peer.")
		link.close()
		return nil, err
	}

	t.goTCPLinkDaemon(log, key, link)

	return link, nil
}

// Connect trys to establish data path to peer.
func (t *TCP) Connect(endpoint string) (l Link, err error) {
	var (
		addr *net.TCPAddr
		link *TCPLink
	)
	if addr, err = t.resolve(endpoint); err != nil {
		return nil, err
	}
	if link, err = t.connect(addr, endpoint); err != nil {
		return nil, err
	}
	return link, err
}

func (t *TCP) forwardProc(log *logging.Entry, key string, link *TCPLink) {
	var err error

	for t.Arbiter.ShouldRun() {
		if err = link.conn.SetDeadline(time.Now().Add(time.Second * 3)); err != nil {
			log.Info("conn.SetDeadline() error: ", err)
			break
		}
		if err = link.read(func(frame []byte) bool {
			// deliver frame to all watchers.
			t.watch.Range(func(k, v interface{}) bool {
				if emit, ok := v.(func(Backend, []byte, string)); ok {
					emit(t, frame, link.publish)
				}
				return true
			})
			return true
		}); err != nil {
			// handle errors.
			if err == io.EOF {
				log.Info("connection closed by peer.")
				break
			}
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				err = nil
			}
		}
	}
}

func (t *TCP) goTCPLinkDaemon(log *logging.Entry, key string, link *TCPLink) {
	var (
		routines uint32
		err      error
	)

	log.Infof("link to foreign peer \"%v\" established.", key)
	// TCP options.
	if bufSize := t.config.SendBufferSize; bufSize > 0 {
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

	if keepalivePeriod := t.config.KeepalivePeriod; keepalivePeriod > 0 {
		if err = link.conn.SetKeepAlivePeriod(time.Second * time.Duration(keepalivePeriod)); err != nil {
			log.Error("conn.SetKeepalivePeriod error: ", err)
			return
		}
	}

	// spwan.
	for n := t.getRoutinesCount(); n > 0; n-- {
		t.Arbiter.Go(func() {
			atomic.AddUint32(&routines, 1)
			defer func() {
				if last := atomic.AddUint32(&routines, 0xFFFFFFFF); last == 0 {
					link.Close()
				}
			}()
			t.forwardProc(log, key, link)
		})
	}
}
