package proto

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProtoHeaderPack(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	buf := make([]byte, ProtocolMessageHeaderSize, ProtocolMessageHeaderSize+8)

	assert.Nil(t, PackProtocolMessageHeader(buf[0:1], MsgTypeRawFrame))
	buf = PackProtocolMessageHeader(buf, MsgTypeRawFrame)
	assert.NotNil(t, buf)
	buf = append(buf, data...)

	ty, origin := UnpackProtocolMessageHeader(buf)
	assert.Equal(t, MsgTypeRawFrame, ty)
	assert.True(t, bytes.Compare(data, origin) == 0)
	buf[0] = 1
	ty, origin = UnpackProtocolMessageHeader(buf)
	assert.Nil(t, origin)
	assert.Equal(t, MsgTypeUnknown, ty)
}
