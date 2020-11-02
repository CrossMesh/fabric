package mux

import (
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"sync"
)

const (
	maxGCMStreamFrameLength = (uint32(1) << 24) - 1
)

var (
	FrameTooLarge  = errors.New("frame too large")
	FrameCorrupted = errors.New("frame corrupted")
)

type GCMStreamMuxer struct {
	w     io.Writer
	aead  cipher.AEAD
	block cipher.Block
	nonce []byte
	buf   []byte
}

func NewGCMStreamMuxer(w io.Writer, block cipher.Block, nonce []byte) (*GCMStreamMuxer, error) {
	if len(nonce) < 1 {
		nonce = make([]byte, 12)

		if read, err := rand.Read(nonce); err != nil {
			return nil, err
		} else if read != len(nonce) {
			return nil, fmt.Errorf("%v byte nonce required, but %v read", len(nonce), read)
		}
	}

	m := &GCMStreamMuxer{
		w:     w,
		block: block,
		nonce: nonce,
		buf:   make([]byte, defaultBufferSize),
	}
	if err := m.Reset(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *GCMStreamMuxer) Parallel() bool { return false }

func (m *GCMStreamMuxer) Mux(frame []byte) (written int, err error) {
	if uint32(len(frame)) > maxGCMStreamFrameLength {
		return 0, FrameTooLarge
	}
	var hdrBuf [3]byte

	dataSize, buf := len(frame)+m.aead.Overhead(), m.buf[0:0]
	// encrypt frame header.
	hdrBuf[0] = byte(dataSize & 0xFF)
	hdrBuf[1] = byte((dataSize >> 8) & 0xFF)
	hdrBuf[2] = byte((dataSize >> 16) & 0xFF)
	buf = m.aead.Seal(buf, m.nonce, hdrBuf[:], nil)
	// encrypt data.
	buf = m.aead.Seal(buf, m.nonce, frame, buf)

	return m.w.Write(buf)
}

func (m *GCMStreamMuxer) ResetNonce(nonce []byte) error {
	m.nonce = nonce
	return m.Reset()
}

func (m *GCMStreamMuxer) Reset() error {
	aead, err := cipher.NewGCMWithNonceSize(m.block, len(m.nonce))
	if err != nil {
		return err
	}
	m.aead = aead
	return nil
}

type GCMStreamDemuxer struct {
	aead        cipher.AEAD
	block       cipher.Block
	nonce       []byte
	buf         []byte
	frameLength int
	bufs        sync.Pool
}

func NewGCMStreamDemuxer(block cipher.Block, nonce []byte) (*GCMStreamDemuxer, error) {
	d := &GCMStreamDemuxer{
		block: block,
		nonce: nonce,
		buf:   make([]byte, 0, defaultBufferSize),
		bufs: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, defaultBufferSize)
			},
		},
		frameLength: -1,
	}
	if err := d.Reset(); err != nil {
		return nil, err
	}
	return d, nil
}

func (m *GCMStreamDemuxer) allocBuffer() []byte {
	buf := m.bufs.Get().([]byte)
	return buf[:0]
}

func (m *GCMStreamDemuxer) freeBuffer(buf []byte) {
	if buf == nil {
		return
	}
	m.bufs.Put(buf)
}

func (d *GCMStreamDemuxer) Demux(raw []byte, emit func([]byte) bool) (read int, err error) {
	originLen, buf, frameLength, headerLength, cont := len(raw), d.buf, d.frameLength, d.aead.Overhead()+3, true

	defer func() {
		if err != nil {
			buf = buf[0:0]
			frameLength = -1
		}
		d.buf, d.frameLength = buf, frameLength
	}()

	if len(raw) > 0 && cont {
		openBuf := d.allocBuffer()
		defer d.freeBuffer(openBuf)

		for cont {
			// no enough bytes to open header.
			if frameLength < 0 {
				// decrypt header.
				if lenBuf := len(buf); lenBuf == 0 {
					if len(raw) < headerLength {
						// no enough bytes to decrypt header.
						buf, raw = append(buf, raw...), raw[len(raw):]
						break
					}

					// fast path: avoid copying.
					if openBuf, err = d.aead.Open(openBuf[:0], d.nonce, raw[:headerLength], nil); err != nil {
						return 0, err
					}
				} else {
					fill := headerLength - len(buf)
					if fill > len(raw) {
						fill = len(raw)
					}
					buf, raw = append(buf, raw[:fill]...), raw[fill:]
					if len(buf) < headerLength {
						// no enough bytes left to decrypt header.
						break
					}
					if openBuf, err = d.aead.Open(openBuf[:0], d.nonce, buf[:headerLength], nil); err != nil {
						return 0, err
					}
				}
				if len(openBuf) != 3 {
					return 0, FrameCorrupted
				}
				frameLength = int(uint32(openBuf[0]) | (uint32(openBuf[1]) << 8) | (uint32(openBuf[2]) << 16))
			}

			need := frameLength + headerLength   // frameLength calculated by muxer. overhead has included.
			if lenBuf := len(buf); lenBuf == 0 { // no copy?
				if need > len(raw) { // no enough bytes left to decrypt payload.
					buf, raw = append(buf, raw...), raw[len(raw):]
					break
				}

				// fast path: avoid copying while decrypting.
				if openBuf, err = d.aead.Open(openBuf[:0], d.nonce, raw[headerLength:need], raw[:headerLength]); err != nil {
					return 0, err
				}
				cont = emit(openBuf)

			} else {
				need -= lenBuf
				if need > len(raw) { // no enough bytes left to decrypt payload.
					buf, raw = append(buf, raw...), raw[len(raw):]
					break
				}

				originBuf := buf
				buf = append(buf, raw[:need]...)
				// decrypt data.
				if openBuf, err = d.aead.Open(openBuf[:0], d.nonce, buf[headerLength:headerLength+frameLength], buf[:headerLength]); err != nil {
					buf = originBuf
					return 0, err
				}
				cont = emit(openBuf)
				buf = buf[:0]
			}

			raw, frameLength = raw[need:], -1
		}
	}

	return originLen - len(raw), nil
}

func (d *GCMStreamDemuxer) Reset() error {
	aead, err := cipher.NewGCMWithNonceSize(d.block, len(d.nonce))
	if err != nil {
		return err
	}
	d.aead = aead
	return nil
}
