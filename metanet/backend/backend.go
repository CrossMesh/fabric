package backend

import (
	"errors"
	"net"
)

var (
	ErrUnknownDestinationType = errors.New("Unknown destination type")
	ErrOperationCanceled      = errors.New("operation canceled")
	ErrConnectionDeined       = errors.New("connection deined")
	ErrConnectionClosed       = errors.New("connection closed")
	ErrBackendTypeUnknown     = errors.New("Unknown backend type")
	ErrInvalidBackendConfig   = errors.New("Unknown backend config")
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
	Watch(func(Backend, []byte, string)) error
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
		return "unknown"
	}
}
