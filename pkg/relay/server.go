package relay

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"git.uestc.cn/sunmxt/utt/pkg/arbiter"
	"git.uestc.cn/sunmxt/utt/pkg/config"
	"git.uestc.cn/sunmxt/utt/pkg/mux"
	"git.uestc.cn/sunmxt/utt/pkg/proto"
	log "github.com/sirupsen/logrus"
)

type resolvedACL struct {
	origin    *config.ACL
	addr      *net.UDPAddr
	conn      net.Conn
	sigChange chan *net.UDPAddr
}

type RelayServer struct {
	bind *net.TCPAddr
	acl  map[string]*config.ACL

	aclLock     sync.RWMutex
	resolvedACL map[string]*resolvedACL

	listener *net.TCPListener

	log *log.Entry
}

func NewServer(bind *net.TCPAddr, acl map[string]*config.ACL, log *log.Entry) *RelayServer {
	return &RelayServer{
		bind:        bind,
		acl:         acl,
		resolvedACL: make(map[string]*resolvedACL),
		log:         log,
	}
}

func (s *RelayServer) goACLResolve(arbiter *arbiter.Arbiter, addr string, acl *config.ACL) {
	arbiter.Go(func() {
		var (
			newAddr  *net.UDPAddr
			err      error
			register bool
		)
		resolved := resolvedACL{
			sigChange: make(chan *net.UDPAddr),
		}
		err = nil
		nextResolveTime := time.Now()
		for arbiter.ShouldRun() {
			newAddr = nil
			if time.Now().Before(nextResolveTime) {
				time.Sleep(time.Second)
				continue
			}

			if newAddr, err = net.ResolveUDPAddr("udp", addr); err != nil {
				if s.log != nil {
					log.Errorf("acl \"%v\" resolve failure: %v", addr, err)
				}
				time.Sleep(time.Second * 5)
				continue
			}

			if !UDPAddrEqual(newAddr, resolved.addr) {
				if !register {
					s.aclLock.Lock()
					s.resolvedACL[addr] = &resolved
					s.aclLock.Unlock()
					s.log.Infof("resolved endpoint: %v --> %v:%v.", addr, newAddr.IP, newAddr.Port)
					register = true
				} else {
					s.log.Infof("resolved endpoint change: %v --> %v:%v.", addr, newAddr.IP, newAddr.Port)
					select {
					case resolved.sigChange <- newAddr:
					case <-time.After(time.Second * 5):
					}
				}
				resolved.addr = newAddr
				resolved.origin = acl
			}
			nextResolveTime = time.Now().Add(time.Second * 60)
		}
	})
}

func (s *RelayServer) forwardRelay(arbiter *arbiter.Arbiter, connID int, forward string, conn net.Conn, logBase *log.Entry) {
	log := logBase.WithFields(log.Fields{
		"traffic": "forward",
		"to":      forward,
	})
	if forward == "" {
		log.Warn("empty forward address. exiting...")
		return
	}

	defer arbiter.Shutdown()
	out, err := net.ListenPacket("udp", ":0")
	if err != nil {
		log.Error("net.ListenPacket error: ", err)
		return
	}
	var addr *net.UDPAddr
	// resolve address.
	for arbiter.ShouldRun() {
		if err != nil {
			time.Sleep(time.Second * 5)
		}
		err = nil

		if addr, err = net.ResolveUDPAddr("udp", forward); err != nil {
			log.Errorf("resolve \"%v\" failure: %v", forward, err)
			continue
		}
		break
	}
	relayDemux := NewDemuxRelay(conn, out, addr, log)
	if err = relayDemux.Do(arbiter); err != nil {
		log.Errorf("relay error: ", err)
	}
	log.Info("forward relaying shutting down...")
}

func (s *RelayServer) reverseRelay(arbiter *arbiter.Arbiter, connID int, reverse string, conn net.Conn, logBase *log.Entry) {
	log := logBase.WithFields(log.Fields{
		"traffic": "reverse",
		"from":    reverse,
	})
	if reverse == "" {
		log.Warn("empty reverse address. exiting...")
		return
	}

	defer arbiter.Shutdown()
	var (
		addr *net.UDPAddr
		err  error
	)
	// resolve address.
	for arbiter.ShouldRun() {
		if err != nil {
			time.Sleep(time.Second * 5)
		}
		err = nil
		if addr, err = net.ResolveUDPAddr("udp", reverse); err != nil {
			log.Errorf("resolve \"%v\" failure: %v", reverse, err)
			continue
		}
		break
	}
	relayMux := NewMuxRelay(conn, addr, log)
	if err = relayMux.Do(arbiter); err != nil {
		log.Error("relay error: ", err.Error())
	}

	log.Info("reverse relay shutting down...")
}

func (s *RelayServer) doRelay(globalArbiter *arbiter.Arbiter, connID int, conn net.Conn, aclKey string, resolved *resolvedACL) error {
	defer conn.Close()

	subLog := s.log.WithField("conn_id", connID)
	relayArbiter := arbiter.New(subLog)
	acl := resolved.origin
	resolved.conn = conn

	// Reverse traffic.
	if acl != nil && acl.Reverse != nil {
		relayArbiter.Go(func() {
			s.reverseRelay(relayArbiter, connID, fmt.Sprintf("%v:%v", acl.Reverse.Address, acl.Reverse.Port), conn, subLog)
		})
	}
	// Forward traffic.
	relayArbiter.Go(func() {
		s.forwardRelay(relayArbiter, connID, aclKey, conn, subLog)
	})
	relayArbiter.Wait()
	resolved.conn = nil

	return nil
}

