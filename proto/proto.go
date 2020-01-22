package proto

import (
	"encoding/binary"
	"errors"
	"reflect"

	"git.uestc.cn/sunmxt/utt/proto/pb"
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
	MsgTypePing         = uint16(6)
	MsgTypeRawFrame     = uint16(7)
)

var IDByProtoType map[reflect.Type]uint16 = map[reflect.Type]uint16{
	reflect.TypeOf((*Hello)(nil)).Elem():           MsgTypeHello,
	reflect.TypeOf((*Connect)(nil)).Elem():         MsgTypeConnect,
	reflect.TypeOf((*Welcome)(nil)).Elem():         MsgTypeWelcome,
	reflect.TypeOf((*pb.RPC)(nil)).Elem():          MsgTypeRPC,
	reflect.TypeOf((*pb.PeerExchange)(nil)).Elem(): MsgTypePeerExchange,
	reflect.TypeOf((*pb.Ping)(nil)).Elem():         MsgTypePing,
	reflect.TypeOf((*NetworkRawFrame)(nil)).Elem(): MsgTypeRawFrame,
}

var ConstructorByID map[uint16]func() interface{} = map[uint16]func() interface{}{
	MsgTypeHello:        func() interface{} { return &Hello{} },
	MsgTypeConnect:      func() interface{} { return &Connect{} },
	MsgTypeWelcome:      func() interface{} { return &Welcome{} },
	MsgTypeRPC:          func() interface{} { return &pb.RPC{} },
	MsgTypePeerExchange: func() interface{} { return &pb.PeerExchange{} },
	MsgTypePing:         func() interface{} { return &pb.Ping{} },
	MsgTypeRawFrame:     func() interface{} { return make(NetworkRawFrame, 0) },
}

const (
	ProtocolMessageHeaderSize = 2
)

func PackProtocolMessageHeader(buf []byte, msgID uint16) []byte {
	if len(buf) < ProtocolMessageHeaderSize {
		return nil
	}
	binary.BigEndian.PutUint16(buf, msgID)
	return buf[:ProtocolMessageHeaderSize]
}
func PackProtocolMessageHeaderByMsg(buf []byte, msg interface{}) (packed []byte, err error) {
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
	return packed[:ProtocolMessageHeaderSize], nil
}

func UnpackProtocolMessageHeader(buf []byte) uint16 {
	if len(buf) < 2 {
		return MsgTypeUnknown
	}
	return binary.BigEndian.Uint16(buf)
}
