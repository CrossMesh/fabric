package rpc

import (
	"reflect"

	"github.com/crossmesh/fabric/proto"
	pb "github.com/golang/protobuf/proto"
	logging "github.com/sirupsen/logrus"
)

type Server struct {
	stub *Stub
	log  *logging.Entry
}

func (s *Server) Invoke(name string, id uint32, raw []byte, reply func([]byte, error) error) (err error) {
	// get service function
	serviceFunction, exist := s.stub.functions[name]
	if !exist || serviceFunction == nil {
		s.log.Errorf("service function \"%v\" not found: ", name)
		return ErrFunctionNotFound
	}

	// unmarshal message
	msg := serviceFunction.inMessageConstructor()
	switch v := msg.(type) {
	case proto.ProtobufMessage:
		err = pb.Unmarshal(raw, v)
	case proto.ProtocolMessage:
		err = v.Decode(raw)
	default:
		err = ErrInvalidRPCMessageType
	}
	if err != nil {
		s.log.Error("failed to unmarshal rpc message: ", err)
		return err
	}

	// invoke
	outs := reflect.ValueOf(serviceFunction.function).Call([]reflect.Value{
		reflect.ValueOf(msg),
	})

	// marshal
	var (
		outErr   error
		replyRaw []byte
	)
	if outs[1].Interface() != nil {
		outErr = outs[1].Interface().(error)
	}
	switch v := outs[0].Interface().(type) {
	case proto.ProtobufMessage:
		if v != nil {
			replyRaw, err = pb.Marshal(v)
		}
	case proto.ProtocolMessage:
		if v != nil {
			replyRaw = make([]byte, 0, v.Len())
			replyRaw = v.Encode(replyRaw)
		}
	default:
		err = ErrInvalidRPCMessageType
	}
	if err != nil {
		s.log.Error("failed to marshal rpc message: ", err)
		return err
	}
	if err = reply(replyRaw, outErr); err != nil {
		s.log.Error("failed to send reply: ", err)
	}
	return err
}
