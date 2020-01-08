package proto

import (
	"reflect"

	"git.uestc.cn/sunmxt/utt/pkg/proto/pb"
	"github.com/golang/protobuf/proto"
)

type ProtocolMessage interface {
	Type() uint32
	Encode([]byte) []byte
	Decode([]byte) error
	Len() int
}

type ProtobufMessage proto.Message

const (
	MsgTypeHello   = uint32(1)
	MsgTypeConnect = uint32(2)
	MsgTypeWelcome = uint32(3)

	MsgTypeRPC          = uint32(4)
	MsgTypePeerExchange = uint32(5)
)

var IDByProtobufType map[reflect.Type]uint32 = map[reflect.Type]uint32{
	reflect.TypeOf((*pb.RPC)(nil)).Elem():          MsgTypeRPC,
	reflect.TypeOf((*pb.PeerExchange)(nil)).Elem(): MsgTypePeerExchange,
}

var ConstructorByID map[uint32]func() interface{} = map[uint32]func() interface{}{
	MsgTypeHello:        func() interface{} { return Hello{} },
	MsgTypeConnect:      func() interface{} { return Connect{} },
	MsgTypeWelcome:      func() interface{} { return Welcome{} },
	MsgTypeRPC:          func() interface{} { return pb.RPC{} },
	MsgTypePeerExchange: func() interface{} { return pb.PeerExchange{} },
}
