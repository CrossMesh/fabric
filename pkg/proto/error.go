package proto

import "errors"

var (
	ErrBufferTooShort = errors.New("Buffer too short.")
	ErrContentTooLong = errors.New("Packet content too long.")
	ErrInvalidPacket  = errors.New("Invalid packet content.")
)
