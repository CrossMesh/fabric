package mux

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"math/rand"
	"strconv"
	"testing"
	"time"
)

func TestMuxDemux(t *testing.T) {
	cases := [][]byte{
		[]byte{0x00, 0x23, 0x48, 0xfe, 0x00, 0x00, 0x01, 0x01},
		[]byte{0x00, 0xFE, 0x00, 0x01, 0x00, 0x01, 0x01},
		[]byte{0x00, 0xFE, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x01},
		[]byte{0x00},
		[]byte{0x00, 0x00, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01},
		[]byte{0x00, 0x00, 0x00, 0x00, 0x01},
		[]byte{0x00, 0x00, 0x00, 0x00, 0x02},
		[]byte{},
	}
	res := [][]byte{
		[]byte{0x00, 0x00, 0x00, 0x01, 0x00, 0x23, 0x48, 0xfe, 0x00, 0x00, 0x01, 0x01, 0x00, 0x00, 0x00, 0x01},
		[]byte{0x00, 0x00, 0x00, 0x01, 0x00, 0xFE, 0x00, 0x01, 0x00, 0x01, 0x01, 0x00, 0x00, 0x00, 0x01},
		[]byte{0x00, 0x00, 0x00, 0x01, 0x00, 0xFE, 0x00, 0x01, 0x00, 0x00, 0x02, 0x00, 0x01, 0x01, 0x00, 0x00, 0x00, 0x01},
		[]byte{0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x01},
		[]byte{0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x02, 0x00, 0x01, 0x00, 0x00, 0x02, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01},
		[]byte{0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x02, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01},
		[]byte{0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x02, 0x02, 0x00, 0x00, 0x00, 0x01},
		[]byte{0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01},
	}
	t.Run("muxing", func(t *testing.T) {
		for idx := range cases {
			t.Run(fmt.Sprintf("case %v", idx), func(t *testing.T) {
				result := bytes.NewBuffer(make([]byte, 0, 16))
				muxer := NewStreamMuxer(result)
				if _, err := muxer.Mux(cases[idx]); err != nil {
					t.Error(err)
				}
				if bytes.Compare(result.Bytes(), res[idx]) != 0 {
					t.Fatalf("%v: %v --> %v, not equal %v", t.Name(), cases[idx], result.Bytes(), res[idx])
				} else {
					t.Logf("%v: %v --> %v, pass.", t.Name(), cases[idx], result.Bytes())
				}
			})
		}
	})
	t.Run("demuxing_auto_seq", func(t *testing.T) {
		seq := bytes.NewBuffer(make([]byte, 0, 16))
		muxer := NewStreamMuxer(seq)
		for idx := range cases {
			if _, err := muxer.Mux(cases[idx]); err != nil {
				t.Error(err)
			}
		}

		// basic
		demuxer := NewStreamDemuxer()
		cidx := 0
		t.Logf("decode seq: %v", seq.Bytes())
		demuxer.Demux(seq.Bytes(), func(pkt []byte) {
			if bytes.Compare(pkt, cases[cidx]) != 0 {
				t.Fatalf("decoded %v: %v, not equal %v", cidx, pkt, cases[cidx])
			} else {
				t.Logf("decoded %v: %v, pass.", cidx, cases[cidx])
			}
			cidx++
		})
		if cidx != len(res) {
			t.Fatalf("package count error.")
		}

		// pieces feed test.
		maxCaseLength := 0
		for idx := range cases {
			if len(cases[idx]) > maxCaseLength {
				maxCaseLength = len(cases[idx])
			}
		}
		maxCaseLength += len(startCode)
		for blkLen := 1; blkLen < maxCaseLength; blkLen++ {
			t.Run(fmt.Sprintf("block_length_%v", blkLen), func(t *testing.T) {
				demuxer.Reset()
				cidx = 0
				for idx := 0; idx < seq.Len(); idx += blkLen {
					end := idx + blkLen
					if end >= seq.Len() {
						end = seq.Len()
					}
					demuxer.Demux(seq.Bytes()[idx:end], func(pkt []byte) {
						if bytes.Compare(pkt, cases[cidx]) != 0 {
							t.Fatalf("decoded %v: %v, not equal %v", cidx, pkt, cases[cidx])
						} else {
							t.Logf("decoded %v: %v, pass.", cidx, cases[cidx])
						}
						cidx++
					})
				}
				if cidx != len(res) {
					t.Fatalf("package count error.")
				}
			})
		}
	})
}

