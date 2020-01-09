package rpc

import (
	"reflect"
	"testing"

	"git.uestc.cn/sunmxt/utt/pkg/proto"
	"git.uestc.cn/sunmxt/utt/pkg/proto/pb"
	"github.com/stretchr/testify/assert"
)

func ServiceFunctionA() {}

func ServiceFunctionB(msg *proto.Hello) (*pb.PeerExchange, error) {
	return nil, nil
}

func ServiceFunctionC(msg proto.Hello) (*pb.PeerExchange, error) {
	return nil, nil
}

func ServiceFunctionD(msg *proto.Hello, err error) (*pb.PeerExchange, error) {
	return nil, nil
}

func ServiceFunctionE(msg *proto.Hello) (pb.PeerExchange, error) {
	return pb.PeerExchange{}, nil
}

func TestStub(t *testing.T) {
	// stub creating.
	s := NewStub(nil)
	assert.NotEqual(t, s.log, nil, "log entry should not be nil if not provided.")
	assert.NotEqual(t, s.functions, nil, "functions map should not be nil")

	// test registering.
	assert.Equal(t, s.Register(ServiceFunctionA), ErrInvalidRPCFunction, "ServiceFunctionA should be treat as invalid function")
	assert.NotEqual(t, s.Register(ServiceFunctionB), ErrInvalidRPCFunction, "ServiceFunctionB should be treat as valid function")
	sf, exists := s.functions["ServiceFunctionB"]
	assert.Equal(t, true, exists, "ServiceFunctionB not exists.")
	if actual, excepted := reflect.ValueOf(sf.inMessageConstructor), reflect.ValueOf(proto.ConstructorByID[proto.MsgTypeHello]); actual != excepted {
		t.Errorf("inMessageConstructor unmatched: actual = %v, excepted = %v", actual, excepted)
	}
	if actual, excepted := reflect.ValueOf(sf.outMessageConstructor), reflect.ValueOf(proto.ConstructorByID[proto.IDByProtoType[reflect.TypeOf((*pb.PeerExchange)(nil)).Elem()]]); actual != excepted {
		t.Errorf("outMessageConstructor unmatched: actual = %v, excepted = %v", actual, excepted)
	}
	assert.Equal(t, s.Register(ServiceFunctionC), ErrInvalidRPCFunction, "ServiceFunctionC should be treat as valid function")
	assert.Equal(t, s.Register(ServiceFunctionD), ErrInvalidRPCFunction, "ServiceFunctionD should be treat as valid function")
	assert.Equal(t, s.Register(ServiceFunctionE), ErrInvalidRPCFunction, "ServiceFunctionE should be treat as valid function")
}
