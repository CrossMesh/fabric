package relay

import (
	"fmt"
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
	conn      *net.Conn
	sigChange chan *net.UDPAddr
}

type RelayServer struct {
	bind *net.TCPAddr
	acl  map[string]*config.ACL

	aclLock     sync.RWMutex
	resolvedACL map[string]*resolvedACL

	listener *net.TCPListener

	Log *log.Entry
}

func NewServer(bind *net.TCPAddr, acl map[string]*config.ACL) *RelayServer {
	return &RelayServer{
		bind: bind,
		acl:  acl,
	}
}

func (s *RelayServer) goACLResolve(arbiter *arbiter.Arbiter, addr string, acl *config.ACL) {
	arbiter.Go(func() {
		log := s.Log
		var (
			newAddr  *net.UDPAddr
			err      error
			register bool
		)
		resolved := resolvedACL{
			sigChange: make(chan *net.UDPAddr),
		}
		err = nil

		for arbiter.ShouldRun() {
			newAddr = nil

			if newAddr, err = net.ResolveUDPAddr("udp", addr); err != nil {
				if log != nil {
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
				} else {
					select {
					case resolved.sigChange <- newAddr:
					case <-time.After(time.Second * 5):
					}
				}
				resolved.addr = newAddr
				resolved.origin = acl
			}
		}
	})
}

func (s *RelayServer) doRelay(arbiter *arbiter.Arbiter, connID int, conn net.Conn) error {
	defer conn.Close()
	return nil
}

func (s *RelayServer) logError(prefix string, err error) {
	log := s.Log
	if log == nil {
		return
	}
	if err != nil {
		log.Error(prefix, err)
	} else {
		log.Error(prefix)
	}
}

func (s *RelayServer) logInfo(prefix string) {
	log := s.Log
	if log == nil {
		return
	}
	log.Error(prefix)
}

func (s *RelayServer) handshakeConnect(arbiter *arbiter.Arbiter, connID int, conn net.Conn) (accepted bool, err error) {
	muxer, demuxer := mux.NewStreamMuxer(conn), mux.NewStreamDemuxer()
	buf := make([]byte, defaultBufferSize)

	// handshake challenge.
	hello := proto.Hello{}
	hello.Refresh() // New challenge.
	rawChallenge := hello.Encode(buf[:0])
	if _, err := muxer.Mux(rawChallenge); err != nil {
		s.logError("write hello error: ", err)
		return false, err
	}

	var connectReq *proto.Connect
	// wait for connect packet.
	for arbiter.ShouldRun() && connectReq == nil {
		if err := conn.SetReadDeadline(time.Now().Add(time.Second * 10)); err != nil {
			s.logError("set deadline error: ", err)
			return false, err
		}
		read, err := conn.Read(buf)
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				s.logInfo("deny due to inactivity.")
				return false, nil
			}
		}
		demuxer.Demux(buf[:read], func(pkt []byte) {
			if connectReq != nil {
				return
			}
			connectReq := &proto.Connect{}
			if err = connectReq.Decode(pkt); err != nil {
				s.logInfo("corrupted connect handshake packet.")
			}
			accepted = true
		})
	}

	// verify
	if !accepted || connectReq == nil {
		return false, nil
	}
	s.aclLock.RLock()
	acl, hasACL := s.resolvedACL[connectReq.ACLKey]
	s.aclLock.RUnlock()
	if !hasACL || acl == nil {
		s.logInfo("deny for no resolved acl.")
		return false, nil
	}
	if acl.origin.PSK != "" {
		accepted = connectReq.Verify(hello.Challenge[:], []byte(acl.origin.PSK))
	}

	// reply
	connRes := proto.ConnectResult{
		Welcome: accepted,
	}
	replyBuf := buf[0:0]
	if !accepted {
		connRes.EncodeMessage("challenge failure")
		s.logInfo(fmt.Sprintf("deny connection %v for challenge failure.", connID))
	} else {
		connRes.EncodeMessage("ok")
	}
	replyBuf = connRes.Encode(replyBuf)
	if _, err = muxer.Mux(replyBuf); err != nil {
		accepted = false
		s.logError("write welcome error: ", err)
	}

	return accepted, err
}

func (s *RelayServer) goServeConnection(arbiter *arbiter.Arbiter, connID int, conn net.Conn) {
	log := s.Log

	arbiter.Go(func() {
		defer conn.Close()

		accepted, err := s.handshakeConnect(arbiter, connID, conn)
		if err != nil && log != nil {
			log.Errorf("handshake error for connection %v: %v", connID, err)
			return
		}
		if !accepted && log != nil {
			log.Errorf("connection %v deined.", connID)
			return
		}
		err = s.doRelay(arbiter, connID, conn)
		if err != nil && log != nil {
			log.Errorf("relay failure: %v", err)
		}
	})
}

func (s *RelayServer) acceptConnection(arbiter *arbiter.Arbiter) (err error) {
	log := s.Log

	connID := 0
	for arbiter.ShouldRun() {
		if err != nil {
			time.Sleep(time.Second * 5)
		}
		err = nil

		if err = s.listener.SetDeadline(time.Now().Add(time.Second * 3)); err != nil {
			if log != nil {
				log.Error("set deadline error: ", err)
			}
			continue
		}

		conn, err := s.listener.Accept()
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				continue
			}
			if log != nil {
				log.Error("accept error: ", err)
			}
		}

		connID++
		s.goServeConnection(arbiter, connID, conn)
	}

	return nil
}

func (s *RelayServer) Do(globalArbiter *arbiter.Arbiter) (err error) {
	// watch all acls.
	aclArbiter := arbiter.New(s.Log)

	for key, acl := range s.acl {
		if key == "" {
			if s.Log != nil {
				s.Log.Warn("empty acl key. skip.")
				continue
			}
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
			log.Errorf("cannot listen to \"%v\": %v", s.bind.String(), err)
			continue
		}
		err = s.acceptConnection(globalArbiter)
	}

	aclArbiter.Shutdown()
	aclArbiter.Wait()

	return nil
}
