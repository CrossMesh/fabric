package uut

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"math/rand"
	"strconv"
	"testing"
)

func TestMuxDemux(t *testing.T) {
	cases := [][]byte{
		[]byte{0x00, 0x23, 0x48, 0xFE, 0x00, 0x00, 0x01, 0x01},
		[]byte{0x00, 0xFE, 0x00, 0x01, 0x00, 0x01, 0x01},
		[]byte{0x00, 0xFE, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x01},
		[]byte{0x00},
		[]byte{0xEA, 0x86, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01},
		[]byte{},
	}
	res := [][]byte{
		[]byte{0x00, 0x00, 0x00, 0x01, 0x00, 0x23, 0x48, 0xFE, 0x00, 0x00, 0x01, 0x01, 0x01},
		[]byte{0x00, 0x00, 0x00, 0x01, 0x00, 0xFE, 0x00, 0x01, 0x00, 0x01, 0x01},
		[]byte{0x00, 0x00, 0x00, 0x01, 0x00, 0xFE, 0x00, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x01},
		[]byte{0x00, 0x00, 0x00, 0x01, 0x00},
		[]byte{0x00, 0x00, 0x00, 0x01, 0xEA, 0x86, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01},
		[]byte{0x00, 0x00, 0x00, 0x01},
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
		if _, err := seq.Write(frameLead); err != nil {
			t.Error(err)
		}
		t.Logf("decode seq: %v", seq.Bytes())
		demuxer.Demux(seq.Bytes(), func(pkt []byte) {
			if bytes.Compare(pkt, cases[cidx]) != 0 {
				t.Fatalf("decoded %v: %v, not equal %v", cidx, pkt, cases[cidx])
			} else {
				t.Logf("decoded %v: %v, pass.", cidx, cases[cidx])
			}
			cidx++
		})

		// pieces feed test.
		maxCaseLength := 0
		for idx := range cases {
			if len(cases[idx]) > maxCaseLength {
				maxCaseLength = len(cases[idx])
			}
		}
		maxCaseLength += len(frameLead)
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
		t.StartTimer()
		for idx := 0; idx < mb; idx++ {
			if _, err := muxer.Mux(buf); err != nil {
				t.Fatal(err)
			}
		}
		t.StopTimer()
	}
	t.Run("16mb", func(t *testing.B) { benchmarkMuxing(16, t) })
	t.Run("32mb", func(t *testing.B) { benchmarkMuxing(32, t) })
	t.Run("64mb", func(t *testing.B) { benchmarkMuxing(64, t) })
	t.Run("128mb", func(t *testing.B) { benchmarkMuxing(128, t) })
	t.Run("256mb", func(t *testing.B) { benchmarkMuxing(256, t) })
}

//func BenchmarkRandomDecodeBlock1MB(t *testing.B) {
//
//	benchmarkDemuxing := func(mb int, t *testing.B) {
//		t.StopTimer()
//		seq := bytes.NewBuffer(make([]byte, 0, 1<<20)) // 1mb
//		muxer := NewStreamMuxer(seq)
//		demuxer := NewStreamDemuxer()
//		blkSize := 1 << 20 // 1mb
//		buf := make([]byte, blkSize)
//		for sz := 0; sz+strconv.IntSize < blkSize; sz += strconv.IntSize / 8 {
//			binary.PutVarint(buf[sz:sz+strconv.IntSize], int64(rand.Int()))
//		}
//		if _, err := muxer.Mux(buf); err != nil {
//			t.Fatal(err)
//		}
//		buf = append(buf, frameLead...)
//		demuxer.Demux(buf, func() {})
//	}
//
//	t.Run("16mb", func(t *testing.B) { benchmarkDemuxing(16, t) })
//	t.Run("32mb", func(t *testing.B) { benchmarkDemuxing(32, t) })
//	t.Run("64mb", func(t *testing.B) { benchmarkDemuxing(64, t) })
//	t.Run("128mb", func(t *testing.B) { benchmarkDemuxing(128, t) })
//	t.Run("256mb", func(t *testing.B) { benchmarkDemuxing(256, t) })
//}
