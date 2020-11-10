package backend

import (
	"fmt"
	"net"
)

type Endpoint struct {
	Type     Type
	Endpoint string
}

var NullEndpoint = Endpoint{Type: UnknownBackend}

func (p Endpoint) String() string {
	return p.Type.String() + "://" + p.Endpoint
}

var TypeByName = map[string]Type{
	TCPBackend.String(): TCPBackend,
}

type Link interface {
	Send([]byte) error
	Close() error
}

type Backend interface {
	Type() Type

	Connect(string) (Link, error)
	Shutdown()

	Publish() string
	IP() net.IP
}

// Type is identifer of backend.
type Type uint8

const (
	// UnknownBackend identifies unknown endpoint.
	UnknownBackend = Type(0)
	// TCPBackend identifies TCP Backend.
	TCPBackend = Type(1)
)

func (b Type) String() string {
	switch b {
	case TCPBackend:
		return "tcp"
	default:
		return fmt.Sprintf("unknown(%v)", uint8(b))
	}
}
