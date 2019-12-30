package relay

import (
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"git.uestc.cn/sunmxt/utt/pkg/arbiter"
	"git.uestc.cn/sunmxt/utt/pkg/config"
	"git.uestc.cn/sunmxt/utt/pkg/mux"
	"git.uestc.cn/sunmxt/utt/pkg/proto"
	log "github.com/sirupsen/logrus"
)

type RelayClient struct {
	connTo  *net.TCPAddr
	reverse *net.UDPAddr
	remote  *net.UDPAddr
	tunnel  *net.UDPAddr
	acl     *config.ACL

	conn    *net.TCPConn
	muxer   *mux.StreamMuxer
	demuxer *mux.StreamDemuxer

	log *log.Entry
}

func NewClient(connTo *net.TCPAddr, reverse *net.UDPAddr, remote *net.UDPAddr, tunnel *net.UDPAddr, acl *config.ACL, log *log.Entry) *RelayClient {
	return &RelayClient{
		connTo:  connTo,
		reverse: reverse,
		remote:  remote,
		tunnel:  tunnel,
		acl:     acl,
		log:     log,
	}
}

func (c *RelayClient) handshake(arbiter *arbiter.Arbiter) (result *proto.ConnectResult, err error) {
	buf := make([]byte, defaultBufferSize)

	// hello
	var hello *proto.Hello
	c.log.Info("waiting for handshake hello.", c.connTo.String())
	for arbiter.ShouldRun() && hello == nil {
		if err = c.conn.SetReadDeadline(time.Now().Add(time.Second * 30)); err != nil {
			return nil, err
		}
		read, err := c.conn.Read(buf)
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				err = errors.New("closed due to inactivity.")
				return nil, nil
			}
			if err == io.EOF {
				c.log.Info("connection closed by server.")
				return nil, nil
			}
			return nil, err
		}
		c.demuxer.Demux(buf[:read], func(pkt []byte) {
			if hello != nil {
				return
			}
			hello = &proto.Hello{}
			err = hello.Decode(pkt)
		})
		if err != nil {
			return nil, err
		}
	}
	if hello == nil {
		return nil, nil
	}

	// connect
	var connBuf []byte
	connectReq := &proto.Connect{
		ACLKey: fmt.Sprintf("%v:%v", c.remote.IP.String(), c.remote.Port),
	}
	if c.acl != nil {
		connectReq.Sign(hello.Challenge[:], []byte(c.acl.PSK))
	}
	if connBuf, err = connectReq.Encode(buf); err != nil {
		return nil, err
	}
	if _, err = c.muxer.Mux(connBuf); err != nil {
		return nil, err
	}
	c.log.Info("connect request sent. waiting for welcome reply.")

	// welcome
	var welcome *proto.ConnectResult
	for arbiter.ShouldRun() && welcome == nil {
		err = nil

		if err = c.conn.SetReadDeadline(time.Now().Add(time.Second * 30)); err != nil {
			return nil, err
		}
		read, err := c.conn.Read(buf)
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				err = errors.New("closed due to inactivity")
				return nil, nil
			}
			if err == io.EOF {
				c.log.Info("connection closed by server.")
				return nil, nil
			}
			return nil, err
		}
		c.demuxer.Demux(buf[:read], func(pkt []byte) {
			if welcome != nil {
				return
			}
			welcome = &proto.ConnectResult{}
			err = welcome.Decode(pkt)
		})
	}
	if welcome == nil {
		return nil, err
	}
	c.log.Info("welcome reply received.")

	if !welcome.Welcome {
		c.log.Error("denied by server: ", welcome.DecodeMessage())
	}
	return welcome, nil
}

func (c *RelayClient) reverseRelay(arbiter *arbiter.Arbiter, logBase *log.Entry) {
	log := logBase.WithFields(log.Fields{
		"traffic": "reverse",
		"to":      c.reverse,
	})
	out, err := net.ListenPacket("udp", ":0")
	if err != nil {
		log.Error("net.ListenPacket error: ", err)
		return
	}
	defer arbiter.Shutdown()

	relay := NewDemuxRelay(c.conn, out, c.reverse, log)
	if err = relay.Do(arbiter); err != nil {
		log.Error("demux relay error: ", err)
	}
}

func (c *RelayClient) forwardRelay(arbiter *arbiter.Arbiter, logBase *log.Entry) {
	log := logBase.WithFields(log.Fields{
		"traffic": "forward",
		"from":    c.tunnel,
	})
	relay := NewMuxRelay(c.conn, c.tunnel, log)
	if err := relay.Do(arbiter); err != nil {
		log.Error("mux relay error: ", err)
	}
}

func (c *RelayClient) doRelay(globalArbiter *arbiter.Arbiter) error {
	relayArbiter := arbiter.New(c.log)

	// forward
	relayArbiter.Go(func() {
		c.forwardRelay(relayArbiter, c.log)
	})
	// reverse
	relayArbiter.Go(func() {
		c.reverseRelay(relayArbiter, c.log)
	})
	relayArbiter.Wait()

	return nil
}

func (c *RelayClient) Do(arbiter *arbiter.Arbiter) (err error) {
	c.muxer, c.demuxer = nil, nil

	c.log.Info("connecting to ", c.connTo.String())
	if c.conn, err = net.DialTCP("tcp", nil, c.connTo); err != nil {
		c.log.Error("connect error: ", err)
		return err
	}
	c.muxer, c.demuxer = mux.NewStreamMuxer(c.conn), mux.NewStreamDemuxer()
	defer c.conn.Close()
	c.log.Info("connected to ", c.connTo.String())

	// handshake
	c.log.Info("handshaking...", c.connTo.String())
	var connectResult *proto.ConnectResult
	if connectResult, err = c.handshake(arbiter); err != nil {
		return err
	}
	if connectResult == nil {
		err = errors.New("handshake result is nil")
		c.log.Error(err)
		return err
	}
	accepted := connectResult.Welcome
	if !accepted {
		err = errors.New(connectResult.DecodeMessage())
		c.log.Error("connection deined by server: ", err)
		return err
	}

	return c.doRelay(arbiter)
}
