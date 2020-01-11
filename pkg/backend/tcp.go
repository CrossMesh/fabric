package backend

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"git.uestc.cn/sunmxt/utt/pkg/arbiter"
	"git.uestc.cn/sunmxt/utt/pkg/config"
	"git.uestc.cn/sunmxt/utt/pkg/mux"
	"git.uestc.cn/sunmxt/utt/pkg/proto"
	"git.uestc.cn/sunmxt/utt/pkg/proto/pb"
	logging "github.com/sirupsen/logrus"
)

func validTCPPublishEndpoint(arr *net.TCPAddr) bool {
	return true
}

type TCPLink struct {
	muxer   mux.Muxer
	demuxer mux.Demuxer
	conn    net.Conn
	crypt   cipher.Block
	remote  *net.TCPAddr
}

func (l *TCPLink) InitializeAESGCM(key []byte, nonce []byte) (err error) {
	if blk, err := aes.NewCipher(key[:]); err != nil {
		return err
	} else {
		l.crypt = blk
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
		l.conn = nil
	}
	return
}

type TCP struct {
	bind     *net.TCPAddr
	listener *net.TCPListener

	config *config.TCPBackend
	psk    *string

	log *logging.Entry

	link   sync.Map
	watch  sync.Map
	connID uint32

	Arbiter *arbiter.Arbiter
}

var (
	ErrNonTCPConnection = errors.New("got non-tcp connection")
)

