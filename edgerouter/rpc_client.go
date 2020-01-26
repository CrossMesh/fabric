package edgerouter

import (
	"context"

	"git.uestc.cn/sunmxt/utt/proto/pb"
	"git.uestc.cn/sunmxt/utt/route"
	"git.uestc.cn/sunmxt/utt/rpc"
)

// RPC Client
type RPCClient struct {
	*rpc.Client

	send func(id uint32, name string, bin []byte) error
}

func (r *EdgeRouter) RPCClient(peer route.Peer) *RPCClient {
	return &RPCClient{
		Client: r.rpcClient,
		send: func(id uint32, name string, proto []byte) error {
			return r.sendRPCMessage(peer, id, name, proto)
		},
	}
}

func (c *RPCClient) Ping(ctx context.Context, msg *pb.Ping) (*pb.Ping, error) {
	raw, err := c.Call(ctx, "Ping", msg, c.send)
	if err != nil {
		return nil, err
	}
	resp, validType := raw.(*pb.Ping)
	if !validType {
		return nil, ErrInvalidRPCMessageType
	}
	return resp, nil
}

func (c *RPCClient) PeerExchange(ctx context.Context, peers *pb.PeerExchange) (*pb.PeerExchange, error) {
	raw, err := c.Call(ctx, "PingExchange", peers, c.send)
	if err != nil {
		return nil, err
	}
	resp, validType := raw.(*pb.PeerExchange)
	if !validType {
		return nil, ErrInvalidRPCMessageType
	}
	return resp, nil
}
