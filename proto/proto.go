package proto

import (
	"encoding/binary"
	"errors"
	"reflect"

	"github.com/golang/protobuf/proto"
)

var (
	ErrUnknownMessageType = errors.New("Unknown message type")
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

	MsgTypeGossip         = uint16(1)
	MsgTypeVNetController = uint16(2)
	MsgTypeVNetDriver     = uint16(3)

	MsgTypePing     = uint16(6)
	MsgTypeRawFrame = uint16(7)

	MsgTypeUserDefineBegin = uint16(64)
)

var IDByProtoType map[reflect.Type]uint16 = map[reflect.Type]uint16{
	//reflect.TypeOf((*pb.Ping)(nil)).Elem():         MsgTypePing,
	reflect.TypeOf((*NetworkRawFrame)(nil)).Elem(): MsgTypeRawFrame,
}

var ConstructorByID map[uint16]func() interface{} = map[uint16]func() interface{}{
	//MsgTypePing:         func() interface{} { return &pb.Ping{} },
	MsgTypeRawFrame: func() interface{} { return make(NetworkRawFrame, 0) },
}

const (
	ProtocolMessageHeaderSize = 3
)

func PackProtocolMessageHeader(buf []byte, msgID uint16) []byte {
	if len(buf) < ProtocolMessageHeaderSize {
		return nil
	}
	buf[0] = 0
	binary.BigEndian.PutUint16(buf[1:], msgID)
	return buf[:ProtocolMessageHeaderSize]
}

func UnpackProtocolMessageHeader(buf []byte) (uint16, []byte) {
	if len(buf) < 2 || buf[0] != 0 {
		return MsgTypeUnknown, nil
	}
	return binary.BigEndian.Uint16(buf[1:]), buf[ProtocolMessageHeaderSize:]
}