func NewTCP(arbiter *arbiter.Arbiter, log *logging.Entry, cfg *config.TCPBackend, psk *string) (t *TCP) {
	if log == nil {
		log = logging.WithField("module", "backend_tcp")
	}
	t = &TCP{
		psk:     psk,
		config:  cfg,
		log:     log,
		Arbiter: arbiter,
	}
	arbiter.Go(func() {
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
	return t
}

func (t *TCP) serve(arbiter *arbiter.Arbiter) (err error) {
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

func (t *TCP) acceptConnection(arbiter *arbiter.Arbiter) (err error) {
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

func (t *TCP) goServeConnection(arbiter *arbiter.Arbiter, connID uint32, conn net.Conn) {
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

func (t *TCP) handshakeConnect(arbiter *arbiter.Arbiter, log *logging.Entry, connID uint32, conn net.Conn) (accepted bool, err error) {
	buf := make([]byte, defaultBufferSize)

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
		log.Warn("unknwon start code setting. skip.")
	}

	var read int
	for arbiter.ShouldRun() && read < hello.Len() {
		if err := conn.SetDeadline(time.Now().Add(time.Second * 15)); err != nil {
			log.Error("conn.SetDeadline() failure: ", err)
			return false, nil
		}
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
	link := &TCPLink{
		conn: conn,
	}
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
	buf = buf[:cap(buf)]
	for arbiter.ShouldRun() && connectReq == nil && err != nil {
		if err := conn.SetReadDeadline(time.Now().Add(time.Second * 15)); err != nil {
			log.Error("set deadline error:", err)
			return false, err
		}
		if read, err = conn.Read(buf); err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				log.Info("deny due to inactivity.")
				return false, nil
			}
			if err == io.EOF {
				log.Info("connection closed by foreign peer.")
				return false, nil
			}
			return false, err
		}
		_, err = link.demuxer.Demux(buf[:read], func(pkt []byte) {
			if connectReq != nil {
				return
			}
			connectReq = &proto.Connect{}
			if err = connectReq.Decode(pkt); err != nil {
				log.Info("corrupted connect handshake packet.")
			}
		})
	}
	if !arbiter.ShouldRun() || err != nil {
		return false, err
	}

	return t.acceptTCPLink(arbiter, log, link, connectReq)
}

func (t *TCP) acceptTCPLink(arbiter *arbiter.Arbiter, log *logging.Entry, link *TCPLink, connectArg *proto.Connect) (bool, error) {
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
		log.Warnf("link to foreign peer \"%v\" exists. closing...")
		return false, nil
	}
	link.remote = addr
	log.Infof("link to foreign peer \"%v\" established.", key)

	t.goTCPLinkDaemon(log, key, link)

	return true, nil
}

func (t *TCP) goTCPLinkDaemon(log *logging.Entry, key string, link *TCPLink) {
	t.Arbiter.Go(func() {
		defer func() {
			link.conn.Close()
			t.link.Delete(key)
		}()

		var err error

		for t.Arbiter.ShouldRun() {
			if err != nil {
				if err == io.EOF {
					log.Info("connection closed by peer.")
					break
				}
			}
		}
	})
}

func (t *TCP) connect(ctx context.Context, addr *net.TCPAddr) (link *TCPLink, err error) {
	key := addr.IP.To4().String() + strconv.FormatInt(int64(addr.Port), 10)
	if v, exists := t.link.Load(key); exists {
		return v.(*TCPLink), nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// dial
	t.log.Info("connecting to ", addr.String())
	link = &TCPLink{}
	dialer := net.Dialer{}
	if ctx != nil {
		if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
			dialer.Deadline = deadline
		}
	}
	if conn, err := dialer.Dial("tcp", addr.String()); err != nil {
		t.log.Error(err)
		return nil, err
	} else if tcpConn, isTCP := conn.(*net.TCPConn); !isTCP {
		t.log.Error("got non-tcp connection")
		return nil, ErrNonTCPConnection
	} else {
		link.conn = tcpConn
	}
	defer func() {
		if err != nil {
			link.Close()
		}
	}()
	if done := ctx.Done(); done != nil {
		select {
		case <-done:
			return nil, ErrOperationCanceled
		}
	}

	connID := atomic.AddUint32(&t.connID, 1)
	log := t.log.WithField("conn_id", connID)

	// handshake
	var identity string
	if identity, err = t.connectHandshake(ctx, log, link); err != nil {
		link.Close()
		return nil, err
	}

	// resolve identity.
	if peerAddr, err := net.ResolveTCPAddr("tcp", identity); err != nil {
		log.Errorf("cannot resolve \"%v\": %v", identity, err)
		return nil, err
	} else if !validTCPPublishEndpoint(peerAddr) {
		err = fmt.Errorf("invalid publish endpoint")
		log.Error(err)
		return nil, err
	} else {
		identity = peerAddr.IP.To4().String() + strconv.FormatInt(int64(peerAddr.Port), 10)
	}

	if v, exists := t.link.LoadOrStore(identity, link); exists {
		link.Close()
		log.Warnf("link to foreign peer \"%v\" exists. closing...")
		link = v.(*TCPLink)
	} else {
		// start receiver for new link.
		t.goTCPLinkDaemon(log, identity, link)
	}

	return link, nil
}

func (t *TCP) connectHandshake(ctx context.Context, log *logging.Entry, link *TCPLink) (identity string, err error) {
	buf := make([]byte, defaultBufferSize)

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
		return "", err
	}

	// can init cipher now.
	buf = buf[:0]
	buf = append(buf, hello.Lead...)
	if t.psk != nil {
		buf = append(buf, []byte(*t.psk)...)
	}
	buf = append(buf, hello.HMAC[:]...)
	key := sha256.Sum256(buf)
	if err = link.InitializeAESGCM(key[:], hello.IV[:]); err != nil {
		log.Error("cipher initializion failure: ", err)
		return "", err
	}

	// wait for welcome.
	var welcome *proto.Welcome
	for welcome == nil && err == nil {
		if done := ctx.Done(); done != nil {
			select {
			case <-done:
				return "", ErrOperationCanceled
			}
		}
		deadline, hasDeadline := ctx.Deadline()
		if hasDeadline {
			if err = link.conn.SetDeadline(deadline); err != nil {
				log.Error("conn.SetDeadline() failure: ", err)
				return "", err
			}
		}

		var read int
		if read, err = link.conn.Read(buf); err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				log.Info("canceled for deadline exceeded.")
				return "", ErrOperationCanceled
			}
			if err == io.EOF {
				log.Info("connection closed by foreign peer.")
				return "", ErrConnectionClosed
			}
		}
		_, err = link.demuxer.Demux(buf[:read], func(frame []byte) {
			if welcome != nil {
				return
			}
			welcome = &proto.Welcome{}
			if err = welcome.Decode(frame); err != nil {
				log.Info("corrupted welcome handshake packet.")
			}
		})
	}
	if err != nil {
		return "", err
	}

	// send connect request.
	connectReq := proto.Connect{
		Identity: t.config.Publish,
	}
	if connectReq.Identity == "" {
		err = fmt.Errorf("empty publish endpoint")
		log.Error(err)
		return "", err
	}
	if t.config.Encrypt {
		connectReq.Version = proto.ConnectAES256GCM
	} else {
		connectReq.Version = proto.ConnectNoCrypt
	}
	buf = connectReq.Encode(buf[:0])
	if _, err = link.muxer.Mux(buf); err != nil {
		log.Info("mux error: ", err)
		return "", err
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
		return "", err
	}

	return connectReq.Identity, nil
}

func (t *TCP) GetTCPLink(ctx context.Context, addr *net.TCPAddr) (*TCPLink, error) {
	return t.connect(ctx, addr)
}

func (t *TCP) Port() uint16 {
	return uint16(t.bind.Port)
}

func (t *TCP) Type() pb.PeerBackend_BackendType {
	return pb.PeerBackend_TCP
}

func (t *TCP) Send(ctx context.Context, frame []byte, dst interface{}) (err error) {
	var (
		addr *net.TCPAddr
		link *TCPLink
	)

	switch v := dst.(type) {
	case string:
		if addr, err = net.ResolveTCPAddr("tcp", v); err != nil {
			t.log.Errorf("destination \"%v\" not resolved: %v", v, err)
			return err
		}
	case *net.TCPAddr:
		addr = v
	default:
		return ErrUnknownDestinationType
	}
	if link, err = t.GetTCPLink(ctx, addr); err != nil {
		return err
	}
	if _, err = link.muxer.Mux(frame); err != nil {
		t.log.Error("mux error: ", err)
		return err
	}
	return nil
}

func (t *TCP) Watch(proc func([]byte, interface{})) error {

	return nil
}

func (t *TCP) IP() net.IP {
	return make(net.IP, 0)
}
