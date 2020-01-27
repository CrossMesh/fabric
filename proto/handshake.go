package proto

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
)

type Hello struct {
	Lead []byte
	IV   [12]byte
	HMAC [32]byte
}

func (h *Hello) Encode(buf []byte) []byte {
	buf = buf[0:0]
	buf = append(buf, h.Lead...)
	buf = append(buf, h.IV[:]...)
	buf = append(buf, h.HMAC[:]...)
	return buf
}

func (h *Hello) Decode(buf []byte) error {
	if len(buf) < h.Len() || bytes.Compare(h.Lead, buf[:len(h.Lead)]) != 0 {
		return ErrInvalidPacket
	}
	copy(h.IV[:12], buf[len(h.Lead):12+len(h.Lead)])
	copy(h.HMAC[:32], buf[len(h.Lead)+12:12+32+len(h.Lead)])
	return nil
}

func (c *Hello) HMACSum(psk []byte) []byte {
	h := hmac.New(sha256.New, psk)
	h.Write([]byte("lead#"))
	h.Write(c.Lead)
	h.Write([]byte("psk#"))
	if psk != nil {
		h.Write(psk)
	}
	h.Write([]byte("IV#"))
	h.Write(c.IV[:])
	return h.Sum(nil)
}

func (c *Hello) Sign(psk []byte) {
	copy(c.HMAC[:], c.HMACSum(psk))
}

func (c *Hello) Verify(psk []byte) bool {
	return bytes.Equal(c.HMACSum(psk), c.HMAC[:])
}

func (h *Hello) Len() int {
	return len(h.Lead) + len(h.IV) + len(h.HMAC)
}

func (h *Hello) Refresh() { rand.Read(h.IV[:]) }

type Welcome struct {
	Welcome  bool
	RawMsg   [31]byte
	MsgLen   int
	Identity string
}

func (c *Welcome) Len() int { return 1 + c.MsgLen + 2 + len([]byte(c.Identity)) }

func (c *Welcome) EncodeMessage(msg string) error {
	raw := []byte(msg)
	if len(raw) > len(c.RawMsg) {
		return ErrBufferTooShort
	}
	copy(c.RawMsg[:len(raw)], raw)
	c.MsgLen = len(raw)
	return nil
}

func (c *Welcome) DecodeMessage() string {
	return string(c.RawMsg[:])
}

func (c *Welcome) Encode(buf []byte) []byte {
	buf = buf[0:0]
	if c.Welcome {
		buf = append(buf, 1)
	} else {
		buf = append(buf, 0)
	}
	identityBin := []byte(c.Identity)
	buf = append(buf, byte(c.MsgLen&0xFF))
	buf = append(buf, byte(len(identityBin)&0xFF))
	buf = append(buf, c.RawMsg[:c.MsgLen]...)
	buf = append(buf, identityBin...)
	return buf
}

func (c *Welcome) Decode(buf []byte) error {
	if len(buf) < 3 {
		return ErrInvalidPacket
	}
	msgLen, identityLength := uint8(buf[1]), uint8(buf[2])
	c.Welcome = buf[0] > 0
	if int(3+msgLen+identityLength) > len(buf) {
		return ErrInvalidPacket
	}
	c.MsgLen = int(msgLen)
	copy(c.RawMsg[:c.MsgLen], buf[3:3+c.MsgLen])
	c.Identity = string(buf[3+c.MsgLen : 3+c.MsgLen+int(identityLength)])
	return nil
}

type Connect struct {
	Version  uint8
	Identity string
}

const (
	ConnectNoCrypt   = 0
	ConnectAES256GCM = 1
)

func (c *Connect) Len() int {
	return 3 + len([]byte(c.Identity))
}

func (c *Connect) Encode(buf []byte) []byte {
	buf = buf[0:0]
	idBin := []byte(c.Identity)
	buf = append(buf, c.Version)
	buf = append(buf, 0, 0)
	binary.BigEndian.PutUint16(buf[1:], uint16(len(idBin)))
	buf = append(buf, idBin...)
	return buf
}

func (c *Connect) Decode(buf []byte) error {
	if len(buf) < 3 {
		return ErrInvalidPacket
	}
	c.Version = buf[0]
	idLen := binary.BigEndian.Uint16(buf[1:3])
	if len(buf) < int(3+idLen) {
		return ErrInvalidPacket
	}
	c.Identity = string(buf[3 : 3+idLen])
	return nil
}
