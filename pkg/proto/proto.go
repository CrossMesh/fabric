package proto

import (
	"encoding/binary"
	"errors"
	"reflect"

	"git.uestc.cn/sunmxt/utt/pkg/proto/pb"
	"github.com/golang/protobuf/proto"
)

var (
	ErrMessageKindUnregistered = errors.New("Message kind not registered")
	ErrUnknownMessageType      = errors.New("Unknown message type")
)

type ProtocolMessage interface {
	Type() uint16
	Encode([]byte) []byte
	Decode([]byte) error
	Len() int
}

type ProtobufMessage proto.Message

const (
	MsgTypeUnknown = uint16(0)
	MsgTypeHello   = uint16(1)
	MsgTypeConnect = uint16(2)
	MsgTypeWelcome = uint16(3)

	MsgTypeRPC          = uint16(4)
	MsgTypePeerExchange = uint16(5)
)

var IDByProtoType map[reflect.Type]uint16 = map[reflect.Type]uint16{
	reflect.TypeOf((*Hello)(nil)).Elem():           MsgTypeHello,
	reflect.TypeOf((*Connect)(nil)).Elem():         MsgTypeConnect,
	reflect.TypeOf((*Welcome)(nil)).Elem():         MsgTypeWelcome,
	reflect.TypeOf((*pb.RPC)(nil)).Elem():          MsgTypeRPC,
	reflect.TypeOf((*pb.PeerExchange)(nil)).Elem(): MsgTypePeerExchange,
}

var ConstructorByID map[uint16]func() interface{} = map[uint16]func() interface{}{
	MsgTypeHello:        func() interface{} { return &Hello{} },
	MsgTypeConnect:      func() interface{} { return &Connect{} },
	MsgTypeWelcome:      func() interface{} { return &Welcome{} },
	MsgTypeRPC:          func() interface{} { return &pb.RPC{} },
	MsgTypePeerExchange: func() interface{} { return &pb.PeerExchange{} },
}

func PackProtocolMessageHeader(buf []byte, msg interface{}) (packed []byte, err error) {
	ty := reflect.TypeOf(msg)
	if ty.Kind() == reflect.Ptr {
		ty = ty.Elem()
	}
	msgTypeID, registered := IDByProtoType[ty]
	if !registered {
		return nil, ErrMessageKindUnregistered
	}
	if len(buf) < 2 {
		packed = make([]byte, 2)
	}
	binary.BigEndian.PutUint16(packed, msgTypeID)
	return packed[:2], nil
}

func UnpackProtocolMessageHeader(buf []byte) uint16 {
	if len(buf) < 2 {
		return MsgTypeUnknown
	}
	return binary.BigEndian.Uint16(buf)
}
