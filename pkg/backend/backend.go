package backend

import (
	"context"
	"errors"
	"net"

	"git.uestc.cn/sunmxt/utt/pkg/proto/pb"
)

const (
	defaultBufferSize = 512
)

var (
	ErrUnknownDestinationType = errors.New("Unknown destination type")
)

type Backend interface {
	Type() pb.PeerBackend_BackendType
	Priority() int

	Send(context.Context, []byte, interface{}) error
	Watch(func([]byte, interface{})) error

	IP() net.IP
}
