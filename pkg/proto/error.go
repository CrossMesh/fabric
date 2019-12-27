package proto

import "errors"

var (
	ErrBufferTooShort = errors.New("Buffer too short.")
	ErrInvalidPacket  = errors.New("Invalid packet content.")
)
