package proto

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHello(t *testing.T) {
	msg, psk := &Hello{
		Lead: []byte{1, 2, 3, 4, 5, 6},
	}, []byte("12345")
	msg.Refresh()
	msg.Sign(psk)

	assert.True(t, msg.Verify(psk))
	assert.False(t, msg.Verify([]byte("dadadad")))

	buf := make([]byte, msg.Len())
	t.Run("encode", func(t *testing.T) {
		buf = msg.Encode(buf)
		assert.True(t, bytes.Compare(buf[:6], msg.Lead) == 0)
	})

	t.Run("decode", func(t *testing.T) {
		msg := &Hello{
			Lead: []byte{1, 1},
		}
		assert.Error(t, msg.Decode(buf))
		msg.Lead = []byte{1, 2, 3, 4, 5, 6}
		assert.NoError(t, msg.Decode(buf))
		assert.True(t, msg.Verify(psk))
	})
}

func TestWelcome(t *testing.T) {
	msg := &Welcome{
		Welcome:  false,
		Identity: "starstudio.org",
	}
	assert.NoError(t, msg.EncodeMessage("ass"))
	assert.Error(t, msg.EncodeMessage("this should be longer then 31 bytes after converted to []byte."))

	// encode
	buf := make([]byte, msg.Len())
	buf = msg.Encode(buf)
	assert.Equal(t, len(buf), msg.Len())

	msg.Welcome = true
	buf = msg.Encode(buf)
	// decode
	decoded := &Welcome{}
	assert.Error(t, decoded.Decode(buf[:3]))
	assert.Error(t, decoded.Decode(buf[:1]))
	assert.NoError(t, decoded.Decode(buf))
	assert.Equal(t, msg.Welcome, decoded.Welcome)
	assert.Equal(t, msg.Identity, decoded.Identity)
	assert.Equal(t, msg.DecodeMessage(), decoded.DecodeMessage())
}

func TestConnect(t *testing.T) {
	msg := &Connect{
		Version:  ConnectAES256GCM,
		Identity: "starstudio.org",
	}

	// encode
	buf := make([]byte, msg.Len())
	buf = msg.Encode(buf)
	assert.Equal(t, len(buf), msg.Len())

	// decode
	decoded := &Connect{}
	assert.Error(t, decoded.Decode(buf[:1]))
	assert.NoError(t, decoded.Decode(buf))
	assert.Equal(t, msg.Version, decoded.Version)
	assert.Equal(t, msg.Identity, decoded.Identity)
}
