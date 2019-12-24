package uut

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
	stripSeq = []byte{0x00, 0x00}
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
	leftBuf *bytes.Buffer
	buf     *bytes.Buffer
	bufs    sync.Pool
}

func NewStreamDemuxer() *StreamDemuxer {
	d := &StreamDemuxer{
		bufs: sync.Pool{
			New: func() interface{} {
				return bytes.NewBuffer(make([]byte, 0, defaultBufferSize))
			},
		},
	}
	return d
}

func (d *StreamDemuxer) allocBuffer() *bytes.Buffer {
	buf := d.bufs.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

func (d *StreamDemuxer) freeBuffer(buf *bytes.Buffer) {
	d.bufs.Put(buf)
}

func (d *StreamDemuxer) Reset() {
	if d.leftBuf != nil {
		d.freeBuffer(d.leftBuf)
	}
	if d.buf != nil {
		d.freeBuffer(d.buf)
	}
	d.leftBuf = nil
	d.buf = nil
}

func (d *StreamDemuxer) Demux(raw []byte, emit func([]byte)) (int, error) {
	preservedRaw := 0

	if d.leftBuf != nil {
		preservedRaw = d.leftBuf.Len()
		if _, err := d.leftBuf.Write(raw); err != nil {
			return 0, err
		}
		raw = d.leftBuf.Bytes()
	}

	last, read, preserved, idx := 0, 0, 0, 0
	if d.buf != nil {
		preserved = d.buf.Len()
	}

	fallback := func() {
		d.buf.Truncate(preserved)
		if d.leftBuf != nil {
			d.leftBuf.Truncate(preservedRaw)
		}
	}
	write := func(dat []byte) error {
		if len(dat) < 0 || d.buf == nil {
			return nil
		}
		n, err := d.buf.Write(dat)
		if err != nil {
			// recover
			fallback()
			return err
		}
		read += n
		return nil
	}

	for idx+2 < len(raw) {
		if d.buf == nil {
			last = idx
		}
		if raw[idx] == 0x00 && raw[idx+1] == 0x00 {
			if raw[idx+2] == 0x01 {
				// case: 00 00 01. strip 01.
				if d.buf == nil { // boundary not found yet. ignore broken bytes.
					continue
				}
				// write demuxed part.
				if err := write(raw[last:idx]); err != nil {
					return 0, err
				}
				if err := write(stripSeq); err != nil {
					return 0, err
				}
				idx += 3
				read++
				last = idx
				continue

			} else if raw[idx+2] == 0x00 {
				if idx+3 >= len(raw) {
					// case: 00 00 00 ??, frameLead may be cut off.
					break
				}
				if raw[idx+3] == 0x01 {
					// boundary.
					if d.buf != nil {
						if err := write(raw[last:idx]); err != nil {
							return 0, err
						}
						emit(d.buf.Bytes())
						d.freeBuffer(d.buf)
					}
					idx += 4
					read += 4
					last = idx
					d.buf = d.allocBuffer()
				} else if raw[idx+3] != 0x00 {
					// case: 00 00 00 xx(!=00 and !=01), could not be frameLead.
					idx += 4
				} else {
					// case: 00 00 00 00 ??, frameLead may be cut off.
					idx++
				}
			} else {
				idx += 3
			}
		} else {
			idx++
		}
	}
	if err := write(raw[last:idx]); err != nil {
		return 0, err
	}

	// deal with left data.
	oldLeftBuf := d.leftBuf
	if idx < len(raw) {
		newLeftBuf := d.allocBuffer()
		n, err := newLeftBuf.Write(raw[idx:])
		if err != nil {
			fallback()
			return 0, err
		}
		read += n
		d.leftBuf = newLeftBuf
	} else {
		// all data consumed.
		d.leftBuf = nil
	}
	if oldLeftBuf != nil {
		d.freeBuffer(oldLeftBuf)
	}

	return read - preservedRaw, nil
}
