package rpc

import (
	"errors"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"

	"github.com/crossmesh/fabric/proto"
	logging "github.com/sirupsen/logrus"
)

var (
	ErrInvalidRPCFunction    = errors.New("Not a valid RPC function type")
	ErrFunctionNotFound      = errors.New("RPC function not found")
	ErrInvalidRPCMessageType = errors.New("Invali RPC message type")
)

type serviceFunction struct {
	inMessageConstructor  func() interface{}
	outMessageConstructor func() interface{}
	inMessageType         reflect.Type
	outMessageType        reflect.Type
	function              interface{}
}

type Stub struct {
	functions map[string]*serviceFunction

	outcoming sync.Map
	incoming  sync.Map

	log *logging.Entry
}

func NewStub(log *logging.Entry) *Stub {
	if log != nil {
		log = logging.WithField("module", "rpc_stub")
	}
	return &Stub{
		functions: make(map[string]*serviceFunction),
		log:       log,
	}
}

func validProtocolMessageType(ty reflect.Type) bool {
	if id, exist := proto.IDByProtoType[ty]; !exist {
		return false
	} else if _, exist := proto.ConstructorByID[id]; !exist {
		return false
	}
	return true
}

func (s *Stub) RegisterWithName(name string, proc interface{}) error {
	return s.register(proc, func(interface{}) string { return name })
}

func (s *Stub) register(proc interface{}, resolveName func(interface{}) string) error {
	ty := reflect.TypeOf(proc)

	// check function type
	if ty.Kind() != reflect.Func || ty.NumIn() != 1 || ty.NumOut() != 2 ||
		!ty.Out(1).AssignableTo(reflect.TypeOf((*error)(nil)).Elem()) {
		return ErrInvalidRPCFunction
	}
	serviceFunction := &serviceFunction{
		function:       proc,
		inMessageType:  ty.In(0),
		outMessageType: ty.Out(0),
	}
	if serviceFunction.inMessageType.Kind() != reflect.Ptr || serviceFunction.outMessageType.Kind() != reflect.Ptr {
		return ErrInvalidRPCFunction
	}
	serviceFunction.inMessageType = serviceFunction.inMessageType.Elem()
	serviceFunction.outMessageType = serviceFunction.outMessageType.Elem()
	if !validProtocolMessageType(serviceFunction.inMessageType) || !validProtocolMessageType(serviceFunction.outMessageType) {
		return ErrInvalidRPCFunction
	}
	serviceFunction.inMessageConstructor = proto.ConstructorByID[proto.IDByProtoType[serviceFunction.inMessageType]]
	serviceFunction.outMessageConstructor = proto.ConstructorByID[proto.IDByProtoType[serviceFunction.outMessageType]]

	name := resolveName(proc)
	s.functions[name] = serviceFunction

	return nil
}

func (s *Stub) Register(proc interface{}) error {
	return s.register(proc, func(p interface{}) string {
		v := reflect.ValueOf(p)
		return strings.TrimPrefix(filepath.Ext(runtime.FuncForPC(v.Pointer()).Name()), ".")
	})
	return nil
}

func (s *Stub) NewServer(log *logging.Entry) *Server {
	if log == nil {
		log = logging.WithField("module", "rpc_server")
	}
	return &Server{
		stub: s,
		log:  log,
	}
}

func (s *Stub) NewClient(log *logging.Entry) *Client {
	if log == nil {
		log = logging.WithField("module", "rpc_client")
	}
	return &Client{
		stub: s,
		log:  log,
	}
}
