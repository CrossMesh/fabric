package backend

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"time"

	"git.uestc.cn/sunmxt/utt/proto"
	logging "github.com/sirupsen/logrus"
)

func (t *TCP) handshakeConnect(log *logging.Entry, connID uint32, adaptedConn net.Conn) (accepted bool, err error) {
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
	hello := proto.Hello{
		Lead: t.getStartCode(log),
	}

	var read int
	// hello message has fixed length.
	for t.Arbiter.ShouldRun() && read < hello.Len() {
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
	if !t.Arbiter.ShouldRun() {
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
	if !t.Arbiter.ShouldRun() || err != nil {
		return false, err
	}

	return t.acceptTCPLink(log, link, connectReq)
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
	hello := proto.Hello{
		Lead: t.getStartCode(log),
	}
	hello.Refresh()
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
