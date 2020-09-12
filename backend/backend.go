package backend

import (
	"errors"
	"net"

	"github.com/crossmesh/fabric/config"
	logging "github.com/sirupsen/logrus"
	arbit "github.com/sunmxt/arbiter"
)

const (
	defaultBufferSize = 4096
)

var (
	ErrUnknownDestinationType = errors.New("Unknown destination type")
	ErrOperationCanceled      = errors.New("operation canceled")
	ErrConnectionDeined       = errors.New("connection deined")
	ErrConnectionClosed       = errors.New("connection closed")
	ErrBackendTypeUnknown     = errors.New("Unknown backend type")
	ErrInvalidBackendConfig   = errors.New("Unknown backend config")
)

type Link interface {
	Send([]byte) error
	Close() error
}

type Backend interface {
	Type() Type
	Priority() uint32

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

type Endpoint struct {
	Type     Type
	Endpoint string
}

var NullEndpoint = Endpoint{Type: UnknownBackend}

func (p *Endpoint) String() string {
	return p.Type.String() + ":" + p.Endpoint
}

type BackendCreator interface {
	Type() Type
	Priority() uint32
	Publish() string

	New(*arbit.Arbiter, *logging.Entry) (Backend, error)
}

var creators = map[string]func(*config.Backend) (BackendCreator, error){
	"tcp": newTCPCreator,
}

var TypeByName = map[string]Type{
	TCPBackend.String(): TCPBackend,
}

func GetCreator(ty string, cfg *config.Backend) (BackendCreator, error) {
	factory, hasCreator := creators[ty]
	if !hasCreator {
		return nil, ErrBackendTypeUnknown
	}
	return factory(cfg)
}
