package backend

import (
	"crypto/aes"
	"crypto/cipher"
	"net"
	"time"

	"git.uestc.cn/sunmxt/utt/mux"
)

// TCPLink maintains data path between two peer.
type TCPLink struct {
	muxer   mux.Muxer
	demuxer mux.Demuxer
	conn    *net.TCPConn
	crypt   cipher.Block
	remote  *net.TCPAddr
	publish string

	// read buffer.
	buf       []byte
	cursor    int
	maxCursor int

	lock chan struct{}

	backend *TCP
}

func newTCPLink(backend *TCP) (r *TCPLink) {
	r = &TCPLink{
		lock:      make(chan struct{}, 1),
		backend:   backend,
		buf:       make([]byte, defaultBufferSize),
		cursor:    0,
		maxCursor: 0,
	}
	r.lock <- struct{}{}
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
	<-l.lock
	defer func() { l.lock <- struct{}{} }()
	if l.Active() {
		return false
	}
	<-right.lock
	defer func() { right.lock <- struct{}{} }()
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

	select {
	case <-time.After(t.getSendTimeout()):
		err = ErrOperationCanceled
		return
	case <-l.lock:
		defer func() { l.lock <- struct{}{} }()
	}

	t.Arbiter.Do(func() {
		muxer := l.muxer
		if muxer == nil {
			// got closed link.
			err = ErrOperationCanceled
			return
		}
		_, err = muxer.Mux(frame)
	})

	if err != nil {
		if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
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
	if l.muxer, err = mux.NewGCMStreamMuxer(l.conn, l.crypt, nonce); err != nil {
		return err
	}
	if l.demuxer, err = mux.NewGCMStreamDemuxer(l.crypt, nonce); err != nil {
		return err
	}
	return nil
}

// InitializeNoCryption initializes normal muxer and demuxer without encryption.
func (l *TCPLink) InitializeNoCryption() {
	l.muxer, l.demuxer = mux.NewStreamMuxer(l.conn), mux.NewStreamDemuxer()
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
	<-l.lock
	defer func() { l.lock <- struct{}{} }()
	return l.close()
}
