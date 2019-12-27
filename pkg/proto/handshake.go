package proto

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
)

var (
	helloMagic = []byte{'u', 't', 't', 0, 0, 0}
)

type Hello struct {
	Challenge [16]byte
}

func (h *Hello) Encode(buf []byte) error {
	if len(buf) < h.Len() {
		return ErrBufferTooShort
	}
	buf = buf[0:0]
	buf = append(buf, helloMagic...)
	buf = append(buf, h.Challenge[:]...)
	return nil
}

func (h *Hello) Decode(buf []byte) error {
	if len(buf) < h.Len() {
		return ErrInvalidPacket
	}
	copy(h.Challenge[0:16], buf[0:16])
	return nil
}

func (h *Hello) Len() int {
	return len(helloMagic) + len(h.Challenge)
}

func (h *Hello) Refresh() { rand.Read(h.Challenge[:]) }

type Connect struct {
	ACLKey    string
	Signature []byte
}

func (c *Connect) HMAC(challenge []byte, psk []byte) []byte {
	h := hmac.New(sha512.New, psk)
	h.Write([]byte("cha"))
	h.Write(challenge)
	h.Write([]byte("acl#" + c.ACLKey))
	return h.Sum(nil)
}

func (c *Connect) Len() int {
	return len([]byte(c.ACLKey)) + len(c.Signature)
}

func (c *Connect) Sign(challenge []byte, psk []byte) {
	c.Signature = c.HMAC(challenge, psk)
}

func (c *Connect) Verify(challenge []byte, psk []byte) bool {
	return bytes.Compare(c.Signature, c.HMAC(challenge, psk)) == 0
}

func (c *Connect) Encode(buf []byte) error {
	if len(buf) < c.Len() {
		return ErrBufferTooShort
	}
	buf = buf[0:0]
	buf = append(buf, []byte(c.ACLKey)...)
	buf = append(buf, c.Signature...)
	return nil
}

type ConnectResult struct {
	Welcome bool
	RawMsg  [31]byte
	MsgLen  int
}

func (c *ConnectResult) Len() int { return 1 + c.MsgLen }

func (c *ConnectResult) EncodeMessage(msg string) error {
	raw := []byte(msg)
	if len(raw) > len(c.RawMsg) {
		return ErrBufferTooShort
	}
	copy(c.RawMsg[:len(raw)], raw)
	c.MsgLen = len(raw)
	return nil
}

func (c *ConnectResult) DecodeMessage() string {
	return string(c.RawMsg[:])
}

func (c *ConnectResult) Encode(buf []byte) error {
	if len(buf) < c.Len() {
		return ErrBufferTooShort
	}
	if c.Welcome {
		buf[0] = 1
	} else {
		buf[0] = 0
	}
	return nil
}

func (c *ConnectResult) Decode(buf []byte) error {
	if len(buf) < c.Len() {
		return ErrInvalidPacket
	}
	c.Welcome = buf[0] > 0
	return nil
}
