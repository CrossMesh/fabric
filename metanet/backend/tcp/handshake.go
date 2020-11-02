package tcp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/crossmesh/fabric/metanet/backend"
	"github.com/crossmesh/fabric/proto"
	logging "github.com/sirupsen/logrus"
)

func (e *tcpEndpoint) handshakeConnect(log *logging.Entry, connID uint32, adaptedConn net.Conn) (accepted bool, err error) {
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
		Lead: e.getStartCode(log),
	}

	var read int
	// hello message has fixed length.
	for e.Arbiter.ShouldRun() && read < hello.Len() {
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
	if !e.Arbiter.ShouldRun() {
		return false, nil
	}
	if err := hello.Decode(buf[:read]); err != nil {
		if err == proto.ErrInvalidPacket {
			return false, nil
		}
		return false, err
	}
	buf = buf[:0]
	psk := e.parameters.Encryption.PSK
	if psk != "" {
		accepted = hello.Verify([]byte(psk))
	} else {
		accepted = hello.Verify(nil)
	}
	if !accepted {
		log.Info("deined for authentication failure.")
		return accepted, nil
	}
	log.Debug("authentication success.")

	// init cipher.
	link := newTCPLink(e)
	link.conn = conn
	buf = buf[:0]
	buf = append(buf, hello.Lead...)
	if psk != "" {
		buf = append(buf, []byte(psk)...)
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
		Identity: e.endpoint,
	}
	if welcome.Identity == "" {
		err = fmt.Errorf("empty publish endpoint")
		log.Error(err)
		return false, err
	}
	welcome.EncodeMessage("ok")
	buf = welcome.Encode(buf[:0])
	if _, err = link.muxer.Mux(buf); err != nil {
		log.Error("send welcome failure: ", err)
		return false, err
	}

	// wait for connect
	log.Debug("negotiate peering information.")
	var connectReq *proto.Connect
	if rerr := link.read(func(frame []byte) bool {
		connectReq = &proto.Connect{}
		if err = connectReq.Decode(frame); err != nil {
			log.Error("corrupted connect handshake packet.")
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
	if !e.Arbiter.ShouldRun() || err != nil || connectReq == nil {
		return false, err
	}

	return e.acceptTCPLink(log, link, connectReq)
}

func (e *tcpEndpoint) connectHandshake(ctx context.Context, log *logging.Entry, link *TCPLink) (accepted bool, err error) {
	buf := make([]byte, defaultBufferSize)

	// deadline.
	deadline, hasDeadline := ctx.Deadline()
	if hasDeadline {
		if err = link.conn.SetDeadline(deadline); err != nil {
			log.Error("conn.SetDeadline() failure: ", err)
			return false, err
		}
	}

	log.Debug("handshaking...")
	// hello
	hello := proto.Hello{
		Lead: e.getStartCode(log),
	}
	hello.Refresh()
	psk := e.parameters.Encryption.PSK
	if psk != "" {
		hello.Sign([]byte(psk))
	} else {
		hello.Sign(nil)
	}
	buf = hello.Encode(buf[:0])
	if _, err = link.conn.Write(buf); err != nil {
		if err == io.EOF {
			log.Error("connection closed by peer.")
		} else {
			log.Error("send error: ", err)
		}
		return false, err
	}

	// can init cipher now.
	log.Debug("initialize cipher.")
	buf = buf[:0]
	buf = append(buf, hello.Lead...)
	if psk != "" {
		buf = append(buf, []byte(psk)...)
	}
	buf = append(buf, hello.HMAC[:]...)
	key := sha256.Sum256(buf)
	if err = link.InitializeAESGCM(key[:], hello.IV[:]); err != nil {
		log.Error("cipher initializion failure: ", err)
		return false, err
	}

	// wait for welcome.
	log.Debug("wait for authentication.")
	var welcome *proto.Welcome
	if rerr := link.read(func(frame []byte) bool {
		welcome = &proto.Welcome{}
		if err = welcome.Decode(frame); err != nil {
			log.Error("corrupted welcome handshake packet.")
		}
		return false
	}); rerr != nil {
		if nerr, ok := rerr.(net.Error); ok && nerr.Timeout() {
			log.Error("canceled for deadline exceeded.")
			return false, backend.ErrOperationCanceled
		}
		if rerr == io.EOF {
			log.Error("connection closed by foreign peer.")
			return false, backend.ErrConnectionClosed
		}
		log.Error("link read failure: ", rerr)
		return false, rerr
	}
	if err != nil {
		return false, err
	}
	if done := ctx.Done(); done != nil {
		select {
		case <-done:
			return false, backend.ErrOperationCanceled
		default:
		}
	}
	if !welcome.Welcome { // denied.
		return false, nil
	}

	log.Debug("good authentication. connecting...")
	// send connect request.
	connectReq := proto.Connect{
		Identity: e.endpoint,
	}
	if connectReq.Identity == "" {
		err = fmt.Errorf("empty publish endpoint")
		log.Error(err)
		return false, err
	}
	if e.parameters.Encryption.Enable {
		connectReq.Version = proto.ConnectAES256GCM
	} else {
		connectReq.Version = proto.ConnectNoCrypt
	}
	buf = connectReq.Encode(buf[:0])
	if _, err = link.muxer.Mux(buf); err != nil {
		log.Error("mux error: ", err)
		return false, err
	}
	// switch protocol
	switch connectReq.Version {
	case proto.ConnectNoCrypt:
		link.InitializeNoCryption()

	case proto.ConnectAES256GCM:
		log.Debug("enable encryption.")
		// let it is.
	default:
		// should not hit this.
		err = fmt.Errorf("invalid connecting protocol version %v", connectReq.Version)
		log.Error(err)
		return false, err
	}

	return true, nil
}
