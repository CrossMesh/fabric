package backend

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	arbit "git.uestc.cn/sunmxt/utt/arbiter"
	"git.uestc.cn/sunmxt/utt/config"
	"git.uestc.cn/sunmxt/utt/mux"
	"git.uestc.cn/sunmxt/utt/proto"
	"git.uestc.cn/sunmxt/utt/proto/pb"
	logging "github.com/sirupsen/logrus"
)

func validTCPPublishEndpoint(arr *net.TCPAddr) bool {
	if arr == nil {
		return false
	}
	return arr.IP.IsGlobalUnicast()
}

const (
	defaultSendTimeout    = 50
	defaultConnectTimeout = 15
)

type TCPBackendConfig struct {
	Bind      string      `json:"bind" yaml:"bind"`
	Publish   string      `json:"publish" yaml:"publish"`
	Priority  uint32      `json:"priority" yaml:"priority"`
	StartCode interface{} `json:"startCode" yaml:"startCode"`

	SendTimeout     uint32 `json:"sendTimeout" yaml:"sendTimeout" default:"50"`
	SendBufferSize  int    `json:"sendBuffer" yaml:"sendBuffer" default:"0"`
	KeepalivePeriod int    `json:"keepalivePeriod" yaml:"keepalivePeriod" default:"60"`
	ConnectTimeout  uint32 `json:"connectTimeout" yaml:"connectTimeout" default:"15"`

	Encrypt bool `json:"-" yaml:"-"`
}

type tcpCreator struct {
	cfg TCPBackendConfig
	raw *config.Backend
}

func newTCPCreator(cfg *config.Backend) (BackendCreator, error) {
	c := &tcpCreator{}
	if cfg.Parameters == nil {
		return nil, ErrInvalidBackendConfig
	}
	// re-parse
	bin, err := json.Marshal(cfg.Parameters)
	if err != nil {
		return nil, fmt.Errorf("parse backend config failure (%v)", err)
	}
	if err = json.Unmarshal(bin, &c.cfg); err != nil {
		return nil, fmt.Errorf("parse backend config failure (%v)", err)
	}
	c.cfg.Encrypt = cfg.Encrypt
	c.raw = cfg
	return c, nil
}

func (c *tcpCreator) Type() pb.PeerBackend_BackendType { return pb.PeerBackend_TCP }
func (c *tcpCreator) Priority() uint32                 { return c.cfg.Priority }
func (c *tcpCreator) Publish() string                  { return c.cfg.Publish }
func (c *tcpCreator) New(arbiter *arbit.Arbiter, log *logging.Entry) (Backend, error) {
	return NewTCP(arbiter, log, &c.cfg, &c.raw.PSK)
}

