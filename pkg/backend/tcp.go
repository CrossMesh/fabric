package backend

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"git.uestc.cn/sunmxt/utt/pkg/arbiter"
	"git.uestc.cn/sunmxt/utt/pkg/config"
	"git.uestc.cn/sunmxt/utt/pkg/mux"
	"git.uestc.cn/sunmxt/utt/pkg/proto"
	"git.uestc.cn/sunmxt/utt/pkg/proto/pb"
	logging "github.com/sirupsen/logrus"
)

type TCPLink struct {
	muxer   mux.Muxer
	demuxer mux.Demuxer
	conn    net.Conn
	crypt   cipher.Block
	remote  *net.TCPAddr
}

type TCP struct {
	bind     *net.TCPAddr
	listener *net.TCPListener

	config *config.TCPBackend
	psk    *string

	log *logging.Entry

	link  sync.Map
	watch sync.Map
}

func NewTCP(arbiter *arbiter.Arbiter, log *logging.Entry, cfg *config.TCPBackend, psk *string) (t *TCP) {
	if log == nil {
		log = logging.WithField("module", "backend_tcp")
	}
	t = &TCP{
		psk:    psk,
		config: cfg,
		log:    log,
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

	connID := 0
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

		connID++
		t.goServeConnection(arbiter, connID, conn)
		conn = nil
	}

	return nil
}

func (t *TCP) goServeConnection(arbiter *arbiter.Arbiter, connID int, conn net.Conn) {
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

func (t *TCP) handshakeConnect(arbiter *arbiter.Arbiter, log *logging.Entry, connID int, conn net.Conn) (accepted bool, err error) {
	buf := make([]byte, defaultBufferSize)

	// wait for hello.
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
	default:
		log.Warn("unknwon start code setting. skip.")
	}
	if t.psk != nil {
		hello.Sign([]byte(*t.psk))
	} else {
		hello.Sign(nil)
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
		return accepted, nil
	}

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
	if blk, err := aes.NewCipher(key[:]); err != nil {
		log.Info("aes cipher creation failure: ", err)
		return false, err
	} else {
		link.crypt = blk
	}
	if link.muxer, err = mux.NewGCMStreamMuxer(conn, link.crypt, hello.IV[:]); err != nil {
		log.Info("unable to create gcm stream muxer: ", err)
		return false, err
	}
	if link.demuxer, err = mux.NewGCMStreamDemuxer(link.crypt, hello.IV[:]); err != nil {
		log.Info("unable to create gcm stream demuxer: ", err)
		return false, err
	}
	link.conn = conn

	// welcome
	welcome := proto.Welcome{
		Welcome: true,
	}
	welcome.EncodeMessage("ok")
	buf = welcome.Encode(buf[:0])
	if _, err = link.muxer.Mux(buf); err != nil {
		log.Info("send welcome failure: ", err)
		return false, err
	}

	// wait for connect
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
		link.demuxer.Demux(buf[:read], func(pkt []byte) {
			if connectReq != nil {
				return
			}
			connectReq = &proto.Connect{}
			if err = connectReq.Decode(pkt); err != nil {
				log.Info("corrupted connect handshake packer.")
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
		link.muxer, link.demuxer = mux.NewStreamMuxer(link.conn), mux.NewStreamDemuxer()

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
	if _, exists := t.link.LoadOrStore(key, link); exists {
		log.Warnf("link to foreign peer \"%v\" exists. closing...")
		return false, nil
	}
	link.remote = addr
	log.Infof("link to foreign peer \"%v\" established.", key)

	t.goTCPLinkDaemon(arbiter, log, key, link)

	return true, nil
}

func (t *TCP) goTCPLinkDaemon(arbiter *arbiter.Arbiter, log *logging.Entry, key string, link *TCPLink) {
	arbiter.Go(func() {
		defer func() {
			link.conn.Close()
			t.link.Delete(key)
		}()

		var err error

		for arbiter.ShouldRun() {
			if err != nil {
				if err == io.EOF {
					log.Info("connection closed by peer.")
					break
				}
			}
		}
	})
}

func (t *TCP) GetTCPLink(ctx context.Context, addr *net.TCPAddr) (*TCPLink, error) {
	key := addr.IP.To4().String() + strconv.FormatInt(int64(addr.Port), 10)
	return nil, nil
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
