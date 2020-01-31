package edgerouter

import (
	"context"

	"git.uestc.cn/sunmxt/utt/proto/pb"
)

func (r *EdgeRouter) Ping(msg *pb.Ping) (*pb.Ping, error) {
	return msg, nil
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
