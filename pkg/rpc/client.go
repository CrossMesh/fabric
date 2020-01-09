package rpc

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"git.uestc.cn/sunmxt/utt/pkg/proto"
	pb "github.com/golang/protobuf/proto"
	logging "github.com/sirupsen/logrus"
)

var (
	ErrRPCCanceled = errors.New("RPC Requesr canceled")
)

type Client struct {
	stub  *Stub
	id    uint32
	log   *logging.Entry
	calls sync.Map
}

type rpcResponse struct {
	err error
	raw []byte
}

func (c *Client) Call(ctx context.Context, name string, msg interface{}, send func(uint32, string, []byte) error) (resp interface{}, err error) {
	// get service function
	serviceFunction, exist := c.stub.functions[name]
	if !exist || serviceFunction == nil {
		c.log.Errorf("service function \"%v\" not found: ", name)
		return nil, ErrFunctionNotFound
	}

	// new request id
	rid := atomic.AddUint32(&c.id, 1)

	// marshal
	var raw []byte
	switch v := msg.(type) {
	case proto.ProtobufMessage:
		raw, err = pb.Marshal(v)
	case proto.ProtocolMessage:
		raw = v.Encode(make([]byte, 0, v.Len()))
	default:
		err = ErrInvalidRPCMessageType
	}
	if err != nil {
		c.log.Error("failed to marshal rpc message: ", err)
		return nil, err
	}

	notify := make(chan *rpcResponse, 0)
	c.calls.Store(rid, notify)
	defer func() {
		c.calls.Delete(rid)
	}()

	// request
	if err = send(rid, name, raw); err != nil {
		c.log.Error("failed to send rpc message: ", err)
		return nil, err
	}

	// wait for resp.
	var respRaw []byte
	select {
	case r := <-notify:
		if r.err != nil {
			return nil, r.err
		}
		respRaw = r.raw

	case <-ctx.Done():
		return nil, ErrRPCCanceled
	}
	if respRaw != nil {
		resp = serviceFunction.outMessageConstructor()
		switch v := resp.(type) {
		case proto.ProtobufMessage:
			err = pb.Unmarshal(respRaw, v)
		case proto.ProtocolMessage:
			err = v.Decode(respRaw)
		default:
			err = ErrInvalidRPCMessageType
		}
		if err != nil {
			c.log.Error("failed to unmarshal rpc message: ", err)
			return nil, err
		}
	}

	return resp, nil
}

func (c *Client) Reply(id uint32, msg []byte, err error) {
	if v, ok := c.calls.Load(id); !ok || v == nil {
		return
	} else if notify, ok := v.(chan *rpcResponse); !ok || notify == nil {
		return
	} else {
		notify <- &rpcResponse{
			err: err,
			raw: msg,
		}
	}
}
