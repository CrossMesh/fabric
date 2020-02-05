package edgerouter

import (
	"errors"

	"git.uestc.cn/sunmxt/utt/proto"
	"git.uestc.cn/sunmxt/utt/proto/pb"

	"git.uestc.cn/sunmxt/utt/route"
	"git.uestc.cn/sunmxt/utt/rpc"
	pbp "github.com/golang/protobuf/proto"
	logging "github.com/sirupsen/logrus"
)

var (
	ErrInvalidRPCMessageType = errors.New("invalid RPC message type")
)

func (r *EdgeRouter) initRPCStub() (err error) {
	if r.rpc == nil {
		r.rpc = rpc.NewStub(logging.WithField("module", "rpc_stub"))
	}
	fns := map[string]interface{}{
		"GossipExchange": r.GossipExchange,
		"Ping":           r.Ping,
	}
	for name, fn := range fns {
		if err = r.rpc.RegisterWithName(name, fn); err != nil {
			return err
		}
	}
	r.rpcClient = r.rpc.NewClient(logging.WithField("module", "edge_router_rpc_client"))
	r.rpcServer = r.rpc.NewServer(logging.WithField("module", "edge_router_rpc_server"))
	return nil
}

func (r *EdgeRouter) sendRPCRequest(peer route.MembershipPeer, rid uint32, name string, data []byte) (err error) {
	return r.sendRPCMessage(peer, &pb.RPC{
		Id:       rid,
		Type:     pb.RPC_Request,
		Function: name,
		Data:     data,
	})
}

func (r *EdgeRouter) sendRPCMessage(peer route.MembershipPeer, msg *pb.RPC) (err error) {
	var dummy [proto.ProtocolMessageHeaderSize]byte
	buf := pbp.NewBuffer(dummy[:proto.ProtocolMessageHeaderSize]) // internal buffer will be replaced by protobuf.
	proto.PackProtocolMessageHeader(dummy[:proto.ProtocolMessageHeaderSize], proto.MsgTypeRPC)
	if err = buf.Marshal(msg); err != nil {
		return err
	}
	return r.forwardVTEPPeer(buf.Bytes(), peer)
}

func (r *EdgeRouter) receiveRPCMessage(data []byte, peer route.MembershipPeer) {
	msg := &pb.RPC{}
	if err := pbp.Unmarshal(data, msg); err != nil {
		r.log.Warn("drop invalid rpc message: ", err)
		return
	}
	switch msg.Type {
	case pb.RPC_Error:
		r.rpcClient.Reply(msg.Id, nil, errors.New(msg.Error))
	case pb.RPC_Reply:
		r.rpcClient.Reply(msg.Id, msg.Data, nil)
	case pb.RPC_Request:
		r.rpcServer.Invoke(msg.Function, msg.Id, msg.Data, func(data []byte, err error) error {
			reply := &pb.RPC{
				Id:       msg.Id,
				Function: msg.Function,
				Type:     pb.RPC_Reply,
				Data:     data,
			}
			if err != nil {
				reply.Error = err.Error()
				reply.Type = pb.RPC_Error
			}
			return r.sendRPCMessage(peer, reply)
		})
	}
}

// RPC Client
type RPCClient struct {
	*rpc.Client

	send func(id uint32, name string, bin []byte) error
}

func (r *EdgeRouter) RPCClient(peer route.MembershipPeer) *RPCClient {
	return &RPCClient{
		Client: r.rpcClient,
		send: func(id uint32, name string, proto []byte) error {
			return r.sendRPCRequest(peer, id, name, proto)
		},
	}
}
