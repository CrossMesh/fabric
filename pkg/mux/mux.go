package mux

import (
	"bytes"
	"io"
	"sync"
)

var (
	frameLead = []byte{0x00, 0x00, 0x00, 0x01}

	// 00 00 01 --> 00 00 01 01
	escapeSeq1 = []byte{0x00, 0x00, 0x01, 0x01}
	// 00 00 00 01 --> 00 00 01 00 01
	escapeSeq2 = []byte{0x00, 0x00, 0x01, 0x00, 0x01}
	// 00 00 01 --> 00 00

	zeroSeq  = []byte{0x00, 0x00, 0x00}
	stripSeq = zeroSeq[:2]
)

const (
	defaultBufferSize = 512
)

type StreamMuxer struct {
	w    io.Writer
	bufs sync.Pool
}

func NewStreamMuxer(w io.Writer) *StreamMuxer {
	return &StreamMuxer{
		w: w,
		bufs: sync.Pool{
			New: func() interface{} {
				return bytes.NewBuffer(make([]byte, 0, defaultBufferSize))
			},
		},
	}
}

func (m *StreamMuxer) allocBuffer() *bytes.Buffer {
	buf := m.bufs.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

func (m *StreamMuxer) freeBuffer(buf *bytes.Buffer) {
	m.bufs.Put(buf)
}

func (m *StreamMuxer) Mux(frame []byte) (written int, err error) {
	buf := m.allocBuffer()
	defer m.freeBuffer(buf)

	if written, err = buf.Write(frameLead); err != nil {
		return 0, err
	}
	last := 0
	for idx := 0; idx+2 < len(frame); {
		if frame[idx] != 0x00 {
			idx++
			continue
		}
		if frame[idx+1] != 0x00 {
			idx += 2
			continue
		}
		if frame[idx+2] == 0x00 {
			// 00 00 00 ??
			if idx+3 >= len(frame) {
				// end.
				idx += 3
				break
			}
			if frame[idx+3] == 0x01 {
				// 00 00 00 01
				if _, err = buf.Write(frame[last:idx]); err != nil {
					return 0, err
				}
				if _, err = buf.Write(escapeSeq2); err != nil {
					return 0, err
				}
				idx += 4
				last = idx
				continue
			}
			if frame[idx+3] == 0x00 {
				// 00 00 00 00
				idx++
				continue
			}
			idx += 4
			continue
		}
		if frame[idx+2] == 0x01 {
			// 00 00 01
			if _, err = buf.Write(frame[last:idx]); err != nil {
				return 0, err
			}
			if _, err = buf.Write(escapeSeq1); err != nil {
				return 0, err
			}
			idx += 3
			last = idx
			continue
		}
		idx += 2
	}
	if _, err = buf.Write(frame[last:]); err != nil {
		return 0, err
	}
	return m.w.Write(buf.Bytes())
}

type StreamDemuxer struct {
	left int
	buf  []byte
}

func NewStreamDemuxer() *StreamDemuxer {
	d := &StreamDemuxer{}
	return d
}

func (d *StreamDemuxer) Reset() {
	d.buf = nil
}

func (d *StreamDemuxer) Demux(raw []byte, emit func([]byte)) (read int, err error) {
	buf, left, preserved, idx := d.buf, d.left, 0, 0
	if d.buf != nil {
		preserved = len(d.buf)
	}
	defer func() {
		if err != nil {
			if d.buf != nil {
				d.buf = d.buf[:preserved]
			}
		} else {
			d.left = left
			d.buf = buf
		}
	}()

	for idx < len(raw) {
		c := raw[idx]
		switch left {
		case 0:
			if c != 0x00 {
				if buf != nil {
					buf = append(buf, c)
				}
				break
			}
			left++

		case 1:
			if c != 0x00 {
				// case 1: 00 xx(xx != 00).
				if buf != nil {
					buf = append(buf, 0x00, c)
				}
				left = 0
				break
			}
			left++

		case 2:
			if c == 0x01 {
				// case 1: 00 00 01. strip 01.
				if buf != nil {
					buf = append(buf, 0x00, 0x00)
				}
				left = 0
			} else if c != 0x00 {
				if buf != nil {
					buf = append(buf, 0x00, 0x00, c)
				}
				left = 0
			} else {
				left++
			}

		case 3:
			if c == 0x01 {
				// case: 00 00 00 01, frameLead
				if buf != nil {
					emit(buf)
					buf = buf[0:0]
				} else {
					buf = make([]byte, 0, defaultBufferSize)
				}
				left = 0
				break
			}
			if c != 0x00 {
				// case 4: 00 00 00 xx(!=00 and !=01), could not be frameLead.
				if buf != nil {
					buf = append(buf, 0x00, 0x00, 0x00, c)
				}
				left = 0
			} else {
				// case 3: 00 00 00 00 ?? --> 00 00 00 ??
				if buf != nil {
					buf = append(buf, 0x00)
				}
			}

		}
		idx++
	}
	return
}
