package relay

import (
	"errors"
	"net"

	"git.uestc.cn/sunmxt/utt/pkg/arbiter"
	"git.uestc.cn/sunmxt/utt/pkg/config"
	log "github.com/sirupsen/logrus"
)

type RelayServer struct {
	bind *net.TCPAddr
	acl  map[string]*config.ACL
	Log  *log.Entry
}

func NewServer(bind *net.TCPAddr, acl map[string]*config.ACL) *RelayServer {
	return &RelayServer{
		bind: bind,
		acl:  acl,
	}
}

func (s *RelayServer) Do(arbiter *arbiter.Arbiter) error {
	return errors.New("not implemented.")
}

//func (s *RelayServer) Go(arbiter *arbiter.Arbiter) (err error) {
//	// tunnel.
//	s.tunnelAddr, err = net.ResolveTCPAddr("tcp", s.config.TCPNetwork)
//	if err != nil {
//		err = fmt.Errorf("Cannod resolve address: %v", s.config.TCPNetwork)
//		log.Error(err)
//		return err
//	}
//	if s.tunnelListener, err = net.ListenTCP("tcp", s.tunnelAddr); err != nil {
//		log.Errorf("Failed to listen for TCP connection: %v", err)
//		return err
//	}
//	defer func() {
//		if err != nil {
//			s.tunnelListener.Close()
//			s.tunnelAddr = nil
//			s.tunnelListener = nil
//		}
//	}()
//
//	// UDP inbound.
//	if s.inConnAddr, err = net.ResolveUDPAddr("udp", s.config.UDPInNetwork); err != nil {
//		log.Errorf("Cannod resolve address: %v", s.config.UDPInNetwork)
//		return err
//	}
//	if s.inConn, err = net.ListenUDP("udp", s.inConnAddr); err != nil {
//		log.Errorf("Failed to listen UDP: %v", s.config.UDPInNetwork)
//		return err
//	}
//	func() {
//		if err != nil {
//			s.inConn.Close()
//			s.inConnAddr = nil
//			s.inConn = nil
//		}
//	}()
//
//	// UDP outbound.
//	if s.outConnAddr, err = net.ResolveUDPAddr("udp", s.config.UDPOutNetwork); err != nil {
//		log.Errorf("Cannod resolve address: %v", s.config.UDPOutNetwork)
//		return err
//	}
//	func() {
//		if err != nil {
//			s.outConnAddr = nil
//		}
//	}()
//
//	arbiter.Go(func() {
//		log := log.Entry{
//			Data: log.Fields{
//				"module": "tunnel_listener",
//			},
//		}
//	AcceptTunnelConnection:
//		for arbiter.ShouldRun() {
//			// timeout to check ShouldRun()
//			s.tunnelListener.SetDeadline(time.Now().Add(time.Second * 2))
//
//			conn, err := s.tunnelListener.Accept()
//			if err != nil {
//				switch v := err.(type) {
//				case net.Error:
//					if v.Timeout() {
//						continue AcceptTunnelConnection
//					}
//				default:
//					log.Errorf("accept error: %v", err)
//				}
//				continue
//			}
//			log.Info("accept connection from ", conn.RemoteAddr().String())
//			conn.Close()
//		}
//	})
//
//	return nil
//}
//
