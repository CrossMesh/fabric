package mux

import (
	"bytes"
	"io"
	"sync"
)

var (
	startCode = []byte{0x00, 0x00, 0x00, 0x01}

	// 00 00 02 --> 00 00 02 02
	escapeSeq1 = []byte{0x00, 0x00, 0x02, 0x02}
	// 00 00 00 01 --> 00 00 02 00 01
	escapeSeq2 = []byte{0x00, 0x00, 0x02, 0x00, 0x01}

	zeroSeq  = []byte{0x00, 0x00, 0x00}
	stripSeq = zeroSeq[:2]
)

const (
	defaultBufferSize = 512
)

type Muxer interface {
	Mux([]byte) (int, error)
	Reset() error
}

type Demuxer interface {
	Demux([]byte, func([]byte)) (int, error)
	Reset() error
}

type StreamMuxer struct {
	w           io.Writer
	leadWritten bool
	bufs        sync.Pool
}

func NewStreamMuxer(w io.Writer) *StreamMuxer {
	return &StreamMuxer{
		w: w,
		bufs: sync.Pool{
			New: func() interface{} {
				return bytes.NewBuffer(make([]byte, 0, defaultBufferSize))
			},
		},
		leadWritten: false,
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

func (m *StreamMuxer) Reset() error {
	m.leadWritten = false
	return nil
}

func (m *StreamMuxer) Mux(frame []byte) (written int, err error) {
	buf := m.allocBuffer()
	defer m.freeBuffer(buf)

	if !m.leadWritten {
		if written, err = buf.Write(startCode); err != nil {
			return 0, err
		}
		m.leadWritten = true
	}

	last := 0
MuxMainLoop:
	for idx := 0; idx+2 < len(frame); {
		if frame[idx] != 0x00 {
			idx++
			continue
		}
		if frame[idx+1] != 0x00 {
			idx += 2
			continue
		}
		for {
			// case 1: 0x00 0x00 0x02 --> 0x00 0x00 0x02 0x02
			if frame[idx+2] == 0x02 {
				if _, err = buf.Write(frame[last:idx]); err != nil {
					return 0, err
				}
				if _, err = buf.Write(escapeSeq1); err != nil {
					return 0, err
				}
				idx += 3
				last = idx
				continue MuxMainLoop
			}
			// case 2: 0x00 0x00 0x??
			if frame[idx+2] != 0x00 {
				idx += 3
				continue MuxMainLoop
			}
			if idx+3 >= len(frame) {
				break
			}
			// case 3: 0x00 0x00 0x00 0x00
			for frame[idx+3] == 0x00 {
				idx++
				if idx+3 >= len(frame) {
					break MuxMainLoop
				}
			}
			// fast path to case 2.
			if frame[idx+3] == 0x02 {
				idx++
				continue
			}
			break
		}

		if idx+3 >= len(frame) {
			break
		}
		// case 4: 0x00 0x00 0x00 0x??
		if frame[idx+3] != 0x01 {
			idx += 4
			continue MuxMainLoop
		}
		// case 5: 0x00 0x00 0x00 0x01 --> 0x00 0x00 0x02 0x00 0x01
		if _, err = buf.Write(frame[last:idx]); err != nil {
			return 0, err
		}
		if _, err = buf.Write(escapeSeq2); err != nil {
			return 0, err
		}
		idx += 4
		last = idx
	}
	// flush the last
	if _, err = buf.Write(frame[last:]); err != nil {
		return 0, err
	}
	if written, err = buf.Write(startCode); err != nil {
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

func (d *StreamDemuxer) Reset() error {
	d.buf = nil
	return nil
}

func (d *StreamDemuxer) indexStartCode(raw []byte) (idx int) {
	left := d.left
loopForStartCode:
	for idx = 0; idx < len(raw); idx++ {
		if left == 3 {
			switch raw[idx] {
			case 0x00:
			case 0x01:
				d.buf = make([]byte, 0, defaultBufferSize)
				idx++
				left = 0
				break loopForStartCode
			default:
				left = 0
			}
		} else {
			switch raw[idx] {
			case 0x00:
				left++
			default:
				left = 0
			}
		}
	}
	d.left = left
	return idx
}

func (d *StreamDemuxer) Demux(raw []byte, emit func([]byte)) (read int, err error) {
	idx := 0
	if d.buf == nil {
		idx = d.indexStartCode(raw)
		if d.buf == nil {
			return len(raw), nil
		}
	}

	buf, left := d.buf, d.left
	preserved := len(d.buf)
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
				buf = append(buf, c)
			} else {
				left++
			}

		case 1:
			if c != 0x00 {
				buf = append(buf, 0x00, c)
				left = 0
			} else {
				left++
			}

		case 2:
			if c == 0x02 {
				buf = append(buf, 0x00, 0x00)
				left = 0

			} else if c != 0x00 {
				buf = append(buf, 0x00, 0x00, c)
				left = 0
			} else {
				left++
			}

		case 3:
			if c == 0x00 {
				buf = append(buf, 0x00)
				break
			}
			if c == 0x01 {
				emit(buf)
				buf = buf[0:0]
			} else if c != 0x02 {
				buf = append(buf, 0x00, 0x00, 0x00, c)
			} else {
				buf = append(buf, 0x00, 0x00, 0x00)
			}
			left = 0
		}
		idx++
	}

	return
}
