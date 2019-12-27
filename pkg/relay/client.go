package relay

import (
	"errors"
	"net"

	"git.uestc.cn/sunmxt/utt/pkg/arbiter"
	log "github.com/sirupsen/logrus"
)

type RelayClient struct {
	connTo  *net.TCPAddr
	reverse *net.UDPAddr
	remote  *net.UDPAddr
	tunnel  *net.UDPAddr

	Log *log.Entry
}

func NewClient(connTo *net.TCPAddr, reverse *net.UDPAddr, remote *net.UDPAddr, tunnel *net.UDPAddr) *RelayClient {
	return &RelayClient{
		connTo:  connTo,
		reverse: reverse,
		remote:  remote,
		tunnel:  tunnel,
	}
}

func (c *RelayClient) Do(arbiter *arbiter.Arbiter) error {
	return errors.New("not implemented.")
}
