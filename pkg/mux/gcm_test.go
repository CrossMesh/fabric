package mux

import (
	"bytes"
	"crypto/aes"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestGCMMuxDemux(t *testing.T) {
	cases := [][]byte{
		[]byte{0x00, 0xFE, 0x00, 0x01, 0x00, 0x01, 0x01},
		[]byte{0x00, 0xFE, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x01},
		[]byte{0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x02, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01},
	}
	buf, nonce, psk := bytes.NewBuffer(make([]byte, 0, 1024)), make([]byte, 16), "testingkey"
	sumRaw := []byte(psk)
	sumRaw = append(sumRaw, nonce...)
	key := sha256.Sum256(sumRaw)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	aes, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}
	var mux *GCMStreamMuxer
	if mux, err = NewGCMStreamMuxer(buf, aes, nonce); err != nil {
		t.Fatal(err)
	}
	for idx := range cases {
		if _, err = mux.Mux(cases[idx]); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("demuxing_auto_seq", func(t *testing.T) {
		var demuxer *GCMStreamDemuxer
		if demuxer, err = NewGCMStreamDemuxer(aes, nonce); err != nil {
			t.Fatal(err)
		}
		// pieces feed test.
		maxPiecesLength := buf.Len()
		for pieceLength := 1; pieceLength < maxPiecesLength; pieceLength++ {
			demuxer.Reset()
			caseN := 0
			t.Run(fmt.Sprintf("block_length_%v", pieceLength), func(t *testing.T) {
				for idx := 0; idx < buf.Len(); idx += pieceLength {
					end := idx + pieceLength
					if end >= buf.Len() {
						end = buf.Len()
					}
					if _, err = demuxer.Demux(buf.Bytes()[idx:end], func(frame []byte) {
						if caseN >= len(cases) {
							t.Fatalf("more then %v frame decoded.", len(cases))
						}
						if bytes.Compare(frame, cases[caseN]) != 0 {
							t.Fatalf("decoded %v: %v, not equal %v", caseN, frame, cases[caseN])
						} else {
							t.Logf("decoded %v: %v, pass.", caseN, cases[caseN])
						}
						caseN++
					}); err != nil {
						t.Fatal(err)
					}
				}
				if caseN != len(cases) {
					t.Fatalf("demuxer missing some frame.")
				}
			})
		}
	})
}
