package backend

import (
	"errors"
	"net"

	"git.uestc.cn/sunmxt/utt/config"
	"git.uestc.cn/sunmxt/utt/proto/pb"
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
	Type() pb.PeerBackend_BackendType
	Priority() uint32

	Connect(string) (Link, error)
	Watch(func(Backend, []byte, string)) error
	Shutdown()

	Publish() string
	IP() net.IP
}

type PeerBackendIdentity struct {
	Type     pb.PeerBackend_BackendType
	Endpoint string
}

func (p *PeerBackendIdentity) String() string {
	return p.Type.String() + ":" + p.Endpoint
}

type BackendCreator interface {
	Type() pb.PeerBackend_BackendType
	Priority() uint32
	Publish() string

	New(*arbit.Arbiter, *logging.Entry) (Backend, error)
}

var creators = map[string]func(*config.Backend) (BackendCreator, error){
	"tcp": newTCPCreator,
}

var NameByType = map[pb.PeerBackend_BackendType]string{
	pb.PeerBackend_TCP: "tcp",
}

var TypeByName = map[string]pb.PeerBackend_BackendType{
	"tcp": pb.PeerBackend_TCP,
}

func GetCreator(ty string, cfg *config.Backend) (BackendCreator, error) {
	factory, hasCreator := creators[ty]
	if !hasCreator {
		return nil, ErrBackendTypeUnknown
	}
	return factory(cfg)
}

func GetBackendIdentityName(ty pb.PeerBackend_BackendType) string {
	name, ok := NameByType[ty]
	if !ok {
		return "unknown"
	}
	return name
}