func BenchmarkRandomMuxBlock1MB(t *testing.B) {
	benchmarkMuxing := func(mb int, t *testing.B) {
		t.StopTimer()
		muxer := NewStreamMuxer(ioutil.Discard)
		blkSize := 1 << 20 // 1mb
		t.N = mb
		buf := make([]byte, blkSize)
		for sz := 0; sz+strconv.IntSize < blkSize; sz += strconv.IntSize / 8 {
			binary.PutVarint(buf[sz:sz+strconv.IntSize], int64(rand.Int()))
		}
		t.SetBytes(int64(blkSize))
		for idx := 0; idx < mb; idx++ {
			didx := 0
			rand.Seed(time.Now().Unix())
			for didx < len(buf) {
				written := rand.Int() & int(0xFFF)
				if written >= len(buf)-didx {
					written = len(buf) - didx
				}
				t.StartTimer()
				if _, err := muxer.Mux(buf[didx : didx+written]); err != nil {
					t.Fatal(err)
				}
				t.StopTimer()
				didx += written
			}
		}
	}
	t.Run("16mb", func(t *testing.B) { benchmarkMuxing(16, t) })
	t.Run("32mb", func(t *testing.B) { benchmarkMuxing(32, t) })
}

func BenchmarkRandomDecodeBlock1MB(t *testing.B) {
	benchmarkDemuxing := func(mb int, t *testing.B) {
		t.StopTimer()
		blkSize := 1 << 20 // 1mb
		streamBuf := bytes.NewBuffer(make([]byte, 0, blkSize))
		muxer := NewStreamMuxer(streamBuf)
		demuxer := NewStreamDemuxer()
		t.N = mb
		buf := make([]byte, blkSize)
		for sz := 0; sz+strconv.IntSize < blkSize; sz += strconv.IntSize / 8 {
			binary.PutVarint(buf[sz:sz+strconv.IntSize], int64(rand.Int()))
		}
		t.SetBytes(int64(blkSize))
		cuts := make([]int, 0, 256)
		for idx := 0; idx < mb; idx++ {
			didx := 0
			cuts = cuts[0:0]
			streamBuf.Reset()
			muxer.Reset()
			for didx < len(buf) {
				written := rand.Int() & 0xFFF
				if written >= len(buf)-didx {
					written = len(buf) - didx
				}
				if _, err := muxer.Mux(buf[didx : didx+written]); err != nil {
					t.Fatal(err)
				}
				didx += written
				cuts = append(cuts, didx)
			}

			demuxer.Reset()
			cidx, lastCut := 0, 0
			didx = 0
			for didx < streamBuf.Len() {
				written := rand.Int() & 0xFFF
				if written >= streamBuf.Len()-didx {
					written = streamBuf.Len() - didx
				}
				t.StartTimer()
				if _, err := demuxer.Demux(streamBuf.Bytes()[didx:didx+written], func(pkt []byte) {
					t.StopTimer()
					if cidx >= len(cuts) {
						t.Fatal("more packets decoded.")
					}
					origin := buf[lastCut:cuts[cidx]]
					if bytes.Compare(pkt, origin) != 0 {
						t.Fatalf("decode %v: %v not equal %v", cidx, pkt, origin)
					}
					lastCut = cuts[cidx]
					cidx++
					t.StartTimer()
				}); err != nil {
					t.Fatal(err)
				}
				didx += written
			}
		}
	}

	t.Run("16mb", func(t *testing.B) { benchmarkDemuxing(16, t) })
	t.Run("32mb", func(t *testing.B) { benchmarkDemuxing(32, t) })
}