type TCPLink struct {
	muxer   mux.Muxer
	demuxer mux.Demuxer
	conn    *net.TCPConn
	crypt   cipher.Block
	remote  *net.TCPAddr
	publish string

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

func (l *TCPLink) read(emit func(frame []byte) bool) (err error) {
	conn, feed, cont := l.conn, 0, true
	if conn == nil {
		return ErrConnectionClosed
	}
	for cont && err == nil {
		if l.maxCursor > 0 {
			// feed demuxer.
			feed, err = l.demuxer.Demux(l.buf[l.cursor:l.maxCursor], func(frame []byte) bool {
				cont = emit(frame)
				return cont
			})
			l.cursor += feed
		} else {
			// read stream.
			feed, err = conn.Read(l.buf)
			if err != nil {
				break
			}
			l.cursor, l.maxCursor = 0, feed
		}
		if l.cursor == l.maxCursor {
			l.cursor, l.maxCursor = 0, 0
		}
	}
	return
}

func (l *TCPLink) Send(frame []byte) (err error) {
	t := l.backend

	select {
	case <-time.After(t.getSendTimeout()):
		return ErrOperationCanceled
	case <-l.lock:
		defer func() { l.lock <- struct{}{} }()
	}

	t.Arbiter.Do(func() {
		_, err = l.muxer.Mux(frame)
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

func (l *TCPLink) InitializeNoCryption() {
	l.muxer, l.demuxer = mux.NewStreamMuxer(l.conn), mux.NewStreamDemuxer()
}

func (l *TCPLink) Close() (err error) {
	conn := l.conn
	if conn != nil {
		conn.Close()
		l.backend.link.Delete(l.publish)
		l.conn = nil
	}
	return
}

type TCP struct {
	bind     *net.TCPAddr
	publish  *net.TCPAddr
	listener *net.TCPListener

	config *TCPBackendConfig
	psk    *string

	log *logging.Entry

	link         sync.Map
	resolveCache sync.Map

	watch  sync.Map
	connID uint32

	Arbiter *arbit.Arbiter
}

var (
	ErrNonTCPConnection = errors.New("got non-tcp connection")
	ErrTCPAmbigousRole  = errors.New("ambigous tcp role")
)

func NewTCP(arbiter *arbit.Arbiter, log *logging.Entry, cfg *TCPBackendConfig, psk *string) (t *TCP, err error) {
	if log == nil {
		log = logging.WithField("module", "backend_tcp")
	}
	t = &TCP{
		psk:    psk,
		config: cfg,
		log:    log,
	}
	if cfg.Publish == "" {
		cfg.Publish = cfg.Bind
	}
	if t.publish, err = net.ResolveTCPAddr("tcp", cfg.Publish); err != nil {
		return nil, err
	}
	if !validTCPPublishEndpoint(t.publish) {
		return nil, fmt.Errorf("cannot publish \"%v\"", cfg.Publish)
	}
	t.Arbiter = arbit.NewWithParent(arbiter, nil)

	t.Arbiter.Go(func() {
		var err error

		for arbiter.ShouldRun() {
			if err != nil {
				log.Info("retry in 3 second.")
				time.Sleep(time.Second * 3)
			}
			err = nil

			if t.bind, err = net.ResolveTCPAddr("tcp", cfg.Bind); err != nil {
				log.Error("resolve bind address failure: ", err)
				continue
			}

			t.serve(arbiter)
		}
	})

	return t, nil
}

func (t *TCP) Priority() uint32 {
	return t.config.Priority
}

func (t *TCP) getSendTimeout() (timeout time.Duration) {
	timeout = time.Microsecond * time.Duration(t.config.SendTimeout)
	if timeout > 0 {
		return
	}
	return time.Duration(defaultSendTimeout) * time.Millisecond
}

func (t *TCP) serve(arbiter *arbit.Arbiter) (err error) {
	for arbiter.ShouldRun() {
		if err != nil {
			time.Sleep(time.Second * 5)
		}
		err = nil

		if t.listener, err = net.ListenTCP("tcp", t.bind); err != nil {
			t.log.Errorf("cannot listen to \"%v\": %v", t.bind.String(), err)
			continue
		}
		t.log.Infof("listening to %v", t.bind.String())

		err = t.acceptConnection(arbiter)
	}
	return
}

func (t *TCP) acceptConnection(arbiter *arbit.Arbiter) (err error) {
	var conn net.Conn

	t.log.Infof("start accepting connection.")

	for arbiter.ShouldRun() {
		if err != nil {
			time.Sleep(time.Second * 5)
		}
		err = nil

		if err = t.listener.SetDeadline(time.Now().Add(time.Second * 3)); err != nil {
			t.log.Error("set deadline error: ", err)
			continue
		}

		if conn, err = t.listener.Accept(); err != nil {
			if err != nil {
				if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
					err = nil
					continue
				}
			}
			t.log.Error("listener.Accept() error: ", err)
			continue
		}
		connID := atomic.AddUint32(&t.connID, 1)

		t.goServeConnection(arbiter, connID, conn)
		conn = nil
	}

	return nil
}

func (t *TCP) goServeConnection(arbiter *arbit.Arbiter, connID uint32, conn net.Conn) {
	log := t.log.WithField("conn_id", connID)
	log.Infof("incomming connection %v from %v", connID, conn.RemoteAddr())

	arbiter.Go(func() {
		accepted, err := t.handshakeConnect(arbiter, log, connID, conn)
		if err != nil {
			log.Errorf("handshake error for connection %v: %v", connID, err)
			return
		}
		if !accepted {
			log.Info("connection deined.")
			conn.Close()
		}
	})

}

func (t *TCP) handshakeConnect(arbiter *arbit.Arbiter, log *logging.Entry, connID uint32, adaptedConn net.Conn) (accepted bool, err error) {
	buf := make([]byte, defaultBufferSize)

	conn, isTCPConn := adaptedConn.(*net.TCPConn)
	if !isTCPConn {
		log.Error("got non-tcp connection. rejected.")
		return false, nil
	}
	// handshake should be finished in 20 seconds.
	if err := conn.SetDeadline(time.Now().Add(time.Second * 20)); err != nil {
		log.Error("conn.SetDeadline() failure: ", err)
		return false, nil
	}

	// wait for hello.
	hello := proto.Hello{}
	switch v := t.config.StartCode.(type) {
	case string:
		hello.Lead = []byte(v)
	case []int:
		hello.Lead = make([]byte, len(v))
		for i := range v {
			if v[i] > 255 || v[i] < 0 {
				hello.Lead = nil
				log.Warn("start code should be in range 0x00 - 0xff. skip.")
				break
			}
		}
	default:
		log.Warn("unknown start code setting. skip.")
	}

	var read int
	// hello message has fixed length.
	for arbiter.ShouldRun() && read < hello.Len() {
		newRead, err := conn.Read(buf[read:cap(buf)])
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				log.Info("deny due to inactivity.")
				return false, nil
			}
			if err == io.EOF {
				log.Info("connection closed by client.")
				return false, nil
			}
			return false, err
		}
		read += newRead
	}
	if !arbiter.ShouldRun() {
		return false, nil
	}
	if err := hello.Decode(buf[:read]); err != nil {
		if err == proto.ErrInvalidPacket {
			return false, nil
		}
		return false, err
	}
	buf = buf[:0]
	if t.psk != nil {
		accepted = hello.Verify([]byte(*t.psk))
	} else {
		accepted = hello.Verify(nil)
	}
	if !accepted {
		log.Info("deined for authentication failure.")
		return accepted, nil
	}
	log.Info("authentication success.")

	// init cipher.
	link := newTCPLink(t)
	link.conn = conn
	buf = buf[:0]
	buf = append(buf, hello.Lead...)
	if t.psk != nil {
		buf = append(buf, []byte(*t.psk)...)
	}
	buf = append(buf, hello.HMAC[:]...)
	key := sha256.Sum256(buf)
	link.conn = conn
	if err = link.InitializeAESGCM(key[:], hello.IV[:]); err != nil {
		log.Error("cipher initializion failure: ", err)
		return false, err
	}

	// welcome
	welcome := proto.Welcome{
		Welcome:  true,
		Identity: t.config.Publish,
	}
	if welcome.Identity == "" {
		err = fmt.Errorf("empty publish endpoint")
		log.Error(err)
		return false, err
	}
	welcome.EncodeMessage("ok")
	buf = welcome.Encode(buf[:0])
	if _, err = link.muxer.Mux(buf); err != nil {
		log.Info("send welcome failure: ", err)
		return false, err
	}

	// wait for connect
	log.Info("negotiate peering information.")
	var connectReq *proto.Connect
	if rerr := link.read(func(frame []byte) bool {
		connectReq = &proto.Connect{}
		if err = connectReq.Decode(frame); err != nil {
			log.Info("corrupted connect handshake packet.")
		}
		return false
	}); rerr != nil {
		if nerr, ok := rerr.(net.Error); ok && nerr.Timeout() {
			log.Info("deny due to inactivity.")
			return false, nil
		}
		if rerr == io.EOF {
			log.Info("connection closed by foreign peer.")
			return false, nil
		}
	}
	if !arbiter.ShouldRun() || err != nil {
		return false, err
	}

	return t.acceptTCPLink(arbiter, log, link, connectReq)
}

func (t *TCP) acceptTCPLink(arbiter *arbit.Arbiter, log *logging.Entry, link *TCPLink, connectArg *proto.Connect) (bool, error) {
	// protocol version
	switch connectArg.Version {
	case proto.ConnectNoCrypt:
		link.InitializeNoCryption()

	case proto.ConnectAES256GCM:
		// let it is.
	default:
		log.Errorf("invalid connecting protocol version %v.", connectArg.Version)
		return false, nil
	}
	log.Infof("protocol version: %v", connectArg.Version)

	// resolve identity.
	addr, err := net.ResolveTCPAddr("tcp", connectArg.Identity)
	if err != nil {
		log.Errorf("cannot resolve \"%v\": %v", connectArg.Identity, err)
		return false, err
	}
	key := fmt.Sprintf("%v:%v", addr.IP.To4().String(), addr.Port)
	if !validTCPPublishEndpoint(addr) {
		log.Info("invalid publish endpoint. deined.")
		return false, nil
	}
	if _, exists := t.link.LoadOrStore(key, link); exists {
		log.Warnf("link to foreign peer \"%v\" exists. closing...", key)
		return false, nil
	}
	link.remote = addr
	link.publish = key
	log.Infof("link to foreign peer \"%v\" established.", key)

	t.goTCPLinkDaemon(log, key, link)

	return true, nil
}

func (t *TCP) goTCPLinkDaemon(log *logging.Entry, key string, link *TCPLink) {
	t.Arbiter.Go(func() {
		defer func() {
			link.Close()
		}()
		var err error

		log.Info("receiver start.")
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

		// pump.
		for t.Arbiter.ShouldRun() {
			if err = link.conn.SetDeadline(time.Now().Add(time.Second * 2)); err != nil {
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
		log.Info("receiver existing...")
	})
}

func (t *TCP) connect(addr *net.TCPAddr, publish string) (link *TCPLink, err error) {
	key := addr.IP.To4().String() + ":" + strconv.FormatInt(int64(addr.Port), 10)
	if v, exists := t.link.Load(key); exists {
		return v.(*TCPLink), nil
	}

	// dial
	t.log.Info("connecting to ", addr.String())
	link = newTCPLink(t)
	link.publish = publish
	done := make(chan struct{}, 1)
	t.Arbiter.Go(func() {
		defer func() { done <- struct{}{} }()

		dialer, connectTimeout := net.Dialer{}, time.Duration(0)
		if connectTimeoutSeconds := t.config.ConnectTimeout; connectTimeoutSeconds > 0 {
			connectTimeout = time.Second * time.Duration(connectTimeoutSeconds)
		} else {
			connectTimeout = time.Duration(defaultConnectTimeout) * time.Second
		}
		deadline := time.Now().Add(connectTimeout)
		dialer.Deadline = deadline

		if conn, ierr := dialer.Dial("tcp", addr.String()); ierr != nil {
			t.log.Error(ierr)
			err = ierr
			return
		} else if tcpConn, isTCP := conn.(*net.TCPConn); !isTCP {
			t.log.Error("got non-tcp connection")
			err = ErrNonTCPConnection
			return
		} else {
			link.conn = tcpConn
		}
		defer func() {
			if err != nil {
				link.Close()
			}
		}()

		connID := atomic.AddUint32(&t.connID, 1)
		log := t.log.WithField("conn_id", connID)

		// handshake
		ctx, canceled := context.WithDeadline(context.TODO(), deadline)
		defer canceled()
		var accepted bool
		if accepted, err = t.connectHandshake(ctx, log, link); err != nil {
			log.Error("handshake failure: ", err)
			return
		}
		if !accepted {
			log.Error("denied by remote peer.")
			return
		}

		if _, exists := t.link.LoadOrStore(key, link); exists {
			link.Close()
			log.Warnf("link to foreign peer \"%v\" exists. closing...", key)
		} else {
			// start receiver for new link.
			t.goTCPLinkDaemon(log, key, link)
		}
	})

	select {
	case <-done:
	case <-time.After(t.getSendTimeout()):
		return nil, ErrOperationCanceled
	}

	return
}

func (t *TCP) connectHandshake(ctx context.Context, log *logging.Entry, link *TCPLink) (accepted bool, err error) {
	buf := make([]byte, defaultBufferSize)

	// deadline.
	deadline, hasDeadline := ctx.Deadline()
	if hasDeadline {
		if err = link.conn.SetDeadline(deadline); err != nil {
			log.Error("conn.SetDeadline() failure: ", err)
			return false, err
		}
	}

	log.Info("handshaking...")
	// hello
	hello := proto.Hello{}
	hello.Refresh()
	switch v := t.config.StartCode.(type) {
	case string:
		hello.Lead = []byte(v)

	case []int:
		hello.Lead = make([]byte, len(v))
		for i := range v {
			if v[i] > 255 || v[i] < 0 {
				hello.Lead = nil
				log.Warn("start code should be in range 0x00 - 0xff. skip.")
				break
			}
		}
	}
	if t.psk != nil {
		hello.Sign([]byte(*t.psk))
	} else {
		hello.Sign(nil)
	}
	buf = hello.Encode(buf[:0])
	if _, err = link.conn.Write(buf); err != nil {
		if err == io.EOF {
			log.Info("connection closed by peer.")
		} else {
			log.Info("send error: ", err)
		}
		return false, err
	}

	// can init cipher now.
	log.Info("initialize cipher.")
	buf = buf[:0]
	buf = append(buf, hello.Lead...)
	if t.psk != nil {
		buf = append(buf, []byte(*t.psk)...)
	}
	buf = append(buf, hello.HMAC[:]...)
	key := sha256.Sum256(buf)
	if err = link.InitializeAESGCM(key[:], hello.IV[:]); err != nil {
		log.Error("cipher initializion failure: ", err)
		return false, err
	}

	// wait for welcome.
	log.Info("wait for authentication.")
	var welcome *proto.Welcome
	if rerr := link.read(func(frame []byte) bool {
		welcome = &proto.Welcome{}
		if err = welcome.Decode(frame); err != nil {
			log.Info("corrupted welcome handshake packet.")
		}
		return false
	}); rerr != nil {
		if nerr, ok := rerr.(net.Error); ok && nerr.Timeout() {
			log.Info("canceled for deadline exceeded.")
			return false, ErrOperationCanceled
		}
		if rerr == io.EOF {
			log.Info("connection closed by foreign peer.")
			return false, ErrConnectionClosed
		}
		log.Info("link read failure: ", rerr)
		return false, rerr
	}
	if err != nil {
		return false, err
	}
	if done := ctx.Done(); done != nil {
		select {
		case <-done:
			return false, ErrOperationCanceled
		default:
		}
	}
	if !welcome.Welcome { // denied.
		return false, nil
	}

	log.Info("good authentication. connecting...")
	// send connect request.
	connectReq := proto.Connect{
		Identity: t.config.Publish,
	}
	if connectReq.Identity == "" {
		err = fmt.Errorf("empty publish endpoint")
		log.Error(err)
		return false, err
	}
	if t.config.Encrypt {
		connectReq.Version = proto.ConnectAES256GCM
	} else {
		connectReq.Version = proto.ConnectNoCrypt
	}
	buf = connectReq.Encode(buf[:0])
	if _, err = link.muxer.Mux(buf); err != nil {
		log.Info("mux error: ", err)
		return false, err
	}
	// switch protocol
	switch connectReq.Version {
	case proto.ConnectNoCrypt:
		link.InitializeNoCryption()

	case proto.ConnectAES256GCM:
		// let it is.
	default:
		// should not hit this.
		err = fmt.Errorf("invalid connecting protocol version %v", connectReq.Version)
		log.Error(err)
		return false, err
	}

	return true, nil
}

func (t *TCP) Port() uint16 {
	return uint16(t.bind.Port)
}

func (t *TCP) Type() pb.PeerBackend_BackendType {
	return pb.PeerBackend_TCP
}

func (t *TCP) Publish() (id string) {
	return t.config.Publish
}

func (t *TCP) Shutdown() {
	t.Arbiter.Shutdown()
	t.Arbiter.Join()
}

func (t *TCP) resolve(endpoint string) (addr *net.TCPAddr, err error) {
	v, ok := t.resolveCache.Load(endpoint)
	if !ok || v == nil {
		if addr, err = net.ResolveTCPAddr("tcp", endpoint); err != nil {
			t.log.Errorf("destination \"%v\" not resolved: %v", v, err)
			return nil, err
		}
		t.resolveCache.Store(endpoint, addr)
	} else {
		addr = v.(*net.TCPAddr)
	}
	return
}

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

func (t *TCP) Watch(proc func(Backend, []byte, string)) error {
	if proc != nil {
		t.watch.Store(&proc, proc)
	}
	return nil
}

func (t *TCP) IP() net.IP {
	return make(net.IP, 0)
}