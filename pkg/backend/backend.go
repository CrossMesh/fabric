package backend

import (
	"context"
	"errors"
	"net"

	arbit "git.uestc.cn/sunmxt/utt/pkg/arbiter"
	"git.uestc.cn/sunmxt/utt/pkg/config"
	"git.uestc.cn/sunmxt/utt/pkg/proto/pb"
	logging "github.com/sirupsen/logrus"
)

const (
	defaultBufferSize = 512
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
	Send(context.Context, []byte) error
	Close() error
}

type Backend interface {
	Type() pb.PeerBackend_BackendType
	Priority() uint32

	Connect(context.Context, string) (Link, error)
	Watch(func(Backend, []byte, string)) error
	Shutdown()

	Publish() string
	IP() net.IP
}

var creators map[string]func(*arbit.Arbiter, *logging.Entry, *config.Backend) (Backend, error) = map[string]func(*arbit.Arbiter, *logging.Entry, *config.Backend) (Backend, error){
	"tcp": createTCPBackend,
}

var nameByType map[pb.PeerBackend_BackendType]string = map[pb.PeerBackend_BackendType]string{
	pb.PeerBackend_TCP: "tcp",
}

func CreateBackend(ty string, arbiter *arbit.Arbiter, log *logging.Entry, cfg *config.Backend) (Backend, error) {
	factory, hasCreator := creators[ty]
	if !hasCreator {
		return nil, ErrBackendTypeUnknown
	}
	return factory(arbiter, log, cfg)
}
