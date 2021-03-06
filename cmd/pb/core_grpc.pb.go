// Code generated by protoc-gen-go-grpc. DO NOT EDIT.

package pb

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion7

// DaemonControlClient is the client API for DaemonControl service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type DaemonControlClient interface {
	ReloadConfig(ctx context.Context, in *ReloadRequest, opts ...grpc.CallOption) (*Result, error)
	ExecuteCommand(ctx context.Context, opts ...grpc.CallOption) (DaemonControl_ExecuteCommandClient, error)
	GetDaemonCommands(ctx context.Context, in *DaemonCommandListRequest, opts ...grpc.CallOption) (DaemonControl_GetDaemonCommandsClient, error)
}

type daemonControlClient struct {
	cc grpc.ClientConnInterface
}

func NewDaemonControlClient(cc grpc.ClientConnInterface) DaemonControlClient {
	return &daemonControlClient{cc}
}

func (c *daemonControlClient) ReloadConfig(ctx context.Context, in *ReloadRequest, opts ...grpc.CallOption) (*Result, error) {
	out := new(Result)
	err := c.cc.Invoke(ctx, "/pb.DaemonControl/ReloadConfig", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *daemonControlClient) ExecuteCommand(ctx context.Context, opts ...grpc.CallOption) (DaemonControl_ExecuteCommandClient, error) {
	stream, err := c.cc.NewStream(ctx, &_DaemonControl_serviceDesc.Streams[0], "/pb.DaemonControl/ExecuteCommand", opts...)
	if err != nil {
		return nil, err
	}
	x := &daemonControlExecuteCommandClient{stream}
	return x, nil
}

type DaemonControl_ExecuteCommandClient interface {
	Send(*CommandExecuteRequest) error
	Recv() (*CommandExecuteResult, error)
	grpc.ClientStream
}

type daemonControlExecuteCommandClient struct {
	grpc.ClientStream
}

func (x *daemonControlExecuteCommandClient) Send(m *CommandExecuteRequest) error {
	return x.ClientStream.SendMsg(m)
}

func (x *daemonControlExecuteCommandClient) Recv() (*CommandExecuteResult, error) {
	m := new(CommandExecuteResult)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *daemonControlClient) GetDaemonCommands(ctx context.Context, in *DaemonCommandListRequest, opts ...grpc.CallOption) (DaemonControl_GetDaemonCommandsClient, error) {
	stream, err := c.cc.NewStream(ctx, &_DaemonControl_serviceDesc.Streams[1], "/pb.DaemonControl/GetDaemonCommands", opts...)
	if err != nil {
		return nil, err
	}
	x := &daemonControlGetDaemonCommandsClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type DaemonControl_GetDaemonCommandsClient interface {
	Recv() (*DaemonCommand, error)
	grpc.ClientStream
}

type daemonControlGetDaemonCommandsClient struct {
	grpc.ClientStream
}

func (x *daemonControlGetDaemonCommandsClient) Recv() (*DaemonCommand, error) {
	m := new(DaemonCommand)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// DaemonControlServer is the server API for DaemonControl service.
// All implementations must embed UnimplementedDaemonControlServer
// for forward compatibility
type DaemonControlServer interface {
	ReloadConfig(context.Context, *ReloadRequest) (*Result, error)
	ExecuteCommand(DaemonControl_ExecuteCommandServer) error
	GetDaemonCommands(*DaemonCommandListRequest, DaemonControl_GetDaemonCommandsServer) error
	mustEmbedUnimplementedDaemonControlServer()
}

// UnimplementedDaemonControlServer must be embedded to have forward compatible implementations.
type UnimplementedDaemonControlServer struct {
}

func (UnimplementedDaemonControlServer) ReloadConfig(context.Context, *ReloadRequest) (*Result, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ReloadConfig not implemented")
}
func (UnimplementedDaemonControlServer) ExecuteCommand(DaemonControl_ExecuteCommandServer) error {
	return status.Errorf(codes.Unimplemented, "method ExecuteCommand not implemented")
}
func (UnimplementedDaemonControlServer) GetDaemonCommands(*DaemonCommandListRequest, DaemonControl_GetDaemonCommandsServer) error {
	return status.Errorf(codes.Unimplemented, "method GetDaemonCommands not implemented")
}
func (UnimplementedDaemonControlServer) mustEmbedUnimplementedDaemonControlServer() {}

// UnsafeDaemonControlServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to DaemonControlServer will
// result in compilation errors.
type UnsafeDaemonControlServer interface {
	mustEmbedUnimplementedDaemonControlServer()
}

func RegisterDaemonControlServer(s *grpc.Server, srv DaemonControlServer) {
	s.RegisterService(&_DaemonControl_serviceDesc, srv)
}

func _DaemonControl_ReloadConfig_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ReloadRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DaemonControlServer).ReloadConfig(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/pb.DaemonControl/ReloadConfig",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DaemonControlServer).ReloadConfig(ctx, req.(*ReloadRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _DaemonControl_ExecuteCommand_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(DaemonControlServer).ExecuteCommand(&daemonControlExecuteCommandServer{stream})
}

type DaemonControl_ExecuteCommandServer interface {
	Send(*CommandExecuteResult) error
	Recv() (*CommandExecuteRequest, error)
	grpc.ServerStream
}

type daemonControlExecuteCommandServer struct {
	grpc.ServerStream
}

func (x *daemonControlExecuteCommandServer) Send(m *CommandExecuteResult) error {
	return x.ServerStream.SendMsg(m)
}

func (x *daemonControlExecuteCommandServer) Recv() (*CommandExecuteRequest, error) {
	m := new(CommandExecuteRequest)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func _DaemonControl_GetDaemonCommands_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(DaemonCommandListRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(DaemonControlServer).GetDaemonCommands(m, &daemonControlGetDaemonCommandsServer{stream})
}

type DaemonControl_GetDaemonCommandsServer interface {
	Send(*DaemonCommand) error
	grpc.ServerStream
}

type daemonControlGetDaemonCommandsServer struct {
	grpc.ServerStream
}

func (x *daemonControlGetDaemonCommandsServer) Send(m *DaemonCommand) error {
	return x.ServerStream.SendMsg(m)
}

var _DaemonControl_serviceDesc = grpc.ServiceDesc{
	ServiceName: "pb.DaemonControl",
	HandlerType: (*DaemonControlServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "ReloadConfig",
			Handler:    _DaemonControl_ReloadConfig_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "ExecuteCommand",
			Handler:       _DaemonControl_ExecuteCommand_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
		{
			StreamName:    "GetDaemonCommands",
			Handler:       _DaemonControl_GetDaemonCommands_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "cmd/pb/core.proto",
}