func (s *RelayServer) handshakeConnect(arbiter *arbiter.Arbiter, connID int, conn net.Conn) (accepted bool, aclKey string, racl *resolvedACL, err error) {
	log := s.log.WithField("conn_id", connID)

	muxer, demuxer := mux.NewStreamMuxer(conn), mux.NewStreamDemuxer()
	buf := make([]byte, defaultBufferSize)

	// handshake challenge.
	hello := proto.Hello{}
	hello.Refresh() // New challenge.
	rawChallenge := hello.Encode(buf[:0])
	if _, err := muxer.Mux(rawChallenge); err != nil {
		log.Error("write hello error: ", err)
		return false, "", nil, err
	}
	log.Info("handshake hello sent.")

	var connectReq *proto.Connect
	// wait for connect packet.
	for arbiter.ShouldRun() && connectReq == nil {
		if err := conn.SetReadDeadline(time.Now().Add(time.Second * 10)); err != nil {
			log.Error("set deadline error: ", err)
			return false, "", nil, err
		}
		read, err := conn.Read(buf)
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				log.Info("deny due to inactivity.")
				return false, "", nil, nil
			}
			if err == io.EOF {
				log.Info("connection closed by client.")
				return false, "", nil, nil
			}
			return false, "", nil, err
		}
		demuxer.Demux(buf[:read], func(pkt []byte) {
			if connectReq != nil {
				return
			}
			connectReq = &proto.Connect{}
			if err = connectReq.Decode(pkt); err != nil {
				log.Info("corrupted connect handshake packet.")
			}
			accepted = true
		})
	}
	log.Info("connect request received.")

	// verify
	if !accepted || connectReq == nil {
		return false, "", nil, nil
	}
	s.aclLock.RLock()
	acl, hasACL := s.resolvedACL[connectReq.ACLKey]
	s.aclLock.RUnlock()
	if !hasACL || acl == nil {
		log.Info("deny for no resolved acl.")
		accepted = false
	} else if acl.origin.PSK != "" {
		accepted = connectReq.Verify(hello.Challenge[:], []byte(acl.origin.PSK))
	}

	// reply
	connRes := proto.ConnectResult{
		Welcome: accepted,
	}
	// has connected.
	replyBuf := buf[0:0]
	if !accepted {
		connRes.EncodeMessage("challenge failure")
		log.Info(fmt.Sprintf("deny connection %v for challenge failure.", connID))
	} else if acl.conn != nil {
		connRes.EncodeMessage("already connected")
		connRes.Welcome = false
	} else {
		connRes.EncodeMessage("ok")
	}

	replyBuf = connRes.Encode(replyBuf)
	if _, err = muxer.Mux(replyBuf); err != nil {
		accepted = false
		log.Error("write welcome error: ", err)
	}

	return accepted, connectReq.ACLKey, acl, err
}

func (s *RelayServer) goServeConnection(arbiter *arbiter.Arbiter, connID int, conn net.Conn) {
	log := s.log.WithField("conn_id", connID)
	log.Infof("incoming connection %v from %v", connID, conn.RemoteAddr())

	arbiter.Go(func() {
		defer conn.Close()

		accepted, aclKey, acl, err := s.handshakeConnect(arbiter, connID, conn)
		if err != nil {
			log.Errorf("handshake error for connection %v: %v", connID, err)
			return
		}
		if !accepted {
			log.Info("connection deined.")
			return
		}
		err = s.doRelay(arbiter, connID, conn, aclKey, acl)
		if err != nil {
			log.Errorf("relay failure: %v", err)
		}
	})
}

func (s *RelayServer) acceptConnection(arbiter *arbiter.Arbiter) (err error) {
	connID := 0
	s.log.Infof("start accepting connection.")

	for arbiter.ShouldRun() {
		if err != nil {
			time.Sleep(time.Second * 5)
		}
		err = nil

		if err = s.listener.SetDeadline(time.Now().Add(time.Second * 3)); err != nil {
			s.log.Error("set deadline error: ", err)
			continue
		}

		conn, err := s.listener.Accept()
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				err = nil
				continue
			}
			s.log.Error("accept error: ", err)
		}

		connID++
		s.goServeConnection(arbiter, connID, conn)
		conn = nil
	}

	return nil
}

func (s *RelayServer) Do(globalArbiter *arbiter.Arbiter) (err error) {
	// watch all acls.
	aclArbiter := arbiter.New(s.log)

	for key, acl := range s.acl {
		if key == "" {
			s.log.Warn("empty acl key. skip.")
		}
		s.goACLResolve(aclArbiter, key, acl)
	}

	for globalArbiter.ShouldRun() {
		if err != nil {
			time.Sleep(time.Second * 5)
		}
		err = nil

		s.listener, err = net.ListenTCP("tcp", s.bind)
		if err != nil {
			s.log.Errorf("cannot listen to \"%v\": %v", s.bind.String(), err)
			continue
		}
		s.log.Infof("listening to %v", s.bind.String())

		err = s.acceptConnection(globalArbiter)
	}

	aclArbiter.Shutdown()
	aclArbiter.Wait()

	return nil
}
