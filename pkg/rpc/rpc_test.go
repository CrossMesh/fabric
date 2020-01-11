package rpc

import (
	"context"
	"reflect"
	"testing"
	"time"

	"git.uestc.cn/sunmxt/utt/pkg/proto"
	"github.com/stretchr/testify/assert"
)

type TestMessage struct {
	Str string
}

func (c *TestMessage) Type() uint16 {
	return 0
}

func (c *TestMessage) Encode(buf []byte) []byte {
	buf = buf[:0]
	buf = append(buf, []byte(c.Str)...)
	return buf
}

func (c *TestMessage) Decode(buf []byte) error {
	c.Str = string(buf)
	return nil
}

func (c *TestMessage) Len() int {
	return len([]byte(c.Str))
}

func ServiceCalleeA(msg *TestMessage) (reply *TestMessage, err error) {
	return &TestMessage{
		Str: msg.Str,
	}, nil
}
func TestRPC(t *testing.T) {
	// mock IDByProtoType
	idByProtoType, oldIDByProtoType := map[reflect.Type]uint32{
		reflect.TypeOf((*TestMessage)(nil)).Elem(): 1,
	}, proto.IDByProtoType
	proto.IDByProtoType = idByProtoType
	// mock ConstructorByID
	constructorByID, oldConstructorByID := map[uint32]func() interface{}{
		1: func() interface{} { return &TestMessage{} },
	}, proto.ConstructorByID
	proto.ConstructorByID = constructorByID
	defer func() {
		proto.IDByProtoType = oldIDByProtoType
		proto.ConstructorByID = oldConstructorByID
	}()
	s := NewStub(nil)
	s.Register(ServiceCalleeA)

	// test server
	t.Run("server", func(t *testing.T) {
		svr, msg := s.NewServer(nil), &TestMessage{
			Str: "dada",
		}
		assert.NotNil(t, svr.log, "log entry should not be nil")

		raw := msg.Encode(make([]byte, 0, msg.Len()))
		assert.Equal(t, ErrFunctionNotFound, svr.Invoke("ServiceCalleeB", 2, raw, func(b []byte, err error) error { return nil }),
			"ErrFunctionNotFound when invoke to unregistered function")

		svr.Invoke("ServiceCalleeA", 1, raw, func(b []byte, err error) error {
			assert.NotNil(t, b, "nil marshaled message")
			assert.Nil(t, err, "got error: ", err)
			reply := &TestMessage{}
			if e := reply.Decode(b); e != nil {
				t.Fatal(e)
			}
			assert.Equal(t, reply.Str, msg.Str, "echo string not matched")
			return nil
		})
	})

	// test client
	t.Run("client", func(t *testing.T) {
		c, msg := s.NewClient(nil), &TestMessage{
			Str: "dada",
		}

		assert.NotNil(t, c.log, "log entry should not be nil")
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
		respIfce, err := c.Call(ctx, "ServiceCalleeB", msg, func(id uint32, name string, data []byte) error {
			return nil
		})
		assert.Equal(t, ErrFunctionNotFound, err, "ErrFunctionNotFound when invoke to unregistered function")

		raw := msg.Encode(make([]byte, 0, msg.Len()))
		respIfce, err = c.Call(ctx, "ServiceCalleeA", msg, func(id uint32, name string, data []byte) error {
			assert.Equal(t, "ServiceCalleeA", name)
			v, exist := c.calls.Load(id)
			assert.True(t, exist)
			respChan, isResp := v.(chan *rpcResponse)
			assert.True(t, isResp)
			assert.NotNil(t, respChan)

			go func() {
				c.Reply(id, raw, nil)
			}()
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp, ok := respIfce.(*TestMessage); !ok {
			t.Fatal("wrong response message type: ", reflect.TypeOf(resp))
		} else {
			assert.Equal(t, msg.Str, resp.Str)
		}

		respIfce, err = c.Call(ctx, "ServiceCalleeA", msg, func(id uint32, name string, data []byte) error {
			cancel()
			return nil
		})
		assert.Equal(t, ErrRPCCanceled, err)
	})
}
