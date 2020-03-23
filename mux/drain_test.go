package mux

import (
	"bytes"
	"math/rand"
	"testing"
	"time"

	arbit "git.uestc.cn/sunmxt/utt/arbiter"
	"github.com/stretchr/testify/assert"
)

func TestDrainer(t *testing.T) {
	maxLatency := time.Second * 2

	arbiter, buffer := arbit.New(nil), bytes.NewBuffer(make([]byte, 0, 32*1024*1024))
	drainer := NewDrainer(arbiter, nil, buffer, 65536*2, maxLatency)
	drainer.FastPathThreshold = 65536 * 2
	drainer.MaxLatency = time.Second

	// prepare random bytes.
	randBytes := make([]byte, 65536*8)
	_, err := rand.Read(randBytes)
	assert.NoError(t, err)

	// inital state: in fast mode.
	assert.True(t, drainer.isFastMode())
	// case 1: low throughput write.
	bytePerOp, ops := 30, 100
	for idx := 0; idx < ops; idx++ {
		sliceBegin := idx * bytePerOp
		written, err := drainer.Write(randBytes[sliceBegin : sliceBegin+bytePerOp])
		assert.NoError(t, err)
		assert.Equal(t, bytePerOp, written)
	}
	assert.True(t, drainer.isFastMode())                                       // no entering to high throughtput mode.
	assert.Equal(t, randBytes[:bytePerOp*ops], buffer.Bytes()[:bytePerOp*ops]) // verify byte order.
	buffer.Reset()
	drainer.written = 0

	// case 2-5: test high throughput mode.
	written, err := drainer.Write(randBytes[:65536*2])
	assert.NoError(t, err)
	assert.Equal(t, 65536*2, written)
	written, err = drainer.Write(randBytes[65536*2 : 65536*5])
	assert.NoError(t, err)
	assert.Equal(t, 65536*3, written)
	// case 2: high throughput mode triggered.
	assert.False(t, drainer.isFastMode())
	assert.Equal(t, 65536*5, buffer.Len())
	// case 3: cut off write.
	written, err = drainer.Write(randBytes[65536*5 : 65536*6])
	assert.NoError(t, err)
	assert.Equal(t, 65536*5, buffer.Len())
	written, err = drainer.Write(randBytes[65536*6 : 65536*8])
	// case 4: only partial bytes is in buffer.
	assert.NoError(t, err)
	assert.Equal(t, 65536*7, buffer.Len())
	assert.Equal(t, 65536, drainer.ptr)
	// case 5: bytes should be drained automatically.
	time.Sleep(time.Second*2 + time.Millisecond*500)
	assert.Equal(t, 65536*8, buffer.Len())
	assert.Equal(t, randBytes[:65536*8], buffer.Bytes()) // final check: verify byte order.
	buffer.Reset()

	// case 6: back to low throughput mode.
	written, err = drainer.Write(randBytes[:128*1024])
	assert.NoError(t, err)
	assert.Equal(t, 128*1024, written)
	written, err = drainer.Write(randBytes[128*1024 : 320*1024])
	assert.NoError(t, err)
	assert.Equal(t, 192*1024, written)
	assert.False(t, drainer.isFastMode())
	assert.Equal(t, 320*1024, buffer.Len())
	written, err = drainer.Write(randBytes[320*1024 : 321*1024])
	assert.NoError(t, err)
	assert.Equal(t, 1024, written)
	assert.Equal(t, 320*1024, buffer.Len())
	drainer.written = 0
	written, err = drainer.Write(randBytes[321*1024 : 322*1024])
	assert.Equal(t, 322*1024, buffer.Len())

	assert.True(t, drainer.isFastMode()) // leaved high throughput mode now.
	drainer.Drain()
	drainer.Close()
	drainer.Close()

	arbiter.Shutdown()
	arbiter.Join()
}
